# field-day

NFC-based identification platform by **AREL Embedded Systems Club**.

Use cases: physical **access control**, closed-loop **payments** (cafeteria,
vending), **person/object identification** and **device configuration** with
NFC cards, tags and phones (Host Card Emulation).

One **shared backend** serves the web panel, the mobile app and the NFC
readers through a single API contract: [`api/openapi.yaml`](api/openapi.yaml).

## Repository

| Path | What | Team |
|---|---|---|
| [`api/openapi.yaml`](api/openapi.yaml) | Shared API contract | all |
| [`apps/api`](apps/api) | Backend (Go) + database | backend |
| `apps/web` | Web admin/user panel *(planned)* | web |
| `apps/mobile` | Mobile app *(mobile team)* | mobile |
| `firmware/` | Reader firmware *(embedded team)* | embedded |
| [`docs/`](docs) | [Architecture](docs/architecture.md), [reader protocol](docs/device-protocol.md), [Casdoor setup](docs/casdoor-setup.md) | all |

Contributor guide: [CONTRIBUTING.md](CONTRIBUTING.md). Agent/automation guide: [AGENTS.md](AGENTS.md).

## Quick start

The only requirements are [Docker Desktop](https://www.docker.com/products/docker-desktop/) and [Bun](https://bun.sh).

```sh
bun run dev
```

That's it: PostgreSQL and the API start, migrations run, and
<http://localhost:8080> opens the interactive API docs. No Casdoor is needed
in development — sign in by sending:

```
Authorization: Bearer dev
```

(`dev` is an admin account, so every endpoint works.) Readers connect to MQTT
on `localhost:1883`. Stop with `Ctrl+C`.

| Command | What it does |
|---|---|
| `bun run dev` | Development: PostgreSQL + API, no Casdoor, `Bearer dev` login |
| `bun run prod` | Production mode: PostgreSQL + **Casdoor** + API, real logins only. Needs a `.env` (copy `.env.example`) and Casdoor configured ([docs/casdoor-setup.md](docs/casdoor-setup.md)) |
| `bun run stop` | Stop everything |
| `bun run logs` | Follow API logs |
| `bun run reset` | Stop and **delete** all local data |

Backend contributors working on Go code: see [apps/api/AGENTS.md](apps/api/AGENTS.md)
(`make test`, `make gen`, `make lint`; Go and sqlc needed there).

## API at a glance

| Prefix | Client | Auth |
|---|---|---|
| `/api/me/*` | mobile app, web (end user) | Casdoor access token (`Bearer dev` in development) |
| `/api/admin/*` | web admin panel | Casdoor access token with role `admin` (`Bearer dev` in development) |
| `/api/device/*` | NFC readers | per-reader API key |

Main capabilities: users (just-in-time from Casdoor, admin pre-registration),
credentials (cards + phone HCE, tap-to-enroll, report lost), readers (API
keys, heartbeat, command queue over MQTT/HTTP), access control (zones,
groups, weekday/time grants, secure-credential zones, event log), wallet
(top-up, card payments, refunds, idempotent ledger) and an admin audit log.

## Tech stack

> The stack is a proposal and open to discussion; items marked *(planned)* are not in the repo yet.

### Backend (shared API for web, mobile and readers)

* Language: Go
* Framework: Gin
* Architecture: Domain-based modular monolith
* API: REST + JSON, contract first
* API contract: OpenAPI 3.0.3, linted with Redocly CLI
* API docs UI: Scalar
* Code generation: oapi-codegen (strict server)
* Request validation: from the OpenAPI contract (kin-openapi)
* Errors: RFC 9457 problem details
* Authentication: Casdoor (OAuth 2.0 / OpenID Connect), verified with coreos/go-oidc
* Device authentication: per-reader API keys (hashed, rotatable)
* Authorization: RBAC (`admin` role) + per-resource ownership
* Reader commands: embedded MQTT broker (mochi-mqtt), HTTP polling fallback
* Configuration: environment variables (caarlos0/env)
* Logging: slog
* Live reload: Air

### Database

* PostgreSQL 18 (UUIDv7 ids)
* Driver: pgx
* SQL code generation: sqlc (one package per module)
* Migrations: golang-migrate (embedded in the binary)
* Migration safety: Squawk

### Testing

* Go testing + testify
* Integration tests: Testcontainers (real PostgreSQL)
* API testing: Bruno *(planned)*
* Web: Vitest + React Testing Library, Playwright *(planned)*

### Web frontend *(planned)*

* Vite, TypeScript, React, TanStack Router + Query, Bun
* Tailwind CSS v4, shadcn/ui, Lucide
* Orval (API client from the contract), React Hook Form, Zod
* Casdoor login with OIDC + PKCE (oidc-client-ts)

### Mobile (iOS & Android)

* Decided by the mobile team. Phone-as-card (HCE) requires native Android
  code (Kotlin, or a native module if React Native/Expo is used).
* Login: Casdoor (OIDC + PKCE); API client generated from the contract.

### NFC reader hardware

* MCU: STM32
* NFC frontend: PN5180 (preferred) / PN512 / ST25R3916
* Connectivity: ESP32-C3 (Wi-Fi), HTTPS + MQTT
* Cards: MIFARE DESFire EV2/EV3 recommended; MIFARE Classic supported as UID-only (insecure)

### Infrastructure & DevOps

* GitHub monorepo
* Docker + Docker Compose; backend image: multi-stage Go → distroless
* Reverse proxy + static web server: Caddy *(planned, with the web app)*
* CI/CD: GitHub Actions; Dependabot for Go, Docker and Actions
* Git hooks: Lefthook
* Linting/formatting: golangci-lint, gofmt; ESLint + Prettier for web
