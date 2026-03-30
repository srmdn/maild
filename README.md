# maild

Lightweight outbound email operations platform.

`maild` focuses on reliable sending workflows for transactional email and simple campaigns:
- queueing and controlled delivery
- retries and failure handling
- suppression and unsubscribe safety
- domain verification and deliverability checks
- delivery event logs and webhooks

It is not a full mailbox server and does not include IMAP/POP webmail.

## Project Status

Bootstrap initialized. Core app scaffold exists, but message queue/worker delivery is not implemented yet.

## Stack

- Go (`cmd/server`, `internal/*`)
- PostgreSQL
- Redis
- Server-rendered/web-first direction (no Node build chain)

## Quick Start

1. Copy environment defaults:

```sh
cp .env.example .env
```

2. Start local dependencies:

```sh
docker compose up -d
```

3. Run the app:

```sh
make run
```

4. Check health:

```sh
curl -sS http://localhost:8080/healthz
```

## Current Endpoints

- `GET /`
- `GET /healthz`
- `GET /readyz`

## Architecture

- [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md)
- [docs/release-risk-checklist.md](docs/release-risk-checklist.md)

## License

GNU Affero General Public License v3.0 (AGPL-3.0). See [LICENSE](LICENSE).

## Governance Docs

- [CONTRIBUTING.md](CONTRIBUTING.md)
- [AGENTS.md](AGENTS.md)
- [CLAUDE.md](CLAUDE.md)
- [SECURITY.md](SECURITY.md)
- [docs/AI-WORKFLOW.md](docs/AI-WORKFLOW.md)
- [docs/release-risk-checklist.md](docs/release-risk-checklist.md)
