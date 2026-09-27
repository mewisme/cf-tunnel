package hello

import (
	"net"

	"github.com/rs/zerolog"
)

func CreateTLSListener(addr string) (net.Listener, error) {
	return net.Listen("tcp", addr)
}

func StartHelloWorldServer(_ *zerolog.Logger, listener net.Listener, shutdownC <-chan struct{}) {
	<-shutdownC
	_ = listener.Close()
}
