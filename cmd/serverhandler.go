package main

import (
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/bluenviron/gortsplib/v5"
	"github.com/bluenviron/gortsplib/v5/pkg/base"
	"github.com/bluenviron/gortsplib/v5/pkg/description"
	"github.com/pion/rtp"
)

// Streamer defines the interface for describing and streaming media content.
type Streamer interface {
	Describe() ([]*description.Media, error)
	Stream(PacketRTPWriter) error
	Close()
}

type PacketRTPWriter interface {
	WritePacketRTP(*description.Media, *rtp.Packet) error
}

// Ensure ServerHandler implements the necessary interfaces
var (
	_ gortsplib.ServerHandlerOnSessionOpen  = (*ServerHandler)(nil)
	_ gortsplib.ServerHandlerOnSessionClose = (*ServerHandler)(nil)
	_ gortsplib.ServerHandlerOnDescribe     = (*ServerHandler)(nil)
	_ gortsplib.ServerHandlerOnSetup        = (*ServerHandler)(nil)
)

// ServerHandler manages RTSP server callbacks and stream initialization.
type ServerHandler struct {
	Streamer Streamer
	Server   *gortsplib.Server
	stream   *gortsplib.ServerStream
	mu       sync.RWMutex
	count    atomic.Uint32
}

func (h *ServerHandler) initialize() error {
	// Get media descriptions from streamer
	medias, err := h.Streamer.Describe()
	if err != nil {
		return fmt.Errorf("failed to get media descriptions: %w", err)
	}
	h.stream = &gortsplib.ServerStream{
		Server: h.Server,
		Desc: &description.Session{
			Medias: medias,
		},
	}
	if err := h.stream.Initialize(); err != nil {
		return err
	}
	go func() {
		h.Streamer.Stream(h.stream)
		h.mu.Lock()
		defer h.mu.Unlock()
		h.stream = nil
	}()
	return nil
}

func (h *ServerHandler) OnSessionOpen(ctx *gortsplib.ServerHandlerOnSessionOpenCtx) {
	h.mu.RLock()
	if h.stream != nil {
		h.mu.RUnlock()
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.stream != nil {
		return
	}
	h.initialize()
}

func (h *ServerHandler) OnSessionClose(ctx *gortsplib.ServerHandlerOnSessionCloseCtx) {}

// OnDescribe handles the RTSP DESCRIBE request, initializing the stream if necessary.
func (h *ServerHandler) OnDescribe(ctx *gortsplib.ServerHandlerOnDescribeCtx) (*base.Response, *gortsplib.ServerStream, error) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return &base.Response{
		StatusCode: base.StatusOK,
	}, h.stream, nil
}

// OnSetup handles the RTSP SETUP request, returning the stream for playback.
func (h *ServerHandler) OnSetup(ctx *gortsplib.ServerHandlerOnSetupCtx) (*base.Response, *gortsplib.ServerStream, error) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return &base.Response{
		StatusCode: base.StatusOK,
	}, h.stream, nil
}
