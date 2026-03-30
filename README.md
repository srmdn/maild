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
  -H "X-API-Key: change-me-operator" \
  -H "Content-Type: application/json" \
  -d '{
    "workspace_id": 1,
    "from_email": "noreply@maild.local",
    "to_email": "user@example.com",
    "subject": "Hello from maild",
    "body_text": "maild first delivery test"
  }'
```

Admin-only suppression example:

```sh
curl -sS -X POST http://localhost:8080/v1/suppressions \
  -H "X-API-Key: change-me-admin" \
  -H "Content-Type: application/json" \
  -d '{
    "workspace_id": 1,
    "email": "user@example.com",
    "reason": "manual block"
  }'
```

Admin-only unsubscribe example:

```sh
curl -sS -X POST http://localhost:8080/v1/unsubscribes \
  -H "X-API-Key: change-me-admin" \
  -H "Content-Type: application/json" \
  -d '{
    "workspace_id": 1,
    "email": "user@example.com",
    "reason": "user clicked unsubscribe"
  }'
```

Domain readiness check example (SPF/DKIM/DMARC):

```sh
curl -sS -X POST http://localhost:8080/v1/domains/readiness \
  -H "X-API-Key: change-me-operator" \
  -H "Content-Type: application/json" \
  -d '{
    "workspace_id": 1,
    "domain": "maild.click",
    "dkim_selector": "default"
  }'
```

Admin-only encrypted SMTP account config:

```sh
curl -sS -X POST http://localhost:8080/v1/smtp-accounts \
  -H "X-API-Key: change-me-admin" \
  -H "Content-Type: application/json" \
  -d '{
    "workspace_id": 1,
    "name": "mxroute-main",
    "host": "smtp.mxroute.com",
    "port": 587,
    "username": "user@example.com",
    "password": "secret",
    "from_email": "noreply@maild.click"
  }'
```

Admin-only SMTP provider validation:

```sh
curl -sS -X POST http://localhost:8080/v1/smtp-accounts/validate \
  -H "X-API-Key: change-me-admin" \
  -H "Content-Type: application/json" \
  -d '{"workspace_id":1}'
```

Operator message logs view:

```sh
curl -sS "http://localhost:8080/v1/messages/logs?workspace_id=1&limit=20" \
  -H "X-API-Key: change-me-operator"
```

Operator message timeline view:

```sh
curl -sS "http://localhost:8080/v1/messages/timeline?message_id=1" \
  -H "X-API-Key: change-me-operator"
```

`/v1/*` endpoints require API key authentication using:
- `API_KEY_HEADER`
- `ADMIN_API_KEY`
- `OPERATOR_API_KEY`

SMTP account credentials saved through API are encrypted at rest in PostgreSQL using AES-GCM (`ENCRYPTION_KEY_BASE64`).

Basic anti-abuse controls are enabled:
- hourly workspace rate limit (`RATE_LIMIT_WORKSPACE_PER_HOUR`)
- hourly recipient-domain rate limit (`RATE_LIMIT_DOMAIN_PER_HOUR`)
- blocked recipient domain list (`BLOCKED_RECIPIENT_DOMAINS`)

## Architecture

- [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md)
- [docs/release-risk-checklist.md](docs/release-risk-checklist.md)
- [docs/PUBLIC-LAUNCH-CHECKLIST.md](docs/PUBLIC-LAUNCH-CHECKLIST.md)
- [docs/BACKUP-RESTORE.md](docs/BACKUP-RESTORE.md)

## License

GNU Affero General Public License v3.0 (AGPL-3.0). See [LICENSE](LICENSE).

## Governance Docs

- [CONTRIBUTING.md](CONTRIBUTING.md)
- [AGENTS.md](AGENTS.md)
- [CLAUDE.md](CLAUDE.md)
- [SECURITY.md](SECURITY.md)
- [docs/AI-WORKFLOW.md](docs/AI-WORKFLOW.md)
- [docs/release-risk-checklist.md](docs/release-risk-checklist.md)
