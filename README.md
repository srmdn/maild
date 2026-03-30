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

At startup, `maild` applies embedded `up` migrations automatically.

4. Check health:

```sh
curl -sS http://localhost:8080/healthz
```

5. Open Mailpit UI (local SMTP inbox):

```text
http://localhost:8025
```

## Current Endpoints

- `GET /`
- `GET /healthz`
- `GET /readyz`
- `POST /v1/messages`

Example:

```sh
curl -sS -X POST http://localhost:8080/v1/messages \
  -H "X-API-Key: change-me" \
  -H "Content-Type: application/json" \
  -d '{
    "workspace_id": 1,
    "from_email": "noreply@maild.local",
    "to_email": "user@example.com",
    "subject": "Hello from maild",
    "body_text": "maild first delivery test"
  }'
```

`/v1/*` endpoints require API key authentication using `API_KEY_HEADER` and `API_KEY`.

## Architecture

- [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md)
- [docs/release-risk-checklist.md](docs/release-risk-checklist.md)
- [docs/PUBLIC-LAUNCH-CHECKLIST.md](docs/PUBLIC-LAUNCH-CHECKLIST.md)

## License

GNU Affero General Public License v3.0 (AGPL-3.0). See [LICENSE](LICENSE).

## Governance Docs

- [CONTRIBUTING.md](CONTRIBUTING.md)
- [AGENTS.md](AGENTS.md)
- [CLAUDE.md](CLAUDE.md)
- [SECURITY.md](SECURITY.md)
- [docs/AI-WORKFLOW.md](docs/AI-WORKFLOW.md)
- [docs/release-risk-checklist.md](docs/release-risk-checklist.md)
