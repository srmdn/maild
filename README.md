# maild

`maild` is a self-hosted delivery control layer for applications that send transactional email through their own SMTP relay. Your application calls an HTTP API; maild queues the message, applies delivery rules, sends through the configured relay, and records what happened.

## Who It Is For

Use maild when you operate an application and already have, or want to choose, an SMTP provider. It centralizes work that otherwise ends up scattered across application code and provider dashboards:

- queueing and retrying sends
- switching to a standby SMTP account after repeated failures
- enforcing suppression, unsubscribe, and blocked-recipient-domain rules
- recording message attempts, webhook outcomes, and incident context

maild is for transactional and conversational application email, such as account notifications, receipts, and password resets.

## What It Is Not

maild is not an inbox host, mail server, marketing suite, or drop-in replacement for SendGrid or Mailgun. It does not accept SMTP submissions from your application.

Today, applications integrate with maild by calling its authenticated HTTP API. A send supports one recipient and a plain-text body. HTML bodies, attachments, CC/BCC, templates, SDKs, and a WordPress plugin are not available yet.

For WordPress specifically, maild cannot be configured as an SMTP host in an SMTP plugin. It would need a dedicated WordPress plugin or an SMTP-submission interface first.

## How It Works

```text
your Go / Node application
          |
          | POST /v1/messages
          v
maild API -> PostgreSQL + Redis queue -> worker -> your SMTP relay -> recipient
```

The API returns after accepting and queueing a message. Sending happens asynchronously in the worker. Use the logs and timeline endpoints to inspect the outcome.

## Current Status

maild is pre-1.0. The core queue, worker, SMTP accounts, retries/backoff, failover, suppression rules, signed webhook handling, and operator UI are implemented. The v1.0 release gate has not passed; see the [QA matrix](docs/qa/v1.0-matrix.md). The API contract can change before v1.0, and there are no releases or tags before the v1.0 gate. See [RELEASING.md](RELEASING.md).

## Run Locally

Requirements:

- Go 1.27.2 or later
- Docker and Docker Compose

Set up PostgreSQL, Redis, and Mailpit for local development:

```sh
make setup
```

`make setup` creates `.env` when it is missing. Before starting maild, set a local encryption key in that file. Generate one with:

```sh
openssl rand -base64 32
```

Set the output as `ENCRYPTION_KEY_BASE64` in `.env`, then start the server:

```sh
make run
```

Confirm that it is running:

```sh
curl -sS http://localhost:8080/healthz
```

Mailpit receives local test messages at `http://localhost:8025`.

## First Send

The development `.env` uses the admin key `change-me-admin`. Do not use it outside local development.

Register Mailpit as the active SMTP account for the default workspace:

```sh
curl -sS -X POST http://localhost:8080/v1/smtp-accounts \
  -H 'X-API-Key: change-me-admin' \
  -H 'Content-Type: application/json' \
  -d '{
    "workspace_id": 1,
    "name": "mailpit",
    "host": "localhost",
    "port": 1025,
    "from_email": "noreply@maild.local"
  }'
```

Queue a message:

```sh
curl -sS -X POST http://localhost:8080/v1/messages \
  -H 'X-API-Key: change-me-admin' \
  -H 'Content-Type: application/json' \
  -d '{
    "workspace_id": 1,
    "from_email": "noreply@maild.local",
    "to_email": "you@example.com",
    "subject": "Hi",
    "body_text": "Hello from maild"
  }'
```

A successful request returns `201 Created` and a queued message object. The worker then sends it through Mailpit. Open `http://localhost:8025` to see it, or inspect the delivery records:

```sh
curl -sS 'http://localhost:8080/v1/messages/logs?workspace_id=1' \
  -H 'X-API-Key: change-me-admin'
```

## Integrate an Application

Every application uses the same JSON request to `POST /v1/messages` and sends the configured API-key header (by default, `X-API-Key`). The required fields are `from_email`, `to_email`, and `subject`; `body_text` may be empty. Personal keys use their own workspace when `workspace_id` is omitted and cannot access another workspace. Trusted instance keys retain the default workspace `1`.

Create personal keys in the dashboard for application integrations. Keys follow current workspace membership and role, and revocation takes effect immediately. The UI uses your session and does not expose instance keys. See [authentication and workspace access](docs/authentication.md).

- [Go integration example](docs/integrations/go.md)
- [Node.js integration example](docs/integrations/node.md)

Before sending real mail, register an SMTP account with `POST /v1/smtp-accounts`. The account credentials are encrypted at rest. `SMTP_*` environment variables are still required at startup and act as the fallback SMTP account if a workspace has no registered account; see [production configuration](deploy/production-config.md).

### Common API Outcomes

| Status | Meaning |
| --- | --- |
| `201 Created` | The message was accepted and queued. It has not necessarily been delivered yet. |
| `400 Bad Request` | The request is invalid, the recipient domain is blocked, or the recipient email is invalid. |
| `401 Unauthorized` / `403 Forbidden` | The API key is missing, invalid, or lacks the required role. |
| `429 Too Many Requests` | A workspace or recipient-domain rate limit was reached. |
| `500 Internal Server Error` | maild could not queue or process the request. Check logs and the message timeline. |

Delivery is asynchronous and at-least-once when an SMTP send returns an error:
the relay may have accepted a message before its response was lost, so a retry
can produce a duplicate. If a worker claim expires while its result is unclear,
maild marks the message `unknown` and does not retry it automatically. Check the
relay's logs before taking manual action.

## Operate It

API-key-authenticated delivery and operational routes include:

- `POST /v1/messages`, `POST /v1/messages/retry`
- `GET /v1/messages/logs`, `GET /v1/messages/timeline`
- `POST /v1/smtp-accounts`, `POST /v1/smtp-accounts/activate`, `POST /v1/smtp-accounts/validate`, `GET /v1/smtp-accounts/list`
- `POST /v1/suppressions`, `POST /v1/unsubscribes`, `GET/POST /v1/workspaces/policy`
- `POST /v1/domains/readiness`
- `GET /v1/analytics/summary`, `GET /v1/analytics/export.csv`, `GET /v1/billing/metering` (usage metering only; it does not process payments)
- `GET /v1/ops/onboarding-checklist`, `GET /v1/incidents/bundle`

When `WEBHOOKS_ENABLED=true`, maild also accepts signed provider events at `POST /v1/webhooks/events`; inspect and replay failed processing through `/v1/webhooks/logs` and `/v1/webhooks/replay`.

The session-authenticated operator UI is available at `/ui`, `/ui/logs`, `/ui/onboarding`, `/ui/incidents`, and `/ui/policy`.

## Production

Set `APP_ENV=production` and begin with [`.env.production.example`](.env.production.example). Production startup rejects missing values and development defaults. Read [production configuration ownership and rotation](deploy/production-config.md) before deployment.

## Development

Run the project gate before contributing:

```sh
make verify
```

Run Postgres store integration tests after creating the local `maild_test` database:

```sh
make test-store
```

See [CONTRIBUTING.md](CONTRIBUTING.md), [ROADMAP.md](ROADMAP.md), and [SECURITY.md](SECURITY.md) for contribution, project direction, and vulnerability-reporting guidance.

## License

GNU Affero General Public License v3.0 (AGPL-3.0). See [LICENSE](LICENSE).
