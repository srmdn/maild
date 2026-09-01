# AGENTS.md

Guide for humans and AI tools contributing to `maild`.

## Project

`maild` is a self-hosted outbound email control plane: API → queue → worker →
Bring-Your-Own SMTP, with retries/backoff, auto-failover, suppression,
unsubscribe, signed webhooks, domain readiness, and metering. It is not a mail
server (no IMAP/POP/webmail) nor an ESP marketing suite. Self-hosted OSS under
AGPL; revenue from sponsors/support. No tags or releases before v1.0 (see
`RELEASING.md`, `ROADMAP.md`).

## Stack

Go (1.27) with `cmd/server` as the single entry point; PostgreSQL + Redis;
server-rendered `html/template` UI in `internal/web` (no Node/build chain).

## Where to look

- Layout and entry point: `cmd/server`, `internal/` (subpackages are
  self-describing: `api`, `auth`, `service`, `worker`, `queue`, ...).
- Build/run/test/verify: `Makefile` (targets include `setup`, `run`, `verify`,
  `test-store`, `vuln`).
- Config and env surface: `internal/config/config.go`, `.env.example`,
  `.env.production.example`, and `deploy/production-config.md`.
- Product/API/roadmap: `README.md`, `ROADMAP.md`, `RELEASING.md`.

## Project-specific constraints

- `APP_ENV=production` fails fast on missing or development-default values.
- Auth: `APP_ALLOW_SIGNUP` (default off in production), `APP_LOGIN_PATH`
  (login page + sign-in; keep it a random secret, out of docs/commits), and
  `APP_LOGIN_MAX_FAILURES`/`_FAILURE_WINDOW`/`_LOCKOUT` (per-IP brute-force
  limit).
- Server-rendered UI; prefer templates over client-side frameworks.
- Normalize emails case-insensitively.
- Never commit a real `.env`. The app runs behind nginx, so trust
  `X-Forwarded-For`/`X-Real-IP` only from that proxy.
- Apply `gofmt`; `make verify` is the gate.

## Review expectations (mail pipeline)

- rate-limit/backoff behavior
- suppression list enforcement
- unsubscribe behavior
- bounce/complaint handling
- absence of credential leaks in logs

## Operating rules

1. Humans own final decisions, reviews, and commits.
2. AI output must be validated for correctness, security, and licensing.
3. Keep changes small, traceable, and reversible.
4. No AI branding or co-author trailers in commit messages.
5. Never paste production secrets, customer data, or private keys into
   external AI tools.
6. If requirements are unclear, ask one concise question instead of guessing.

## Commit ownership

Human-authored commit messages. Use `scripts/check-commit-attribution.sh`
before pushing.

Refer to the shared skills (Git change management, secret remediation,
continuity, repository hygiene) rather than restating them here.
