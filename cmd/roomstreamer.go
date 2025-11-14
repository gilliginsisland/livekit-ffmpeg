package main

import (
	"context"
	"fmt"
	"log"
	"sync/atomic"

	"github.com/bluenviron/gortsplib/v5/pkg/description"
	lksdk "github.com/livekit/server-sdk-go/v2"
	"github.com/pion/webrtc/v4"
	"golang.org/x/sync/errgroup"
)

type ConnecterFunc func() (endpoint, token string, err error)

// RoomStreamer manages streaming from a LiveKit room.
type RoomStreamer struct {
	Connecter ConnecterFunc
	room      *lksdk.Room
	audio     atomic.Pointer[TrackStreamer]
	video     atomic.Pointer[TrackStreamer]
	gathering chan struct{}
	ctx       context.Context
	cancel    func()
}

// NewRoomStreamer creates a new RoomStreamer with a function to get connection details.
func NewRoomStreamer(details ConnecterFunc) *RoomStreamer {
	rs := RoomStreamer{
		Connecter: details,
		gathering: make(chan struct{}),
	}
	rs.ctx, rs.cancel = context.WithCancel(context.Background())
	rs.room = lksdk.NewRoom(&lksdk.RoomCallback{
		ParticipantCallback: lksdk.ParticipantCallback{
			OnTrackSubscribed: func(track *webrtc.TrackRemote, pub *lksdk.RemoteTrackPublication, rp *lksdk.RemoteParticipant) {
				ts, err := NewTrackStreamer(track)
				if err != nil {
					return
				}
				// Try to assign track based on kind
				switch track.Kind() {
				case webrtc.RTPCodecTypeAudio:
					if !rs.audio.CompareAndSwap(nil, ts) {
						log.Printf("Audio track skipped, already exists")
						return
					}
				case webrtc.RTPCodecTypeVideo:
					if !rs.video.CompareAndSwap(nil, ts) {
						log.Printf("Video track skipped, already exists")
						return
					}
				}
				if rs.audio.Load() != nil && rs.video.Load() != nil {
					close(rs.gathering)
				}
			},
		},
	})
	endpoint, token, err := details()
	if err != nil {
		log.Fatalf("Failed to get livekit room credentials: %v", err)
		return nil
	}
	err = rs.room.JoinWithToken(endpoint, token)
	if err != nil {
		log.Fatalf("Failed to join livekit room: %v", err)
		return nil
	}
	context.AfterFunc(rs.ctx, rs.room.Disconnect)
	return &rs
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
