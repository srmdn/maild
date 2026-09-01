# Release Policy

This project is **pre-1.0**. Until the first stable release, **no git tags and no GitHub Releases are created.** `main` is the only trunk; versions are derived from build information, not from tags.

## Versioning

- Use [Semantic Versioning 2.0.0](https://semver.org).
- Pre-1.0 means the 0.x contract makes no API-stability promise; breaking changes are allowed.
- The single source of truth for the version is `internal/buildinfo.Version`, injected at build time via `-ldflags`.

## Tags & Releases

- **Do not create git tags** (for example `v0.6.0`) before `v1.0.0`. This includes local tags.
- **Do not publish GitHub Releases** before `v1.0.0`.
- `v1.0.0` is the first stable release and is gated by the release checklist below.

## Build Version Stamping

- Default (dev) builds stamp the version from `git rev-parse --short HEAD` via `make build`.
- Release automation overrides explicitly with `make build VERSION=v1.0.0`.
- Inspect the stamped version at runtime:

  ```sh
  curl -s -H 'Accept: application/json' http://localhost:8080/
  ```

  or to print the effective version value:

  ```sh
  make version
  ```

## Release Gate (required before any tag)

A stable release may only be published when all of the following pass:

- [ ] `make verify-full` (fmt, build, tests, attribution, `govulncheck`)
- [ ] End-to-end send verification against a real SMTP provider (see README)
- [ ] README reflects the actual route and feature surface
- [ ] Version is stamped via `-ldflags`, not the `dev` fallback
- [ ] No secrets or local state are present in the release artifact

## Scope Guardrails

Transactional and conversational sending only. Mass / cold email tooling is deliberately out of scope (see ROADMAP.md) to stay compliant with relay providers' acceptable-use policies such as MXRoute's.
