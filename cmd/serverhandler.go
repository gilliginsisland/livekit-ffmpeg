package main

import (
	"errors"
	"fmt"
	"log"
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
	_ gortsplib.ServerHandlerOnConnOpen         = (*ServerHandler)(nil)
	_ gortsplib.ServerHandlerOnConnClose        = (*ServerHandler)(nil)
	_ gortsplib.ServerHandlerOnSessionOpen      = (*ServerHandler)(nil)
	_ gortsplib.ServerHandlerOnSessionClose     = (*ServerHandler)(nil)
	_ gortsplib.ServerHandlerOnRequest          = (*ServerHandler)(nil)
	_ gortsplib.ServerHandlerOnResponse         = (*ServerHandler)(nil)
	_ gortsplib.ServerHandlerOnDescribe         = (*ServerHandler)(nil)
	_ gortsplib.ServerHandlerOnAnnounce         = (*ServerHandler)(nil)
	_ gortsplib.ServerHandlerOnSetup            = (*ServerHandler)(nil)
	_ gortsplib.ServerHandlerOnPlay             = (*ServerHandler)(nil)
	_ gortsplib.ServerHandlerOnRecord           = (*ServerHandler)(nil)
	_ gortsplib.ServerHandlerOnPause            = (*ServerHandler)(nil)
	_ gortsplib.ServerHandlerOnGetParameter     = (*ServerHandler)(nil)
	_ gortsplib.ServerHandlerOnSetParameter     = (*ServerHandler)(nil)
	_ gortsplib.ServerHandlerOnPacketsLost      = (*ServerHandler)(nil)
	_ gortsplib.ServerHandlerOnDecodeError      = (*ServerHandler)(nil)
	_ gortsplib.ServerHandlerOnStreamWriteError = (*ServerHandler)(nil)
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
		h.stream.Close()
		h.mu.Lock()
		defer h.mu.Unlock()
		h.stream = nil
	}()
	return nil
}

func (h *ServerHandler) OnSessionOpen(ctx *gortsplib.ServerHandlerOnSessionOpenCtx) {
	log.Println("OnSessionOpen called")
}

func (h *ServerHandler) OnSessionClose(ctx *gortsplib.ServerHandlerOnSessionCloseCtx) {
	log.Println("OnSessionClose called")
}

// OnDescribe handles the RTSP DESCRIBE request, initializing the stream if necessary.
func (h *ServerHandler) OnDescribe(ctx *gortsplib.ServerHandlerOnDescribeCtx) (*base.Response, *gortsplib.ServerStream, error) {
	log.Println("OnDescribe called")

	h.mu.RLock()
	stream := h.stream
	h.mu.RUnlock()
	if stream == nil {
		h.mu.Lock()
		defer h.mu.Unlock()
		if h.stream == nil {
			if err := h.initialize(); err != nil {
				return &base.Response{
					StatusCode: base.StatusBadGateway,
				}, nil, err
			}
		}
		stream = h.stream
	}

	return &base.Response{
		StatusCode: base.StatusOK,
	}, h.stream, nil
}

// OnSetup handles the RTSP SETUP request, returning the stream for playback.
func (h *ServerHandler) OnSetup(ctx *gortsplib.ServerHandlerOnSetupCtx) (*base.Response, *gortsplib.ServerStream, error) {
	log.Println("OnSetup called")
	h.mu.RLock()
	defer h.mu.RUnlock()
	if h.stream == nil {
		return &base.Response{
			StatusCode: base.StatusBadGateway,
		}, nil, errors.New("empty stream")
	}
	return &base.Response{
		StatusCode: base.StatusOK,
	}, h.stream, nil
}

// OnConnOpen logs when a connection is opened.
func (h *ServerHandler) OnConnOpen(ctx *gortsplib.ServerHandlerOnConnOpenCtx) {
	log.Println("OnConnOpen called")
}

// OnConnClose logs when a connection is closed.
func (h *ServerHandler) OnConnClose(ctx *gortsplib.ServerHandlerOnConnCloseCtx) {
	log.Println("OnConnClose called")
}

// OnRequest logs when a request is received from a connection.
func (h *ServerHandler) OnRequest(conn *gortsplib.ServerConn, req *base.Request) {
	log.Println("OnRequest called")
}

// OnResponse logs when a response is sent to a connection.
func (h *ServerHandler) OnResponse(conn *gortsplib.ServerConn, res *base.Response) {
	log.Println("OnResponse called")
}

// OnAnnounce logs when an ANNOUNCE request is received.
func (h *ServerHandler) OnAnnounce(ctx *gortsplib.ServerHandlerOnAnnounceCtx) (*base.Response, error) {
	log.Println("OnAnnounce called")
	return &base.Response{
		StatusCode: base.StatusOK,
	}, nil
}

// OnPlay logs when a PLAY request is received.
func (h *ServerHandler) OnPlay(ctx *gortsplib.ServerHandlerOnPlayCtx) (*base.Response, error) {
	log.Println("OnPlay called")
	return &base.Response{
		StatusCode: base.StatusOK,
	}, nil
}

// OnRecord logs when a RECORD request is received.
func (h *ServerHandler) OnRecord(ctx *gortsplib.ServerHandlerOnRecordCtx) (*base.Response, error) {
	log.Println("OnRecord called")
	return &base.Response{
		StatusCode: base.StatusOK,
	}, nil
}

// OnPause logs when a PAUSE request is received.
func (h *ServerHandler) OnPause(ctx *gortsplib.ServerHandlerOnPauseCtx) (*base.Response, error) {
	log.Println("OnPause called")
	return &base.Response{
		StatusCode: base.StatusOK,
	}, nil
}

// OnGetParameter logs when a GET_PARAMETER request is received.
func (h *ServerHandler) OnGetParameter(ctx *gortsplib.ServerHandlerOnGetParameterCtx) (*base.Response, error) {
	log.Println("OnGetParameter called")
	return &base.Response{
		StatusCode: base.StatusOK,
	}, nil
}

// OnSetParameter logs when a SET_PARAMETER request is received.
func (h *ServerHandler) OnSetParameter(ctx *gortsplib.ServerHandlerOnSetParameterCtx) (*base.Response, error) {
	log.Println("OnSetParameter called")
	return &base.Response{
		StatusCode: base.StatusOK,
	}, nil
}

// OnPacketsLost logs when the server detects lost packets.
func (h *ServerHandler) OnPacketsLost(ctx *gortsplib.ServerHandlerOnPacketsLostCtx) {
	log.Println("OnPacketsLost called")
}

// OnDecodeError logs when a non-fatal decode error occurs.
func (h *ServerHandler) OnDecodeError(ctx *gortsplib.ServerHandlerOnDecodeErrorCtx) {
	log.Println("OnDecodeError called")
}

// OnStreamWriteError logs when a ServerStream is unable to write packets to a session.
func (h *ServerHandler) OnStreamWriteError(ctx *gortsplib.ServerHandlerOnStreamWriteErrorCtx) {
	log.Println("OnStreamWriteError called")
}
