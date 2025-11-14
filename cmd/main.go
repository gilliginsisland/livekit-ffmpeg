package main

import (
	"context"
	"flag"
	"log"
	"os/signal"
	"syscall"

	"github.com/bluenviron/gortsplib/v5"
)

func main() {
	// App flags
	url := flag.String("url", "", "LiveKit server URL (e.g., wss://host)")
	token := flag.String("token", "", "LiveKit access token")
	listen := flag.String("listen", "", "Address for RTSP server")

	flag.Parse()

	ctx, _ := signal.NotifyContext(context.Background(), syscall.SIGINT)

	handler := ServerHandler{
		Streamer: NewRoomStreamer(func() (string, string, error) {
			return *url, *token, nil
		}),
	}
	handler.Server = &gortsplib.Server{
		RTSPAddress: *listen,
		Handler:     &handler,
	}

	context.AfterFunc(ctx, func() {
		handler.Streamer.Close()
		handler.Server.Close()
	})

	// RTSP Server setup
	if err := handler.Server.Start(); err != nil {
		log.Fatalf("Failed to start RTSP server: %v", err)
	}
	log.Printf("RTSP server started on %s", *listen)
	context.AfterFunc(ctx, handler.Server.Close)

	// Wait for server to close
	handler.Server.Wait()
}
