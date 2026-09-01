# Roadmap

`maild` is a **self-hosted outbound email control plane** — a sending orchestration, policy, and observability layer in front of your SMTP relay (for example MXRoute). It is transactional and conversational by design (see RELEASING.md) and deliberately avoids mass/cold-email tooling.

The business model and deployment topology are decided (see `## Business model & deployment`), and the immediate focus is shipping this outbound control plane to `v1.0.0`.

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
- [x] README refreshed to the actual surface
- [x] DB layer tests (`store/postgres`) against a real Postgres
- [x] First-send end-to-end path documented in the README

### Phase 1 — Correctness & release engineering

- [x] `store/postgres` test coverage against real Postgres
- [x] Verify and document the "first send" path (signup -> SMTP account -> send -> logs)
- [x] Review email-safety expectations against implementation (suppression, rate-limit, backoff, no credential leakage)
  - [x] Normalize suppression/unsubscribe matching (case-insensitive)
  - [x] Non-blocking backoff retry (scheduled Redis ZSET) so a single worker is not blocked by backoff
  - [x] Migrate password hashing to bcrypt with legacy rehash path
- [x] Add a release workflow (test, vuln scan, image build, version stamping — no automatic tag)

### Phase 2 — v1.0 gate

- [ ] Define and pass the end-to-end QA matrix
- [ ] Pass `make verify-full` in CI
- [ ] Tag `v1.0.0` (manual, gated by RELEASING.md)

## Business model & deployment (decided)

Self-hosted, open-source (AGPL) outbound email control plane, following the self-hosted OSS playbook (BillionMail-style). It is BYO-SMTP: the customer brings the relay; maild provides the orchestration, policy, and observability layer. It sends transactional/conversational mail and respects relay AUP (not mass/cold-email tooling).

Revenue does not come from a hosted cloud at v1.0. It comes from:
- GitHub Sponsors / Ko-fi (already configured: `ko_fi: srmdn`)
- Paid support and deployment help (installs, migration, managed support)

Deployment topology:
- Personal / own use -> Docker containers (Postgres, Redis, maild) on the existing VPS (`server.srmdn.com` / gc1dc2).
- Client deployments -> a dedicated small VPS per client, isolated from the user's production host.

Not pursued now (revisit only if demand proves it): a hosted, multi-tenant maild cloud on the same box. That would require operating 24/7 email infrastructure and support, and the user's client-production VPS stays isolated.

Open task: rework the landing pricing cards to match this model (free self-host + Sponsor/Support) instead of paid cloud tiers.
