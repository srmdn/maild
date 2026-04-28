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

Actively used v0.x control plane:
- API -> queue -> worker delivery is implemented
- operator console workflows are available at `/ui`, `/ui/logs`, `/ui/onboarding`, `/ui/incidents`, and `/ui/policy`
- backend-first technical scope (Tracks A/B/C) is complete as of April 3, 2026
- next focus is frontend product UX for broader operator workflows

## Stack

- Go (`cmd/server`, `internal/*`)
- PostgreSQL
- Redis
- Server-rendered/web-first direction (no Node build chain)

## Quick Start

1. Bootstrap local development in one command:

```sh
make setup
```

2. Run the app:

```sh
make run
```

At startup, `maild` applies embedded `up` migrations automatically.

3. Check health:

```sh
curl -sS http://localhost:8080/healthz
```

4. Open Mailpit UI (local SMTP inbox):

```text
http://localhost:8025
```

## Current Endpoints

- `GET /`
- `GET /healthz`
- `GET /readyz`
- `POST /v1/messages`
- `POST /v1/messages/retry`
- `GET /v1/ops/onboarding-checklist`
- `GET /v1/incidents/bundle`
- `POST /v1/webhooks/events` (only when `WEBHOOKS_ENABLED=true`)
- `GET /v1/webhooks/logs`
- `POST /v1/webhooks/replay`
- `GET /v1/smtp-accounts/list`
- `POST /v1/smtp-accounts/activate`
- `GET/POST /v1/workspaces/policy`
- `GET /ui/policy`
- `GET /ui`
- `GET /ui/onboarding`
- `GET /ui/incidents`
- `GET /v1/analytics/summary`
- `GET /v1/analytics/export.csv`
- `GET /v1/billing/metering`
- `GET /ui/logs` (operator console: logs/timeline, queue-state summary, domain readiness, suppression tools)

Example:

```sh
curl -sS -X POST http://localhost:8080/v1/messages \
  -H "X-API-Key: change-me-operator" \
  -H "Content-Type: application/json" \
  -d '{
    "workspace_id": 1,
    "from_email": "noreply@example.test",
    "to_email": "user@example.test",
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
    "email": "user@example.test",
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
    "email": "user@example.test",
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
    "domain": "example.test",
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
    "name": "primary-smtp",
    "host": "smtp.example.com",
    "port": 587,
    "username": "user@example.test",
    "password": "secret",
    "from_email": "noreply@example.test"
  }'
```

Admin-only SMTP provider validation:

```sh
curl -sS -X POST http://localhost:8080/v1/smtp-accounts/validate \
  -H "X-API-Key: change-me-admin" \
  -H "Content-Type: application/json" \
  -d '{"workspace_id":1}'
```

SMTP account list and manual active-provider switch:

```sh
curl -sS "http://localhost:8080/v1/smtp-accounts/list?workspace_id=1" \
  -H "X-API-Key: change-me-operator"

curl -sS -X POST http://localhost:8080/v1/smtp-accounts/activate \
  -H "X-API-Key: change-me-admin" \
  -H "Content-Type: application/json" \
  -d '{"workspace_id":1,"name":"primary-smtp"}'
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

Operator controlled retry:

```sh
curl -sS -X POST http://localhost:8080/v1/messages/retry \
  -H "X-API-Key: change-me-operator" \
  -H "Content-Type: application/json" \
  -d '{"workspace_id":1,"message_ids":[1]}'
```

Technical onboarding checklist:

```sh
curl -sS "http://localhost:8080/v1/ops/onboarding-checklist?workspace_id=1&domain=example.test&dkim_selector=default" \
  -H "X-API-Key: change-me-operator"
```

Incident bundle export (timeline, attempts, webhook outcomes):

```sh
curl -sS "http://localhost:8080/v1/incidents/bundle?workspace_id=1&message_id=1" \
  -H "X-API-Key: change-me-operator"
