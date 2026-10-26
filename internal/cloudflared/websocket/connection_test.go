package websocket

import (
	"bytes"
	"context"
	"io"
	"net"
	"testing"
	"time"

	"github.com/gobwas/ws/wsutil"
	"github.com/rs/zerolog"
)

func TestReadDoesNotTruncateLargeWebSocketFrames(t *testing.T) {
	for _, sizes := range [][]int{{100 * 1024}, {1, 16 * 1024, 16*1024 + 1, 50000, 7}} {
		server, client := net.Pipe()
		ctx, cancel := context.WithCancel(context.Background())
		logger := zerolog.New(io.Discard)
		conn := NewConn(ctx, server, &logger)
		_ = server.SetReadDeadline(time.Now().Add(5 * time.Second))
		expected := []byte{}
		frames := make([][]byte, 0, len(sizes))
		for _, n := range sizes {
			frame := bytes.Repeat([]byte{byte(n % 251)}, n)
			frames = append(frames, frame)
			expected = append(expected, frame...)
		}
		writerDone := make(chan error, 1)
		go func() {
			for _, frame := range frames {
				if err := wsutil.WriteClientBinary(client, frame); err != nil {
					writerDone <- err
					return
				}
			}
			writerDone <- nil
		}()
		got := make([]byte, 0, len(expected))
		buf := make([]byte, 16*1024)
		for len(got) < len(expected) {
			n, err := conn.Read(buf)
			if err != nil {
				t.Fatal(err)
			}
			got = append(got, buf[:n]...)
		}
		if err := <-writerDone; err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, expected) {
			t.Fatalf("websocket payload was truncated, got %d bytes instead of %d", len(got), len(expected))
		}
		cancel()
		conn.Close()
		_ = server.Close()
		_ = client.Close()
	}
}
