package connection

import (
	"context"
	"net"
	"testing"

	"github.com/rs/zerolog"
	"io"
)

func TestObserverTracksRegisteredEdgeLocation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	log := zerolog.New(io.Discard)
	observer := NewObserverWithContext(ctx, &log)
	if got := observer.EdgeLocation(); got != "" {
		t.Fatalf("initial edge location=%q", got)
	}
	observer.sendConnectedEvent(0, QUIC, "SIN", net.IPv4(1, 1, 1, 1))
	if got := observer.EdgeLocation(); got != "SIN" {
		t.Fatalf("connected edge location=%q", got)
	}
	observer.sendConnectedEvent(0, QUIC, "HKG", net.IPv4(1, 1, 1, 2))
	if got := observer.EdgeLocation(); got != "HKG" {
		t.Fatalf("updated edge location=%q", got)
	}
}
