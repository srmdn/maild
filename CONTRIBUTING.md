# Contributing

Thanks for contributing to `maild`.

## Core Rules

- Keep pull requests focused and reviewable.
- Humans are accountable for final code, docs, tests, and releases.
- AI assistance is allowed, but must be disclosed in PR descriptions when material.

## AI Contribution Disclosure

If AI materially influenced a PR, include:
- tools/models used
- files/areas influenced
- human validation performed (tests, review, security checks)

## Commit Attribution Policy

Commit history must not include AI branding/co-author trailers.

Install local guard:

```sh
cp scripts/pre-commit .git/hooks/pre-commit
chmod +x .git/hooks/pre-commit
```

Run check manually:

```sh
scripts/check-commit-attribution.sh
```

Cleanup runbook: [docs/AI-COMMIT-CLEANUP.md](docs/AI-COMMIT-CLEANUP.md)

## Development Expectations

Before requesting review for mail flow changes:
- test success path and failure path
- verify retry behavior and max retry boundaries
- verify rate limiting and suppression are respected
- confirm no secrets are printed in logs

## Security Reports

Please do not open public issues for vulnerabilities.
See [SECURITY.md](SECURITY.md).

