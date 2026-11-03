# Changelog

## v0.1.3 — 2026-11-03

- Expose the last applied Dashboard-managed ingress routes through the Go SDK with `Tunnel.Routes()` and `Tunnel.WaitUntilConfigured()`.
- Add `cf-tunnel routes` to inspect hostname, path and local service mappings using a short-lived connector without a separate Cloudflare management API token.
- Preserve the original Dashboard service when serving HTTP(S) origins through `Config.Token` and an in-memory `Config.Handler`.
- Reflect accepted remote configuration updates atomically and retain the last good snapshot after rejected or stale changes.
- Redact origin credentials, query strings and Unix socket paths from reported routes; add readiness, update, safety and CLI tests.

## v0.1.2 — 2026-10-26

- Allow Go callers to combine `Config.Token` with `Config.Handler` for a Dashboard-managed, fully in-process HTTP origin.
- Apply Dashboard hostname/path rules and HTTP status fallbacks while substituting HTTP(S)/WebSocket origin routes with the supplied handler before starting services; no loopback HTTP listener or local origin dial is required.
- Process remote ingress updates independently per connector with atomic proxy replacement; reject non-HTTP remote origin updates and deny tunneled TCP forwarding in managed handler mode.
- Preserve existing Quick Tunnel, Quick Handler, and managed token-only behavior; support multiple concurrent connector instances.
- Add routing, isolation, update, SSE and fail-closed tests and document secure embedding.

## v0.1.1 — 2026-10-26

- Added offline managed connector token parsing and temporary Edge registration verification.

## v0.1.0 — 2026-10-26

- Initial release of standalone and embeddable Quick Tunnel runtime with Dashboard-managed token mode.
