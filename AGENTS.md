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

Go (version in `go.mod`) with `cmd/server` as the single entry point;
PostgreSQL + Redis; server-rendered `html/template` UI in `internal/web`
(no Node/build chain).

## Where to look

- Layout and entry point: `cmd/server`, `internal/` (subpackages are
  self-describing: `api`, `auth`, `service`, `worker`, `queue`, ...).
- Build/run/test/verify: `Makefile` (targets include `setup`, `run`, `verify`,
  `test-store`, `test-auth`, `vuln`).
- Session/API authentication and workspace scope: `docs/authentication.md`.
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
- Never expose instance-wide API keys in HTML or browser JavaScript. Use
  sessions for the UI and workspace-scoped personal keys for user API access.
- Render untrusted browser data as text; avoid interpolating it into HTML.

## Validation

- Apply `gofmt` to changed Go files; run `make verify` for code changes.
- For auth, workspace, key, or related migration changes, also run
  `make test-auth`. Follow `docs/authentication.md` for the required environment
  variables and use separate disposable store/HTTP databases and test Redis.
  Integration tests can delete data; never point them at production.
- Use `make verify-full` when checking vulnerabilities too; `make verify` alone
  does not include that check. Report skipped dependency-backed tests and
  unresolved failures explicitly.
- For documentation-only changes, check changed text, referenced paths, and
  command accuracy; application tests are unnecessary unless behavior changed.

## Code Review Rules

- Flag workspace data access without ownership and role checks, including
  message timelines and omitted workspace IDs. Personal keys must use current
  membership and respect revocation; only explicit instance keys grant global
  access.
- Flag session writes without same-origin protection and any credential leaks
  in responses, templates, logs, or error messages.
- For mail pipeline changes, check rate limits and retry/backoff, suppression
  enforcement, unsubscribe behavior, and bounce/complaint handling. Flag changes
  that bypass these controls or retry permanently rejected messages.

## Working agreements

- Humans own scope, final review, and release decisions. Validate changes for
  correctness, security, and licensing; keep them traceable and reversible.
- When the user requests a commit, agents may stage the intended changes,
  write an accurate commit message, and create the commit. Commit authorization
  does not authorize pushing, releasing, or deploying; obtain explicit user
  authorization for those actions.
- Preserve unrelated local changes. Never commit real `.env` files, production
  secrets, customer data, private keys, or machine-specific operational state;
  never paste sensitive data into external AI tools.
- Do not add AI branding or co-author trailers to commit messages. Run
  `scripts/check-commit-attribution.sh` before pushing.
- Ask one concise question when ambiguity affects scope, security, or an
  irreversible action. Resolve routine implementation choices within the
  authorized scope without repeatedly asking for approval.

Use available shared skills when their triggers apply, including Git change
management, secret remediation, continuity, and repository hygiene. Keep reusable
workflow instructions there and repository-specific requirements here.
