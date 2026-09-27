package cloudflared

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/mewisme/cf-tunnel/internal/cloudflared/connection"
)

const (
	DefaultQuickService = "https://api.trycloudflare.com"
	userAgent           = "cf-tunnel/2026.9.1"
)

type QuickTunnelResponse struct {
	Success bool
	Result  QuickTunnel
	Errors  []QuickTunnelError
}

type QuickTunnelError struct {
	Code    int
	Message string
}

type QuickTunnel struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Hostname   string `json:"hostname"`
	AccountTag string `json:"account_tag"`
	Secret     []byte `json:"secret"`
}

type provision struct {
	credentials connection.Credentials
	hostname    string
	url         string
}

func parseProvisionResponse(status int, body []byte) (*provision, error) {
	if status < 200 || status >= 300 {
		var data QuickTunnelResponse
		if err := json.Unmarshal(body, &data); err == nil && len(data.Errors) > 0 {
			return nil, fmt.Errorf("quick tunnel provisioning failed with status %d: %s", status, formatQuickTunnelErrors(data.Errors))
		}
		return nil, fmt.Errorf("quick tunnel provisioning failed with status %d", status)
	}

	var data QuickTunnelResponse
	if err := json.Unmarshal(body, &data); err != nil {
		return nil, fmt.Errorf("failed to unmarshal quick Tunnel")
	}
	if len(data.Errors) > 0 {
		return nil, fmt.Errorf("quick tunnel provisioning failed: %s", formatQuickTunnelErrors(data.Errors))
	}
	if !data.Success {
		return nil, fmt.Errorf("quick tunnel provisioning failed")
	}
	tunnelID, err := uuid.Parse(data.Result.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to parse quick Tunnel ID")
	}
	if data.Result.Hostname == "" {
		return nil, fmt.Errorf("quick tunnel provisioning failed: missing hostname")
	}
	if len(data.Result.Secret) == 0 {
		return nil, fmt.Errorf("quick tunnel provisioning failed: missing secret")
	}

	url := data.Result.Hostname
	if !strings.HasPrefix(url, "https://") {
		url = "https://" + url
	}
	return &provision{
		credentials: connection.Credentials{
			AccountTag:   data.Result.AccountTag,
			TunnelSecret: data.Result.Secret,
			TunnelID:     tunnelID,
		},
		hostname: data.Result.Hostname,
		url:      url,
	}, nil
}

func formatQuickTunnelErrors(errors []QuickTunnelError) string {
	messages := make([]string, len(errors))
	for i, e := range errors {
		messages[i] = fmt.Sprintf("[%d] %s", e.Code, e.Message)
	}
	return strings.Join(messages, "; ")
}
