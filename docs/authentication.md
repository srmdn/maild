# Authentication and workspace access

The dashboard and operator pages use the signed-in user's session. They do not
require an API key or expose instance credentials. Browser requests that change
data must originate from the same origin.

## Personal API keys

Create a personal key in the dashboard and copy its value immediately. The value
is shown once; later listings show only the name, prefix, and last-use time. Send
the key in the configured API-key header, which defaults to `X-API-Key`.

Each key belongs to the creating user and their current workspace. Omitting
`workspace_id` selects that workspace. An explicit different workspace is denied.
Message timeline requests also verify the message's workspace before returning
its contents. Revoking a key or removing its workspace membership invalidates it
on the next request. Role changes apply to existing keys and sessions immediately.

The current roles are:

| Role | Access |
| --- | --- |
| `admin` | Read and operate the workspace, including SMTP configuration, policy, and recipient safety changes |
| `operator` | Read, submit messages, retry failed messages, replay webhooks, and inspect domain readiness |
| `viewer` | Read workspace data; cannot send, change policy, or create keys |

Key management is session-only. Users can list and revoke only their own keys in
the current workspace; an admin or operator can create a key. The current app
selects the oldest workspace membership, with workspace ID as a deterministic
tie-breaker. Workspace switching and invitations are separate features.

Missing or invalid credentials return `401`. A valid identity without permission
returns `403`; a message outside the caller's workspace is reported as `404`.
On `/v1` delivery/operator API routes, an explicitly supplied invalid API key does
not fall back to a valid session.

## Instance credentials

The configured admin and operator instance keys retain their trusted access
across workspaces. They are for instance operators and must not be distributed
to application users or embedded in pages. Personal integrations use personal
keys instead. The legacy default workspace `1` applies only to instance keys.

## Existing installations

The API-key migration binds existing keys to the user's oldest current workspace
membership without changing their hashes. A key without a membership remains
unusable. Older keys have no recoverable display prefix and are labeled as legacy
keys in the dashboard. New keys store a display prefix alongside their hash.

## Regression tests

Use disposable PostgreSQL databases and a test Redis instance. The store fixture
truncates its data, so it must use a different database from the HTTP fixture.
Set `MAILD_TEST_DSN`, `MAILD_HTTP_TEST_DSN`, and
`MAILD_HTTP_TEST_REDIS_ADDR`, then run:

```sh
make test-auth
```

This target requires all three settings and runs middleware, store, migration,
and HTTP access tests. Ordinary `make verify` still skips dependency-backed tests
when their test settings are absent; it does not replace this auth gate.
