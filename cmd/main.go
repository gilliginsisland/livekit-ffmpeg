package main

import (
    "context"
    "errors"
    "flag"
    "fmt"
    "io"
    "log"
    "net"
    "os"
    "os/exec"
    "os/signal"
    "strings"
    "sync"
    "syscall"

    lksdk "github.com/livekit/server-sdk-go/v2"
    "github.com/pion/rtp"
    "github.com/pion/sdp/v3"
    "github.com/pion/webrtc/v4"
)

// FFMpeg manages the FFmpeg process and input file descriptors
type FFMpeg struct {
    cmd *exec.Cmd
    SDP string
}

// Start constructs and starts the FFmpeg process with the configured inputs and user-provided extraArgs
func (f *FFMpeg) StartContext(ctx context.Context, outputArgs []string) error {
    args := append([]string{
        "-protocol_whitelist",
        "udp,rtp,pipe",
        "-i", "pipe:0",
    }, outputArgs...)

    f.cmd = exec.CommandContext(ctx, "ffmpeg", args...)
    f.cmd.Stdout = os.Stdout
    f.cmd.Stderr = os.Stderr
    f.cmd.Stdin = strings.NewReader(f.SDP)

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

    // Prepare udp conns
    // Also update incoming packets with expected PayloadType, the browser may use
    // a different value. We have to modify so our stream matches what rtp-forwarder.sdp expects
    conns := map[webrtc.RTPCodecType]*RTPConn{
        webrtc.RTPCodecTypeAudio: {Port: 4000, Type: 111},
        webrtc.RTPCodecTypeVideo: {Port: 4002, Type: 96},
    }
    for _, conn := range conns {
        var err error
        if conn.Conn, err = net.Dial("udp", fmt.Sprintf("127.0.0.1:%d", conn.Port)); err != nil {
            panic(err)
        }
        defer func(conn net.Conn) {
            if err := conn.Close(); err != nil {
                panic(err)
            }
        }(conn.Conn)
    }

    s := &sdp.SessionDescription{
        Version: 0,
        Origin: sdp.Origin{
            Username:       "-",
            SessionID:      0,
            SessionVersion: 0,
            NetworkType:    "IN",
            AddressType:    "IP4",
            UnicastAddress: "127.0.0.1",
        },
        ConnectionInformation: &sdp.ConnectionInformation{
            NetworkType: "IN",
            AddressType: "IP4",
            Address:     &sdp.Address{Address: "127.0.0.1"},
        },
        SessionName: "LiveKit WebRTC",
        TimeDescriptions: []sdp.TimeDescription{
            {Timing: sdp.Timing{StartTime: 0, StopTime: 0}},
        },
    }

    var (
        mu   sync.Mutex
        cond = sync.NewCond(&mu)
    )
    context.AfterFunc(ctx, cond.Signal)

    var wg sync.WaitGroup

    room := lksdk.NewRoom(&lksdk.RoomCallback{
        ParticipantCallback: lksdk.ParticipantCallback{
            OnTrackSubscribed: func(track *webrtc.TrackRemote, publication *lksdk.RemoteTrackPublication, rp *lksdk.RemoteParticipant) {
                conn, ok := conns[track.Kind()]
                if !ok {
                    return
                }

                params := track.Codec()
                _, encoding, _ := strings.Cut(params.MimeType, "/")
                if encoding == "" {
                    return
                }
                encoding = strings.ToUpper(encoding)

                var rtpmap string
                switch track.Kind() {
                case webrtc.RTPCodecTypeAudio:
                    rtpmap = fmt.Sprintf("%d %s/%d/%d", conn.Type, encoding, params.ClockRate, params.Channels)
                case webrtc.RTPCodecTypeVideo:
                    rtpmap = fmt.Sprintf("%d %s/%d", conn.Type, encoding, params.ClockRate)
                }

                md := &sdp.MediaDescription{
                    MediaName: sdp.MediaName{
                        Media:   track.Kind().String(),
                        Port:    sdp.RangedPort{Value: conn.Port},
                        Protos:  []string{"RTP", "AVP"},
                        Formats: []string{fmt.Sprintf("%d", conn.Type)},
                    },
                    Attributes: []sdp.Attribute{
                        {Key: "rtpmap", Value: rtpmap},
                    },
                }
                if params.SDPFmtpLine != "" {
                    md.WithValueAttribute("fmtp", fmt.Sprintf("%d %s", conn.Type, params.SDPFmtpLine))
                }
                mu.Lock()
                s.WithMedia(md)
                mu.Unlock()
                cond.Signal()

                wg.Go(func() {
                    buf := make([]byte, 1500)
                    pkt := &rtp.Packet{}

                    for {
                        // Read
                        n, _, err := track.Read(buf)
                        if err == io.EOF {
                            break
                        }
                        if err != nil {
                            panic(err)
                        }

                        // Unmarshal the packet and update the PayloadType
                        if err = pkt.Unmarshal(buf[:n]); err != nil {
                            panic(err)
                        }
                        pkt.PayloadType = conn.Type

                        // Marshal into original buffer with updated PayloadType
                        if n, err = pkt.MarshalTo(buf); err != nil {
                            panic(err)
                        }

                        // Write
                        if _, err = conn.Write(buf[:n]); err != nil {
                            // For this particular example, third party applications usually timeout after a short
                            // amount of time during which the user doesn't have enough time to provide the answer
                            // to the browser.
                            // That's why, for this particular example, the user first needs to provide the answer
                            // to the browser then open the third party application. Therefore we must not kill
                            // the forward on "connection refused" errors
                            var opError *net.OpError
                            if errors.As(err, &opError) && opError.Err.Error() == "write: connection refused" {
                                continue
                            }
                            panic(err)
                        }
                    }
                })
            },
        },
    })
    context.AfterFunc(ctx, room.Disconnect)

    if err := room.JoinWithToken(*url, *token); err != nil {
        log.Fatalf("Failed to connect to livekit room: %v", err)
    }

    mu.Lock()
    for len(s.MediaDescriptions) < 2 {
        cond.Wait()
    }
    var ffmpeg FFMpeg
    if b, err := s.Marshal(); err != nil {
        panic(err)
    } else {
        ffmpeg.SDP = string(b)
    }
    mu.Unlock()
    log.Println(ffmpeg.SDP)
    if err := ffmpeg.StartContext(ctx, ffmpegArgs); err != nil {
        log.Fatalf("Failed to start FFmpeg: %v", err)
    }

    // Wait for all track processing to complete
    ffmpeg.Wait()
}

type RTPConn struct {
    net.Conn
    Port int
    Type uint8
}
