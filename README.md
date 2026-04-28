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
- inbox hosting (no IMAP/POP/webmail)
- a complete ESP marketing suite (yet)

## Current State (April 28, 2026)

- Stable v0.x control-plane core is implemented.
- API, queue/worker, retries, safety checks, and signed webhooks are in place.
- User-facing auth and dashboard exist.
- Operator UI exists at `/ui`, `/ui/logs`, `/ui/onboarding`, `/ui/incidents`, and `/ui/policy`.

## Production Profile

- Use [`.env.production.example`](.env.production.example) as the baseline for production deployments.
- `APP_ENV=production` now enforces strict startup validation and fails fast when required runtime values are missing or still using development defaults.
- Ownership and rotation expectations are documented in [`deploy/production-config.md`](deploy/production-config.md).

## Public Roadmap

Roadmap execution is tracked in GitHub milestones/issues:

- `v0.6.0` Production hardening baseline
  - [#14](https://github.com/srmdn/maild/issues/14) tracker
  - [#15](https://github.com/srmdn/maild/issues/15) production env and config validation
  - [#16](https://github.com/srmdn/maild/issues/16) deploy baseline and runtime topology
  - [#17](https://github.com/srmdn/maild/issues/17) preflight release gate
- `v0.7.0` Campaign composer MVP
  - [#18](https://github.com/srmdn/maild/issues/18) campaign model and API
  - [#19](https://github.com/srmdn/maild/issues/19) composer UI + preview + test-send
- `v0.8.0` Audience builder MVP
  - [#20](https://github.com/srmdn/maild/issues/20) import pipeline and compliance-aware filtering
  - [#21](https://github.com/srmdn/maild/issues/21) audience UI and basic segmentation
- `v0.9.0` Ops and onboarding maturity
  - [#22](https://github.com/srmdn/maild/issues/22) observability and alerting baseline
  - [#12](https://github.com/srmdn/maild/issues/12) workspace invitation flow
  - [#13](https://github.com/srmdn/maild/issues/13) design partner onboarding program
- `v1.0.0` GA release gate
  - [#23](https://github.com/srmdn/maild/issues/23) full end-to-end QA matrix

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

## Core API Surface

- `POST /v1/messages`
- `POST /v1/messages/retry`
- `POST /v1/webhooks/events`
- `GET /v1/webhooks/logs`
- `POST /v1/webhooks/replay`
- `POST /v1/smtp-accounts`
- `GET /v1/smtp-accounts/list`
- `POST /v1/smtp-accounts/activate`
- `GET/POST /v1/workspaces/policy`
- `GET /v1/messages/logs`
- `GET /v1/messages/timeline`
- `GET /v1/incidents/bundle`

User/auth routes:
- `GET /`
- `GET/POST /signup`
- `GET/POST /login`
- `GET /dashboard`

Operator routes:
- `GET /ui`
- `GET /ui/logs`
- `GET /ui/onboarding`
- `GET /ui/incidents`
- `GET /ui/policy`

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

## Governance

- [CONTRIBUTING.md](CONTRIBUTING.md)
- [AGENTS.md](AGENTS.md)
- [CLAUDE.md](CLAUDE.md)
- [SECURITY.md](SECURITY.md)

## License

GNU Affero General Public License v3.0 (AGPL-3.0). See [LICENSE](LICENSE).
