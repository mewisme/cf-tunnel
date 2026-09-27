package management

import "net/http"

const EventTypeKey = "event"

type LogEventType int8

const (
	Cloudflared LogEventType = iota
	HTTP
	TCP
	UDP
)

func (l LogEventType) String() string {
	switch l {
	case Cloudflared:
		return "cloudflared"
	case HTTP:
		return "http"
	case TCP:
		return "tcp"
	case UDP:
		return "udp"
	default:
		return ""
	}
}

type ManagementService struct {
	Hostname string
}

func (m *ManagementService) ServeHTTP(http.ResponseWriter, *http.Request) {}
