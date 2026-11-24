# Contributing

## Workflow

1. Open (or pick) an issue describing the change.
2. Create a branch from `main`: `feat/<topic>`, `fix/<topic>`, `chore/<topic>`, `docs/<topic>`.
3. Commit with [Conventional Commits](https://www.conventionalcommits.org/):
   `feat(api): add access groups`, `fix(mobile): refresh token on resume`.
4. Open a PR, fill in the template, get a review from the code owners.
5. Once CI is green, merge with **"Create a merge commit"** (default) or
   **"Rebase and merge"**. Squash is disabled so every commit stays in the
   history. Because each commit lands on `main` as is, every commit message
   must follow Conventional Commits.

Install the git hooks once: `lefthook install`.

## Working on the shared API

The backend, web app, mobile app and reader firmware all depend on
[`api/openapi.yaml`](api/openapi.yaml).

- **Backend team:** change the contract first, then the code (see
  [`apps/api/AGENTS.md`](apps/api/AGENTS.md)).
- **Web / mobile teams:** generate your client from the contract (Orval for
  TypeScript). Need a new endpoint or field? Open an issue or a PR that only
  changes `api/openapi.yaml` and discuss it before anyone implements it.
- **Embedded team:** the reader protocol is in
  [`docs/device-protocol.md`](docs/device-protocol.md). The `/device/*`
  endpoints in the contract are authoritative.

Breaking changes (removing/renaming fields or endpoints, new required fields)
must be marked in the PR and coordinated with every affected team.

## Running locally

See the "Quick start" section in the [README](README.md).

## Security

Never commit secrets, real card UIDs or personal data. If you find a security
problem, report it privately to the maintainers instead of opening an issue.
