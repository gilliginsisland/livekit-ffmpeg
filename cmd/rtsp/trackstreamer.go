package main

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/bluenviron/gortsplib/v5/pkg/description"
	"github.com/pion/rtp"
	"github.com/pion/sdp/v3"
	"github.com/pion/webrtc/v4"
)

// TrackStreamer manages a single track and its associated operations.
type TrackStreamer struct {
	Track  *webrtc.TrackRemote
	md     description.Media
	ctx    context.Context
	cancel func()
}

func NewTrackStreamer(track *webrtc.TrackRemote) (*TrackStreamer, error) {
	codec := track.Codec()
	_, encoding, _ := strings.Cut(codec.MimeType, "/")
	if encoding == "" {
		encoding = codec.MimeType
	}
	encoding = strings.ToUpper(encoding)

	// Create Pion SDP media description
	pmd := &sdp.MediaDescription{
		MediaName: sdp.MediaName{
			Media:  track.Kind().String(),
			Protos: []string{"RTP", "AVP"},
		},
	}
	pmd.WithCodec(
		uint8(codec.PayloadType),
		encoding,
		codec.ClockRate,
		codec.Channels,
		codec.SDPFmtpLine,
	)
	pmd.WithMediaSource(uint32(track.SSRC()), "", "", track.ID())

	ts := TrackStreamer{Track: track}
	if err := ts.md.Unmarshal(pmd); err != nil {
		return nil, err
	}
	ts.ctx, ts.cancel = context.WithCancel(context.Background())
	return &ts, nil
}

func (ts *TrackStreamer) Description() ([]*description.Media, error) {
	return []*description.Media{&ts.md}, nil
}

// Start starts reading from the track and writing to the provided stream.
func (ts *TrackStreamer) Stream(w PacketRTPWriter) error {
	buf := make([]byte, 1500)
	for {
		if err := ts.ctx.Err(); err != nil {
			return err
		}

		n, _, err := ts.Track.Read(buf)
		if err == io.EOF {
			return nil
		} else if err != nil {
			return fmt.Errorf("error reading track: %w", err)
		}

		pkt := rtp.Packet{}
		if err := pkt.Unmarshal(buf[:n]); err != nil {
			return fmt.Errorf("unmarshal RTP packet: %w", err)
		}

		if err := w.WritePacketRTP(&ts.md, &pkt); err != nil {
			return fmt.Errorf("write RTP packet: %w", err)
		}
	}
}

func (ts *TrackStreamer) Close() {
	ts.cancel()
}
