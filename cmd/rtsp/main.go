package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/url"
	"os/signal"
	"syscall"

	"github.com/bluenviron/gortsplib/v5"
)

func main() {
	// App flags
	listen := flag.String("listen", "", "Address for RTSP server")

	flag.Parse()

	ctx, _ := signal.NotifyContext(context.Background(), syscall.SIGINT)

	// Define the factory function for creating streamers
	factory := func(path string, query url.Values) (Streamer, error) {
		u := query.Get("url")
		if u == "" {
			return nil, fmt.Errorf("no url provided in query parameters")
		}
		token := query.Get("token")
		if token == "" {
			return nil, fmt.Errorf("no token provided in query parameters")
		}
		rs := NewRoomStreamer()
		err := rs.JoinWithToken(u, token)
		if err != nil {
			return nil, fmt.Errorf("Failed to join LiveKit room: %w")
		}
		return rs, nil
	}

	handler := NewServerHandler(factory)
	handler.Server = &gortsplib.Server{
		RTSPAddress: *listen,
		Handler:     handler,
	}

	context.AfterFunc(ctx, func() {
		// Close all streamers and streams
		handler.mu.Lock()
		for _, route := range handler.routes {
			if route.Streamer != nil {
				route.Streamer.Close()
			}
			if route.Stream != nil {
				route.Stream.Close()
			}
		}
		handler.mu.Unlock()
		handler.Server.Close()
	})

	// RTSP Server setup
	if err := handler.Server.Start(); err != nil {
		log.Fatalf("Failed to start RTSP server: %v", err)
	}
	log.Printf("RTSP server started on %s, provide path and token via rtsp://%s/path?token=your-token", *listen, *listen)
	context.AfterFunc(ctx, handler.Server.Close)

	// Wait for server to close
	handler.Server.Wait()
}
