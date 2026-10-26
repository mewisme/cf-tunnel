package ingress

import (
	"encoding/json"
	"net/http"

	"github.com/rs/zerolog"
)

type handlerService struct {
	http.Handler
}

// NewHandlerService makes an in-process HTTP handler available to the tunnel.
func NewHandlerService(handler http.Handler) OriginService {
	return &handlerService{Handler: handler}
}

func (*handlerService) String() string {
	return "in-memory"
}

func (*handlerService) start(_ *zerolog.Logger, _ <-chan struct{}, _ OriginRequestConfig) error {
	return nil
}

func (s *handlerService) MarshalJSON() ([]byte, error) {
	return json.Marshal(s.String())
}
