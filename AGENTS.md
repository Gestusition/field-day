# AGENTS.md

Guidance for AI coding agents (and humans) working in this repository.
App-specific rules live in the `AGENTS.md` inside each app folder; the
closest file to the code you are changing wins.

## What this is

Field Day is an NFC identification platform by AREL Embedded Systems Club:
access control, closed-loop payments and object/person identification with
NFC cards and phones. One **shared backend** serves every client:

| Client | Folder | Talks to backend via |
|---|---|---|
| Backend (Go) | `apps/api` | — |
| Web admin + user panel | `apps/web` (planned) | REST, `/api/admin/*`, `/api/me/*` |
| Mobile app | `apps/mobile` (mobile team) | REST, `/api/me/*` |
| NFC reader (STM32 + NFC frontend + ESP32-C3) | `firmware/` (embedded team) | REST `/api/device/*` + MQTT |

## Repository map

```
api/openapi.yaml     THE contract. Single source of truth for every client.
apps/api/            Go backend (domain-based modular monolith)
docs/                Architecture, device protocol, Casdoor setup
deploy/              Container/infra support files
package.json         `bun run dev` / `bun run prod` / `stop` / `logs` / `reset`
compose.yaml         Dev stack: PostgreSQL + API (Casdoor only under the "casdoor" profile)
compose.prod.yaml    Production overrides: Casdoor required (AUTH_MODE=oidc)
.github/             CI, Dependabot, templates, CODEOWNERS
```

## Golden rules

1. **Contract first.** Any API change starts in `api/openapi.yaml`, then
   `make gen` in `apps/api`, then the implementation. Never hand-edit
   generated code (`internal/apigen/*.gen.go`, `internal/*/store/*`).
2. **Never break clients silently.** Removing/renaming a field or endpoint, or
   making an optional field required, is a breaking change: call it out in the
   PR and tag the affected teams (web, mobile, firmware).
3. **Migrations are append-only.** Never edit a migration that is on `main`;
   add a new one. Every migration must pass Squawk.
4. **Secrets never enter git**: no `.env`, keys, tokens, real card UIDs or
   personal data (names, phone numbers, emails) in code, tests, fixtures or docs.
5. **Small, focused PRs** with Conventional Commit titles.

## Git conventions

- Branches: `feat/<topic>`, `fix/<topic>`, `chore/<topic>`, `docs/<topic>`.
- Commits: [Conventional Commits](https://www.conventionalcommits.org/) —
  `feat(api): add wallet refunds`, `fix(firmware): debounce taps`.
  Scopes: `api`, `contract`, `db`, `web`, `mobile`, `firmware`, `ci`, `docs`.
- Never commit directly to `main`; open a PR. CI must be green.
- Merge PRs with a merge commit (`gh pr merge <n> --merge`), never squash or
  rebase, so every commit stays visible in the history.
- Do not add tool, assistant or bot attribution to branch names, commit
  messages, trailers or PR descriptions.

## Commands

Run everything (repo root, needs only Docker + Bun):

```sh
bun run dev      # PostgreSQL + API on :8080, no Casdoor; auth header "Bearer dev" (admin)
bun run prod     # production mode: Casdoor required, "dev" token rejected
```

`AUTH_MODE=dev` is refused when `APP_ENV=production`. Keep all Casdoor/OIDC
code; it is what production uses.

Backend commands run from `apps/api` (see its `AGENTS.md`):

```sh
make gen                                # regenerate from contract + SQL
make test                               # unit + integration (needs Docker)
make lint
```

Contract lint (repo root): `npx @redocly/cli lint api/openapi.yaml`.

## Domain vocabulary

- **Credential** — anything a reader identifies: physical card (`desfire`,
  `mifare_classic`, `ntag`, ...) or a phone using HCE (`mobile_hce`).
- **Reader** — one NFC terminal; authenticates with an API key; mode
  `access` (doors) or `wallet` (payments).
- **Zone / group / grant** — access control model: grants give a user or a
  group access to a zone, optionally limited by weekday/time and validity dates.
- **Tap** — one credential presentation; always recorded as an access event.
- **Enrollment** — arming a reader so the next tapped card is registered.
- **Secure credential** — verified cryptographically (DESFire mutual auth or
  HCE HMAC). MIFARE Classic UID-only reads are *not* secure.