```

Provider webhook event ingest (signature required):

```sh
body='{"workspace_id":1,"type":"bounce","email":"user@example.test","reason":"hard_bounce"}'
ts="$(date +%s)"
sig="$(printf '%s.%s' "$ts" "$body" | openssl dgst -sha256 -hmac "$WEBHOOK_SIGNING_SECRET" -hex | sed 's/^.* //')"

curl -sS -X POST http://localhost:8080/v1/webhooks/events \
  -H "Content-Type: application/json" \
  -H "X-Webhook-Timestamp: $ts" \
  -H "X-Webhook-Signature: v1=$sig" \
  -d "$body"
```

Webhook payload compatibility:
- single event object (`workspace_id`, `type`/`event`, `email`/`recipient`, optional `reason`)
- event arrays (for provider batch delivery)
- common provider aliases map to internal types: `bounce`, `complaint`, `unsubscribe`

For mixed batches, the API returns `processed_count` and `rejected_count`.

Webhook reliability behavior:
- each webhook apply action uses bounded retries (`WEBHOOK_APPLY_MAX_ATTEMPTS`)
- malformed/unsupported streams are persisted as dead-letter webhook events for audit

Provider failover behavior:
- manual switch via `POST /v1/smtp-accounts/activate`
- automatic switch to standby account when active provider repeatedly fails (`AUTO_FAILOVER_*` settings)

Operator webhook logs view:

```sh
curl -sS "http://localhost:8080/v1/webhooks/logs?workspace_id=1&limit=20&status=dead_letter" \
  -H "X-API-Key: change-me-operator"
```

Tenant policy controls:

```sh
curl -sS "http://localhost:8080/v1/workspaces/policy?workspace_id=1" \
  -H "X-API-Key: change-me-operator"

curl -sS -X POST http://localhost:8080/v1/workspaces/policy \
  -H "X-API-Key: change-me-admin" \
  -H "Content-Type: application/json" \
  -d '{
    "workspace_id": 1,
    "rate_limit_workspace_per_hour": 600,
    "rate_limit_domain_per_hour": 250,
    "blocked_recipient_domains": ["mailinator.com", "tempmail.com"]
  }'
```

Policy UI:

```text
http://localhost:8080/ui/policy?workspace_id=1
```

Operator logs UI:

```text
http://localhost:8080/ui/logs?workspace_id=1
```

Operator dashboard UI:

```text
http://localhost:8080/ui?workspace_id=1
```

Operator onboarding UI:

```text
http://localhost:8080/ui/onboarding?workspace_id=1
```

Operator incidents UI:

```text
http://localhost:8080/ui/incidents?workspace_id=1
```

`/ui/logs` capabilities:
- queue-state snapshot from recent message logs (`queued/sending/sent/failed/suppressed`)
- timeline drill-down and controlled message retry by message ID
- domain readiness check (SPF/DKIM/DMARC) via existing API
- suppression/unsubscribe quick actions (requires admin API key for those writes)
- webhook dead-letter view and replay controls
- saved filter presets for `failed`, `suppressed`, and `dead_letter` views
- time-range filtering (`from`/`to`) for message logs and webhook logs
- bulk retry/replay actions with per-item outcomes
- technical onboarding checklist workflow (optional domain readiness check)
- incident bundle export flow for message timeline, attempts, and webhook outcomes

Analytics/export and billing metering:

```sh
curl -sS "http://localhost:8080/v1/analytics/summary?workspace_id=1" \
  -H "X-API-Key: change-me-operator"

curl -sS "http://localhost:8080/v1/analytics/export.csv?workspace_id=1&limit=1000" \
  -H "X-API-Key: change-me-operator"

curl -sS "http://localhost:8080/v1/billing/metering?workspace_id=1" \
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
- signed webhook verification with replay window (`WEBHOOK_*` config, when enabled)

## Architecture

Architecture and operational runbooks are maintained privately by the maintainers.

## License

GNU Affero General Public License v3.0 (AGPL-3.0). See [LICENSE](LICENSE).

## Governance Docs

- [CONTRIBUTING.md](CONTRIBUTING.md)
- [AGENTS.md](AGENTS.md)
- [CLAUDE.md](CLAUDE.md)
- [SECURITY.md](SECURITY.md)
