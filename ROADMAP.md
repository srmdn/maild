# Roadmap

`maild` is a **self-hosted outbound email control plane** — a sending orchestration, policy, and observability layer in front of your SMTP relay (for example MXRoute). It is transactional and conversational by design (see RELEASING.md) and deliberately avoids mass/cold-email tooling.

The long-term target is a hybrid brand (see `## Hybrid`), but the immediate focus is shipping this outbound control plane to `v1.0.0`.

## In scope for v1.0

- Queue + worker sending (API -> queue -> SMTP relay)
- Retries / backoff and failover between SMTP providers
- Suppression list, unsubscribe, blocked-recipient domains, rate limits
- Signed webhooks with dead-letter handling and replay
- Logs, message timeline, incident bundle
- Analytics / metering / billing
- Domain readiness (SPF / DKIM / DMARC)
- User auth, RBAC (admin/operator), per-user API keys
- Server-rendered operator UI

## Explicitly out of scope for v1.0

- Mass / cold email tooling
- Mailbox hosting, inbound receiving, webmail, forwarders, aliases

## Phases

### Phase 0 — Foundation

- [x] Direction and release policy locked (RELEASING.md)
- [x] Stale root `migrations/` removed (single source: `internal/migrate/sql`)
- [x] `buildinfo.Version` stamped via ldflags (`make version`, `make build`)
- [ ] README refreshed to the actual surface
- [ ] DB layer tests (`store/postgres`) against a real Postgres
- [ ] First-send end-to-end path documented

### Phase 1 — Correctness & release engineering

- [ ] `store/postgres` test coverage against real Postgres
- [ ] Verify and document the "first send" path (signup -> SMTP account -> send -> logs)
- [ ] Review email-safety expectations against implementation (suppression, rate-limit, backoff, no credential leakage)
- [ ] Add a release workflow (test, vuln scan, image build, version stamping — no automatic tag)

### Phase 2 — v1.0 gate

- [ ] Define and pass the end-to-end QA matrix
- [ ] Pass `make verify-full` in CI
- [ ] Tag `v1.0.0` (manual, gated by RELEASING.md)

## Hybrid (long-term target)

Business email hosting (mailbox, webmail, IMAP) on top of MXRoute/DirectAdmin is the revenue engine; `maild` becomes the transactional/control-plane companion for the same customers. That path reuses from `maild`: auth/RBAC, workspaces, domain readiness, onboarding, metering/billing, and incidents.
