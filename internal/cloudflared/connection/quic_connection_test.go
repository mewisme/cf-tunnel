package connection

import (
	"bytes"
	"io"
	"sync"
	"testing"
)

type mockBidirectionalStream struct {
	mu        sync.Mutex
	buf       bytes.Buffer
	closed    bool
	closeRead int
	readDone  chan struct{}
}

func (s *mockBidirectionalStream) Read([]byte) (int, error) {
	<-s.readDone
	return 0, io.EOF
}

func (s *mockBidirectionalStream) Write(data []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return 0, io.ErrClosedPipe
	}
	return s.buf.Write(data)
}

func (s *mockBidirectionalStream) CloseRead() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closeRead++
	if s.closeRead == 1 {
		close(s.readDone)
	}
	return nil
}

func (s *mockBidirectionalStream) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	return nil
}

func TestQUICRequestBodyCloseOnlyCancelsRead(t *testing.T) {
	stream := &mockBidirectionalStream{readDone: make(chan struct{})}
	body := &nopCloserReadWriter{readWriteCloser: stream}
	done := make(chan error, 1)
	go func() {
		_, err := body.Read(make([]byte, 1))
		done <- err
	}()
	if err := body.Close(); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != io.EOF && err.Error() != "closed by handler" {
		t.Fatalf("read after close: %v", err)
	}
	if err := body.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := body.Write([]byte("response")); err != nil {
		t.Fatalf("response write after body close: %v", err)
	}
	stream.mu.Lock()
	defer stream.mu.Unlock()
	if stream.closed || stream.closeRead != 1 || stream.buf.String() != "response" {
		t.Fatalf("receive cancel=%d stream_closed=%t written=%q", stream.closeRead, stream.closed, stream.buf.String())
	}
}
