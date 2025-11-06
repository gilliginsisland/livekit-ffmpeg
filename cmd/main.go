package main

import (
    "context"
    "flag"
    "fmt"
    "io"
    "log"
    "os"
    "os/exec"
    "os/signal"
    "strconv"
    "sync"
    "syscall"

    lksdk "github.com/livekit/server-sdk-go/v2"
    "github.com/pion/interceptor"
    "github.com/pion/webrtc/v4"
)

// TrackWriter handles writing RTP packets to a destination
type TrackReader struct {
    track interface {
        Read(b []byte) (n int, attributes interceptor.Attributes, err error)
    }
}

func (tr *TrackReader) Read(b []byte) (int, error) {
    n, _, err := tr.track.Read(b)
    return n, err
}

// FFMpeg manages the FFmpeg process and input file descriptors
type FFMpeg struct {
    fds    []*os.File
    cmd    *exec.Cmd
    cancel func()
}

// TrackPipe creates a new pipe and returns an RTPWriter for writing to it, adding the read end to extraFiles
func (f *FFMpeg) TrackPipe() (*os.File, error) {
    r, w, err := os.Pipe()
    if err != nil {
        return nil, fmt.Errorf("failed to create pipe: %v", err)
    }
    f.fds = append(f.fds, r)
    return w, nil
}

// Start constructs and starts the FFmpeg process with the configured inputs and user-provided extraArgs
func (f *FFMpeg) StartContext(ctx context.Context, outputArgs []string) error {
    var args []string
    for i := range f.fds {
        // Extra FDs start at 3 (after 0=stdin, 1=stdout, 2=stderr)
        args = append(args, "-i", "fd:", "-fd", strconv.Itoa(3+i))
        // Add mapping for this input to allow all tracks
        args = append(args, "-map", fmt.Sprintf("%d", i))
    }
    args = append(args, outputArgs...)

    f.cmd = exec.CommandContext(ctx, "ffmpeg", args...)
    f.cmd.ExtraFiles = f.fds
    f.cmd.Stdout = os.Stdout
    f.cmd.Stderr = os.Stderr

    // Start the FFmpeg process
    if err := f.cmd.Start(); err != nil {
        return fmt.Errorf("failed to start FFmpeg: %v", err)
    }

    log.Printf("Started FFmpeg with PID %d", f.cmd.Process.Pid)
    return nil
}

func (f *FFMpeg) Wait() error {
    return f.cmd.Wait()
}

func main() {
    // App flags (pointers returned by flag.String)
    url := flag.String("url", "", "LiveKit server URL (e.g., wss://host)")
    token := flag.String("token", "", "LiveKit access token")

    // Parse flags; everything after “--” remains in flag.Args()
    flag.Parse()
    ffmpegArgs := flag.Args()

    ctx, _ := signal.NotifyContext(context.Background(), syscall.SIGINT)
    // ctx, stop := context.WithCancel(ctx)

    var ffmpeg FFMpeg
    var wg sync.WaitGroup
    codecTypes := make(chan webrtc.RTPCodecType, 2)

    room := lksdk.NewRoom(&lksdk.RoomCallback{
        ParticipantCallback: lksdk.ParticipantCallback{
            OnTrackSubscribed: func(track *webrtc.TrackRemote, publication *lksdk.RemoteTrackPublication, rp *lksdk.RemoteParticipant) {
                trackID := track.ID()
                mediaType := track.Kind()

                // Add input to FFMpeg and get an RTPWriter
                writer, err := ffmpeg.TrackPipe()
                if err != nil {
                    log.Printf("Failed to add track %s: %v", trackID, err)
                    return
                }
                log.Printf("Track subscribed: %s (Kind: %s)", trackID, mediaType.String())

                // Schedule CopyRTP using WaitGroup.Go (starts the goroutine immediately, pipe buffering handles timing)
                wg.Go(func() {
                    defer writer.Close()
                    if _, err := io.Copy(writer, &TrackReader{track}); err != nil {
                        log.Printf("Copy failed for track %s: %v", trackID, err)
                    }
                })

                codecTypes <- mediaType
            },
        },
    })
    context.AfterFunc(ctx, room.Disconnect)

    if err := room.JoinWithToken(*url, *token); err != nil {
        log.Fatalf("Failed to connect to livekit room: %v", err)
    }

    func() {
        var (
            haveAudio bool
            haveVideo bool
        )
        for !haveAudio || !haveVideo {
            select {
            case <-ctx.Done():
                return
            case codecType := <-codecTypes:
                switch codecType {
                case webrtc.RTPCodecTypeAudio:
                    haveAudio = true
                case webrtc.RTPCodecTypeVideo:
                    haveVideo = true
                }
            }
        }
    }()

    if err := ffmpeg.StartContext(ctx, ffmpegArgs); err != nil {
        log.Fatalf("Failed to start FFmpeg: %v", err)
    }

    // Wait for all track processing to complete
    ffmpeg.Wait()
}
