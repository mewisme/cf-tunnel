// Package cftunnel exposes a minimal embeddable Cloudflare Quick Tunnel
// client. One Start call owns one ephemeral tunnel to one HTTP(S) origin.
package cftunnel

import (
	"context"
	"errors"

	"github.com/mewisme/cf-tunnel/internal/cloudflared"
)

// DefaultQuickService is Cloudflare's anonymous Quick Tunnel provisioning
// endpoint.
const DefaultQuickService = cloudflared.DefaultQuickService

// Config configures one ephemeral Quick Tunnel.
type Config struct {
	// OriginURL is the HTTP(S) origin exposed through the tunnel.
	OriginURL string
	// QuickService overrides the provisioning service. Empty uses
	// DefaultQuickService. This is primarily useful for tests and compatible
	// private provisioning frontends.
	QuickService string
}

// Tunnel is one running ephemeral Quick Tunnel. Cancel the context passed to
// Start to stop it, then call Wait to observe completion.
type Tunnel struct {
	// URL is the ephemeral public https://*.trycloudflare.com URL.
	URL string

	inner *cloudflared.Tunnel
}

// Start provisions and starts one Quick Tunnel to cfg.OriginURL.
func Start(ctx context.Context, cfg Config) (*Tunnel, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	inner, err := cloudflared.Start(ctx, cloudflared.Config{
		OriginURL:    cfg.OriginURL,
		QuickService: cfg.QuickService,
	})
	if err != nil {
		return nil, err
	}
	return &Tunnel{URL: inner.URL, inner: inner}, nil
}

// PublicURL returns the ephemeral public URL. It exists alongside URL so
// process supervisors can depend on a small interface rather than this type.
func (t *Tunnel) PublicURL() string {
	if t == nil {
		return ""
	}
	return t.URL
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
