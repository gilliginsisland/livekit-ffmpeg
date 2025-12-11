package main

import (
	"context"
	"fmt"
	"log/slog"
	"sync/atomic"
	"time"

	"github.com/bluenviron/gortsplib/v5/pkg/description"
	lksdk "github.com/livekit/server-sdk-go/v2"
	"github.com/pion/webrtc/v4"
	"golang.org/x/sync/errgroup"
)

// RoomStreamer manages streaming from a LiveKit room.
type RoomStreamer struct {
	room      *lksdk.Room
	audio     atomic.Pointer[TrackStreamer]
	video     atomic.Pointer[TrackStreamer]
	gathering chan struct{}
	ctx       context.Context
	cancel    func()
}

// NewRoomStreamer creates a new RoomStreamer with a function to get connection details.
func NewRoomStreamer() *RoomStreamer {
	rs := RoomStreamer{
		gathering: make(chan struct{}),
	}
	rs.ctx, rs.cancel = context.WithCancel(context.Background())
	rs.room = lksdk.NewRoom(&lksdk.RoomCallback{
		ParticipantCallback: lksdk.ParticipantCallback{
			OnTrackSubscribed: rs.onTrackSubscribed,
		},
	})
	context.AfterFunc(rs.ctx, rs.room.Disconnect)
	return &rs
}

func (rs *RoomStreamer) onTrackSubscribed(track *webrtc.TrackRemote, pub *lksdk.RemoteTrackPublication, rp *lksdk.RemoteParticipant) {
	ts, err := NewTrackStreamer(track)
	if err != nil {
		return
	}
	// Try to assign track based on kind
	switch track.Kind() {
	case webrtc.RTPCodecTypeAudio:
		if !rs.audio.CompareAndSwap(nil, ts) {
			slog.Debug("audio track skipped", slog.String("reason", "already exists"))
			return
		}
	case webrtc.RTPCodecTypeVideo:
		if !rs.video.CompareAndSwap(nil, ts) {
			slog.Debug("video track skipped", slog.String("reason", "already exists"))
			return
		}
		go func() {
			// Create a new ticker that fires every 2 seconds.
			ticker := time.NewTicker(2 * time.Second)
			defer ticker.Stop()
			for {
				select {
				case <-rs.ctx.Done():
					return
				case <-ticker.C:
					rp.WritePLI(track.SSRC())
				}
			}
		}()
	}
	if rs.audio.Load() != nil && rs.video.Load() != nil {
		close(rs.gathering)
	}
}

func (rs *RoomStreamer) Join(url string, info lksdk.ConnectInfo, opts ...lksdk.ConnectOption) error {
	return rs.room.Join(url, info, opts...)
}

func (rs *RoomStreamer) JoinWithToken(url string, token string, opts ...lksdk.ConnectOption) error {
	return rs.room.JoinWithToken(url, token, opts...)
}

// Describe connects to the LiveKit room and returns media descriptions for audio and video tracks.
func (rs *RoomStreamer) Describe() ([]*description.Media, error) {
	select {
	case <-rs.ctx.Done():
		return nil, rs.ctx.Err()
	case <-rs.gathering:
	}

	audio, video := rs.audio.Load(), rs.video.Load()
	if audio == nil || video == nil {
		return nil, fmt.Errorf("nil track")
	}

	var medias []*description.Media
	for _, streamer := range []*TrackStreamer{audio, video} {
		media, err := streamer.Description()
		if err != nil {
			return nil, fmt.Errorf("failed to initialize track: %w", err)
		}
		medias = append(medias, media...)
	}

	return medias, nil
}

// Stream starts streaming audio and video tracks to the provided ServerStream.
func (rs *RoomStreamer) Stream(w PacketRTPWriter) error {
	audio, video := rs.audio.Load(), rs.video.Load()
	if audio == nil || video == nil {
		return fmt.Errorf("nil track")
	}
	g, ctx := errgroup.WithContext(rs.ctx)
	for _, streamer := range []*TrackStreamer{audio, video} {
		g.Go(func() error {
			return streamer.Stream(w)
		})
		context.AfterFunc(ctx, streamer.Close)
	}
	return g.Wait()
}

func (rs *RoomStreamer) Close() {
	rs.cancel()
}
