# Production Configuration Ownership And Rotation

Use `.env.production.example` as the baseline template for production deployments.

## Ownership

| Variable(s) | Owner | Notes |
| --- | --- | --- |
| `ADMIN_API_KEY`, `OPERATOR_API_KEY` | Security + Platform | Generate strong random values, never re-use across environments. |
| `ENCRYPTION_KEY_BASE64` | Security | Must be a 32-byte AES key encoded in base64. |
| `WEBHOOK_SIGNING_SECRET` | Security + Integrations | Shared with webhook sender(s), treat as a secret. |
| `POSTGRES_DSN` | Platform/DBA | Must use production DB endpoint and TLS (`sslmode=require` or stronger). |
| `REDIS_ADDR`, `REDIS_DB` | Platform | Production Redis instance only. |
| `SMTP_HOST`, `SMTP_PORT`, `SMTP_USERNAME`, `SMTP_PASSWORD`, `SMTP_FROM` | Messaging Operations | Use provider-issued credentials and verified sender identity. |

## Rotation Expectations

- `ADMIN_API_KEY`, `OPERATOR_API_KEY`: rotate at least every 90 days and immediately on suspected leak.
- `ENCRYPTION_KEY_BASE64`: rotate on incident response or key custody change; plan controlled re-encryption migration.
- `WEBHOOK_SIGNING_SECRET`: rotate every 90 days and coordinate with sender cutover.
- `SMTP_PASSWORD`: rotate per provider policy (recommended 60-90 days).
- `POSTGRES_DSN` / `REDIS_ADDR` credentials: rotate per platform standard and after access-control changes.

## Startup Validation Behavior

When `APP_ENV=production`, `maild` fails fast at startup if required production runtime values are missing or if development defaults are used:

- `ADMIN_API_KEY`
- `OPERATOR_API_KEY`
- `ENCRYPTION_KEY_BASE64`
- `POSTGRES_DSN`
- `REDIS_ADDR`
- `SMTP_HOST`
- `SMTP_PORT`
- `SMTP_FROM`

This guard is intentional and blocks accidental production boot with local/development defaults.
