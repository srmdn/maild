# maild

Outbound email operations control plane for teams that want reliable sending without heavyweight ESP lock-in.

## Manifesto

Sending email is easy. Operating email safely at scale is hard.

`maild` exists to make outbound delivery operations auditable and reliable:
- queue first, send safely
- enforce suppression and unsubscribe rules everywhere
- keep failure handling explicit (retries, replay, incident context)
- let operators see what happened and act fast

`maild` is intentionally focused. It is not trying to be a full CRM or marketing automation suite.

## What Problem `maild` Solves

Most teams end up with ad-hoc scripts plus provider dashboards. That creates blind spots:
- retries and rate limits are inconsistent
- suppression/unsubscribe enforcement is fragile
- webhook failures are hard to recover from
- incidents are slow to triage because logs and context are fragmented

`maild` centralizes those concerns in one control plane.

## What `maild` Is (And Is Not)

`maild` is:
- outbound send orchestration (API -> queue -> worker)
- SMTP provider control with failover-aware operations
- policy and compliance safety layer
- operator console for logs, timeline, and incident workflows

`maild` is not:
- inbox hosting (no IMAP/POP/webmail, no mailbox provisioning)
- a mail server (it sends through an existing SMTP relay; it is not a Postfix/Dovecot stack)
- mass / cold-email tooling (transactional & conversational only, to respect relay provider AUP)
- a complete ESP marketing suite (yet)

## Current State (pre-1.0)

- Outbound control-plane core is implemented and tested.
- API, queue/worker, retries/backoff, safety checks, and signed webhooks are in place.
- User auth, RBAC, per-user API keys, and a server-rendered UI exist.
- Operator console: `/ui`, `/ui/logs`, `/ui/onboarding`, `/ui/incidents`, and `/ui/policy`.
- Pre-1.0: no tags or releases until the v1.0 gate (see [RELEASING.md](RELEASING.md)).

## Production Profile

- Use [`.env.production.example`](.env.production.example) as the baseline for production deployments.
- `APP_ENV=production` now enforces strict startup validation and fails fast when required runtime values are missing or still using development defaults.
- Ownership and rotation expectations are documented in [`deploy/production-config.md`](deploy/production-config.md).

## Public Roadmap

Short- to mid-term plan lives in [ROADMAP.md](ROADMAP.md); release/version policy lives in [RELEASING.md](RELEASING.md).

## Stack

- Go (`cmd/server`, `internal/*`)
- PostgreSQL
- Redis
- Server-rendered web UI (no Node build chain)

## Quick Start

1. Bootstrap development:

```sh
make setup
```

2. Run server:

```sh
make run
```

3. Health check:

```sh
curl -sS http://localhost:8080/healthz
```

4. Local SMTP inbox (Mailpit):

```text
http://localhost:8025
```

## First Send (end-to-end)

Using the operator API key from `.env` (`ADMIN_API_KEY` / `OPERATOR_API_KEY`):

1. Register a local SMTP relay (in development, Mailpit at `localhost:1025` accepts without auth):

   ```sh
   curl -s -X POST http://localhost:8080/v1/smtp-accounts \
     -H 'X-API-Key: change-me-admin' -H 'Content-Type: application/json' \
     -d '{"workspace_id":1,"name":"mailpit","host":"localhost","port":1025,"from_email":"noreply@maild.local"}'
   ```

2. Queue a message:

   ```sh
   curl -s -X POST http://localhost:8080/v1/messages \
     -H 'X-API-Key: change-me-admin' -H 'Content-Type: application/json' \
     -d '{"workspace_id":1,"from_email":"noreply@maild.local","to_email":"you@example.com","subject":"Hi","body_text":"Hello from maild"}'
   ```

3. The worker sends it via the configured SMTP account. Confirm delivery in Mailpit at `http://localhost:8025`, or inspect the attempt/log:

   ```sh
   curl -s "http://localhost:8080/v1/messages/logs?workspace_id=1" -H 'X-API-Key: change-me-admin'
   curl -s "http://localhost:8080/v1/messages/timeline?message_id=1" -H 'X-API-Key: change-me-admin'
   ```

## Core API Surface

Authenticated with an `X-API-Key` header (`ADMIN_API_KEY` / `OPERATOR_API_KEY` from `.env`):

Message & delivery:
- `POST /v1/messages`
- `POST /v1/messages/retry`
- `GET /v1/messages/logs`
- `GET /v1/messages/timeline`

SMTP accounts:
- `POST /v1/smtp-accounts`
- `POST /v1/smtp-accounts/activate`
- `POST /v1/smtp-accounts/validate`
- `GET /v1/smtp-accounts/list`

Compliance & safety:
- `POST /v1/suppressions`
- `POST /v1/unsubscribes`
- `GET/POST /v1/workspaces/policy`
- `POST /v1/domains/readiness`

Webhooks:
- `POST /v1/webhooks/events` (only when `WEBHOOKS_ENABLED=true`)
- `GET /v1/webhooks/logs`
- `POST /v1/webhooks/replay`

Operational:
- `GET /v1/ops/onboarding-checklist`
- `GET /v1/incidents/bundle`
- `GET /v1/analytics/summary`
- `GET /v1/analytics/export.csv`
- `GET /v1/billing/metering`

User/auth routes (session-based):
- `GET /` (landing page / JSON build info)
- `GET /signup`, `GET /login`, `GET /logout`, `GET /me`
- `POST /api/v1/auth/signup`, `POST /api/v1/auth/login`
- `GET /api/v1/onboarding/checklist`
- `GET/POST/DELETE /api/v1/user/keys*`
- `GET /dashboard` (requires login)

Operator routes (require auth):
- `GET /ui`
- `GET /ui/logs`
- `GET /ui/onboarding`
- `GET /ui/incidents`
- `GET /ui/policy`

Health:
- `GET /healthz`, `GET /readyz`

## Security And Safety Defaults

- API key auth for `/v1/*`
- role separation (`admin` vs `operator`)
- encrypted SMTP credentials at rest (AES-GCM)
- workspace/domain rate limits
- blocked-recipient domain policy
- suppression and unsubscribe enforcement
- signed webhook verification (when enabled)

## Verification

Before merging:

```sh
make verify
```

For a security-inclusive local pass:

```sh
make verify-full
```

Store integration tests against a real Postgres (skipped automatically when `MAILD_TEST_DSN` is unset; requires the dev Postgres from `make setup` plus a `maild_test` database):

```sh
make test-store
```

## Governance

- [CONTRIBUTING.md](CONTRIBUTING.md)
- [AGENTS.md](AGENTS.md)
- [SECURITY.md](SECURITY.md)

## License

GNU Affero General Public License v3.0 (AGPL-3.0). See [LICENSE](LICENSE).
