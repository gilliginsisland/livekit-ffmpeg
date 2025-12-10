package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"sync"
	"sync/atomic"
	"time"

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
	_ gortsplib.ServerHandlerOnConnOpen  = (*ServerHandler)(nil)
	_ gortsplib.ServerHandlerOnConnClose = (*ServerHandler)(nil)
	_ gortsplib.ServerHandlerOnRequest   = (*ServerHandler)(nil)
	_ gortsplib.ServerHandlerOnResponse  = (*ServerHandler)(nil)
	_ gortsplib.ServerHandlerOnDescribe  = (*ServerHandler)(nil)
	_ gortsplib.ServerHandlerOnSetup     = (*ServerHandler)(nil)
	_ gortsplib.ServerHandlerOnPlay      = (*ServerHandler)(nil)
	_ gortsplib.ServerHandlerOnPause     = (*ServerHandler)(nil)
)

// route holds a streamer and its associated server stream for a specific path.
type route struct {
	Streamer Streamer
	Stream   *gortsplib.ServerStream
	path     string
	count    atomic.Int32
	timer    *time.Timer
	expires  atomic.Int64
}

func (r *route) Acquire() int32 {
	r.timer.Stop()
	return r.count.Add(1)
}

func (r *route) Release() int32 {
	return r.count.Add(-1)
}

// UserData holds the context and cancel function for a connection.
type UserData struct {
	ctx    context.Context
	cancel context.CancelFunc
}

// NewUserData creates a new connUserData with a cancellable context.
func NewUserData() *UserData {
	ctx, cancel := context.WithCancel(context.Background())
	return &UserData{ctx: ctx, cancel: cancel}
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

func (h *ServerHandler) getOrCreateRoute(path string, query url.Values) (*route, error) {
	h.mu.RLock()
	{
		r, exists := h.routes[path]
		if exists {
			defer h.mu.RUnlock()
			r.Acquire()
			return r, nil
		}
	}
	h.mu.RUnlock()

	h.mu.Lock()
	defer h.mu.Unlock()

	// Double-check after acquiring write lock
	if r, exists := h.routes[path]; exists {
		r.Acquire()
		return r, nil
	}

	// Use the factory to create a new streamer
	streamer, err := h.Factory(path, query)
	if err != nil {
		return nil, fmt.Errorf("failed to create streamer for path %s: %w", path, err)
	}

	// Get media descriptions from streamer
	medias, err := streamer.Describe()
	if err != nil {
		streamer.Close()
		return nil, fmt.Errorf("failed to get media descriptions for path %s: %w", path, err)
	}

	stream := &gortsplib.ServerStream{
		Server: h.Server,
		Desc: &description.Session{
			Medias: medias,
		},
	}
	if err := stream.Initialize(); err != nil {
		streamer.Close()
		return nil, err
	}

	r := &route{
		Streamer: streamer,
		Stream:   stream,
		path:     path,
	}
	r.timer = time.AfterFunc(30*time.Second, func() {
		h.mu.Lock()
		defer h.mu.Unlock()
		if r != h.routes[path] || r.count.Load() > 0 || r.expires.Load() > time.Now().UnixNano() {
			return
		}
		delete(h.routes, path)
		h.mu.Unlock()
		streamer.Close()
		stream.Close()
	})
	r.Acquire()
	h.routes[path] = r

	go func() {
		streamer.Stream(stream)
		h.mu.Lock()
		defer h.mu.Unlock()
		if r != h.routes[path] {
			return
		}
		delete(h.routes, path)
		h.mu.Unlock()
		streamer.Close()
		stream.Close()
	}()

	return r, nil
}

func (h *ServerHandler) onStreamRequest(ctx *gortsplib.ServerHandlerOnDescribeCtx) (*base.Response, *gortsplib.ServerStream, error) {
	// Get user data from connection
	ud, ok := ctx.Conn.UserData().(*UserData)
	if !ok {
		return &base.Response{
			StatusCode: base.StatusInternalServerError,
		}, nil, fmt.Errorf("no user data in connection")
	}

	r, err := h.getOrCreateRoute(ctx.Path, (*url.URL)(ctx.Request.URL).Query())
	if err != nil {
		return &base.Response{
			StatusCode: base.StatusBadGateway,
		}, nil, err
	}

	context.AfterFunc(ud.ctx, func() {
		h.mu.RLock()
		r.expires.Store(time.Now().Add(30 * time.Second).UnixNano())
		if r.Release() > 0 {
			return
		}
		h.mu.RUnlock()
		if !h.mu.TryLock() {
			return
		}
		defer h.mu.Unlock()
		if r.count.Load() > 0 {
			return
		}
		r.timer.Reset(30 * time.Second)
	})

	return &base.Response{
		StatusCode: base.StatusOK,
	}, r.Stream, nil
}

// OnDescribe handles the RTSP DESCRIBE request, initializing the stream for the requested path if necessary.
func (h *ServerHandler) OnDescribe(ctx *gortsplib.ServerHandlerOnDescribeCtx) (*base.Response, *gortsplib.ServerStream, error) {
	slog.Debug("OnDescribe called for path:", ctx.Path)
	return h.onStreamRequest(ctx)
}

// OnSetup handles the RTSP SETUP request, returning the stream for the requested path.
func (h *ServerHandler) OnSetup(ctx *gortsplib.ServerHandlerOnSetupCtx) (*base.Response, *gortsplib.ServerStream, error) {
	slog.Debug("OnSetup called for path:", ctx.Path)
	return h.onStreamRequest(&gortsplib.ServerHandlerOnDescribeCtx{
		Conn:    ctx.Conn,
		Request: ctx.Request,
		Path:    ctx.Path,
		Query:   ctx.Query,
	})
}

// OnConnOpen sets up user data with a cancellable context.
func (h *ServerHandler) OnConnOpen(ctx *gortsplib.ServerHandlerOnConnOpenCtx) {
	slog.Debug("OnConnOpen called")
	ctx.Conn.SetUserData(NewUserData())
}

// OnConnClose cancels the context, triggering cleanup of routes.
func (h *ServerHandler) OnConnClose(ctx *gortsplib.ServerHandlerOnConnCloseCtx) {
	slog.Debug("OnConnClose called")
	if ud, ok := ctx.Conn.UserData().(*UserData); ok {
		ud.cancel()
	}
}

// OnRequest logs when a request is received from a connection.
func (h *ServerHandler) OnRequest(conn *gortsplib.ServerConn, req *base.Request) {
	slog.Debug("OnRequest called", slog.String("req", req.String()))
}

// OnResponse logs when a response is sent to a connection.
func (h *ServerHandler) OnResponse(conn *gortsplib.ServerConn, res *base.Response) {
	slog.Debug("OnResponse called: %s", res)
}

// OnPlay logs when a PLAY request is received.
func (h *ServerHandler) OnPlay(ctx *gortsplib.ServerHandlerOnPlayCtx) (*base.Response, error) {
	slog.Debug("OnPlay called")
	return &base.Response{
		StatusCode: base.StatusOK,
	}, nil
}

// OnPause logs when a PAUSE request is received.
func (h *ServerHandler) OnPause(ctx *gortsplib.ServerHandlerOnPauseCtx) (*base.Response, error) {
	slog.Debug("OnPause called")
	return &base.Response{
		StatusCode: base.StatusOK,
	}, nil
}
