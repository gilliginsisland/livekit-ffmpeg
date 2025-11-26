package main

import (
	"fmt"
	"log"
	"net/url"
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

// route holds a streamer and its associated server stream for a specific path.
type route struct {
	Streamer Streamer
	Stream   *gortsplib.ServerStream
	count    atomic.Uint32
}

// StreamerFactory defines a function to create a Streamer for a given path and query parameters.
type StreamerFactory func(path string, query url.Values) (Streamer, error)

// ServerHandler manages RTSP server callbacks and stream initialization for multiple paths.
type ServerHandler struct {
	Server  *gortsplib.Server
	Factory StreamerFactory
	routes  map[string]*route
	mu      sync.RWMutex
}

// NewServerHandler creates a new ServerHandler with the provided factory function.
func NewServerHandler(factory StreamerFactory) *ServerHandler {
	return &ServerHandler{
		Factory: factory,
		routes:  make(map[string]*route),
	}
}

func (h *ServerHandler) initialize(path string, query url.Values) (*base.Response, *gortsplib.ServerStream, error) {
	h.mu.RLock()
	{
		r, exists := h.routes[path]
		h.mu.RUnlock()
		if exists {
			return &base.Response{
				StatusCode: base.StatusOK,
			}, r.Stream, nil
		}
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	// Retrieve or create the route for the path
	if r, exists := h.routes[path]; exists {
		return &base.Response{
			StatusCode: base.StatusOK,
		}, r.Stream, nil
	}

	// Use the factory to create a new streamer
	streamer, err := h.Factory(path, query)
	if err != nil {
		return &base.Response{
			StatusCode: base.StatusBadGateway,
		}, nil, fmt.Errorf("failed to create streamer for path %s: %w", path, err)
	}

	// Get media descriptions from streamer
	medias, err := streamer.Describe()
	if err != nil {
		return &base.Response{
			StatusCode: base.StatusBadGateway,
		}, nil, fmt.Errorf("failed to get media descriptions for path %s: %w", path, err)
	}

	stream := &gortsplib.ServerStream{
		Server: h.Server,
		Desc: &description.Session{
			// BaseURL: ,
			Medias: medias,
		},
	}
	if err := stream.Initialize(); err != nil {
		return &base.Response{
			StatusCode: base.StatusBadGateway,
		}, nil, err
	}

	h.routes[path] = &route{
		Streamer: streamer,
		Stream:   stream,
	}

	go func() {
		streamer.Stream(stream)
		h.mu.Lock()
		defer h.mu.Unlock()
		if r, exists := h.routes[path]; exists && r.Streamer == streamer {
			delete(h.routes, path)
		}
	}()

	return &base.Response{
		StatusCode: base.StatusOK,
	}, stream, nil
}

func (h *ServerHandler) OnSessionOpen(ctx *gortsplib.ServerHandlerOnSessionOpenCtx) {
	log.Println("OnSessionOpen called")
}

func (h *ServerHandler) OnSessionClose(ctx *gortsplib.ServerHandlerOnSessionCloseCtx) {
	log.Println("OnSessionClose called")
}

// OnDescribe handles the RTSP DESCRIBE request, initializing the stream for the requested path if necessary.
func (h *ServerHandler) OnDescribe(ctx *gortsplib.ServerHandlerOnDescribeCtx) (*base.Response, *gortsplib.ServerStream, error) {
	log.Println("OnDescribe called for path:", ctx.Path)
	return h.initialize(ctx.Path, (*url.URL)(ctx.Request.URL).Query())
}

// OnSetup handles the RTSP SETUP request, returning the stream for the requested path.
func (h *ServerHandler) OnSetup(ctx *gortsplib.ServerHandlerOnSetupCtx) (*base.Response, *gortsplib.ServerStream, error) {
	log.Println("OnSetup called for path:", ctx.Path)
	return h.initialize(ctx.Path, (*url.URL)(ctx.Request.URL).Query())
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
	log.Println("OnRequest called: %s", req)
}

// OnResponse logs when a response is sent to a connection.
func (h *ServerHandler) OnResponse(conn *gortsplib.ServerConn, res *base.Response) {
	log.Println("OnResponse called: %s", res)
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
