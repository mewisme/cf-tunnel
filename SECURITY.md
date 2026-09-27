# Security Policy

Please report security issues privately through the GitHub repository's
security advisory feature rather than opening a public issue.

Quick Tunnel credentials and raw provisioning responses must never be exposed
through logs, lifecycle events, or returned diagnostic strings. Changes to
provisioning, transport, error handling, or upstream Cloudflared-derived code
should preserve the secret-redaction tests in this repository.