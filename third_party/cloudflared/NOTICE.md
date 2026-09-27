# Cloudflared Quick Tunnel attribution

`cf-tunnel` contains a deliberately limited and modified Cloudflare
`cloudflared` Quick Tunnel keep-set. This directory records the upstream
provenance for that implementation.

- Upstream: `github.com/cloudflare/cloudflared`
- Tag: `2026.9.1`
- Commit: `f11dea9cb7079e90a982c1a2d5548ab40847fdcf`
- Annotated tag object: `be5825661dca56de70b5493ece66237f290427e3`
- Official linux-amd64 checksum: `03f1f25d1cc93b9ad6c60569d44060bc4f17ed97075760ed8cfca4b12dcd68cc`
- License: Apache License 2.0; see `LICENSE`.
- Upstream-matching QUIC override: `github.com/chungthuang/quic-go v0.45.1-0.20260529212404-a9fddf436fc4` replacing `github.com/quic-go/quic-go`.

The extraction and provenance notes are recorded in `UPSTREAM.md`. The retained
implementation is used by both the standalone binary and the public Go module.

The retained product behavior is limited to anonymous Quick Tunnel provisioning, one HTTP origin per connection, QUIC transport, reconnect behavior, ephemeral public URL reporting, and the minimum upstream-derived implementation required for those behaviors. `cmd/cloudflared`, updater/login/service-management, generic Cloudflare CLI surfaces, and Sentry are explicitly out of scope.
