// Package cftunnel exposes a minimal embeddable Cloudflare Quick Tunnel
// client. One Start call owns one ephemeral tunnel to one HTTP(S) origin.
package cftunnel

import (
	"context"
	"errors"
	"net/http"

	"go.mewis.me/cf-tunnel/internal/cloudflared"
)

// DefaultQuickService is Cloudflare's anonymous Quick Tunnel provisioning
// endpoint.
const DefaultQuickService = cloudflared.DefaultQuickService

// Config configures either a Quick Tunnel or a Dashboard-managed tunnel.
type Config struct {
	// OriginURL is the HTTP(S) origin exposed through the tunnel.
	// Set either OriginURL or Handler, not both.
	OriginURL string
	// Handler serves requests in-process without opening a local HTTP listener.
	// With Token, it replaces Dashboard HTTP(S)/WebSocket origins while
	// preserving Dashboard hostname/path matches and HTTP status fallbacks.
	Handler http.Handler
	// Token connects an existing Dashboard-managed Cloudflare Tunnel.
	// Managed tunnel ingress rules are received from Cloudflare. OriginURL
	// cannot be combined with Token; Handler is optional in managed mode.
	Token string
	// QuickService overrides the provisioning service. Empty uses
	// DefaultQuickService. This is primarily useful for tests and compatible
	// private provisioning frontends.
	QuickService string
}

// Tunnel is one running ephemeral Quick Tunnel. Cancel the context passed to
// Start to stop it, then call Wait to observe completion.
type Tunnel struct {
	// URL is the ephemeral public https://*.trycloudflare.com URL for a
	// Quick Tunnel. Managed tunnels route via Dashboard-defined hostnames.
	URL string
	// ID is the managed tunnel UUID (empty for anonymous Quick Tunnels).
	ID string

	inner *cloudflared.Tunnel
}

// Start starts an anonymous Quick Tunnel or a Dashboard-managed tunnel.
func Start(ctx context.Context, cfg Config) (*Tunnel, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	inner, err := cloudflared.Start(ctx, cloudflared.Config{
		OriginURL:    cfg.OriginURL,
		Handler:      cfg.Handler,
		Token:        cfg.Token,
		QuickService: cfg.QuickService,
	})
	if err != nil {
		return nil, err
	}
	return &Tunnel{URL: inner.URL, ID: inner.ID, inner: inner}, nil
}

// TunnelID returns the managed tunnel UUID. Quick Tunnels return an empty ID.
func (t *Tunnel) TunnelID() string {
	if t == nil {
		return ""
	}
	return t.ID
}

// PublicURL returns the Quick Tunnel's ephemeral public URL, or "" for
// Dashboard-managed tunnels whose hostnames are configured remotely.
func (t *Tunnel) PublicURL() string {
	if t == nil {
		return ""
	}
	return t.URL
}

// EdgeLocation returns the Cloudflare edge location after successful
// registration. It is empty before a connection is established.
func (t *Tunnel) EdgeLocation() string {
	if t == nil || t.inner == nil {
		return ""
	}
	return t.inner.EdgeLocation()
}

// Wait blocks until the tunnel stops and returns the terminal runtime error.
func (t *Tunnel) Wait() error {
	if t == nil || t.inner == nil {
		return errors.New("quick tunnel is not initialized")
	}
	return t.inner.Wait()
}

// WaitUntilReady blocks until the first Cloudflare edge connection is ready,
// the tunnel exits, or ctx is canceled.
func (t *Tunnel) WaitUntilReady(ctx context.Context) error {
	if t == nil || t.inner == nil {
		return errors.New("quick tunnel is not initialized")
	}
	return t.inner.WaitUntilReady(ctx)
}
