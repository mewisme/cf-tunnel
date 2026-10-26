# Changelog

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
