# Architecture

## Overview

```
 ┌──────────┐  ┌──────────┐   OIDC (PKCE)   ┌──────────┐
 │   Web    │  │  Mobile  │ ──────────────▶ │ Casdoor  │
 └────┬─────┘  └────┬─────┘                 └────┬─────┘
      │ REST /api/admin, /api/me                 │ JWKS
      ▼             ▼                            ▼
 ┌─────────────────────────────────────────────────────┐
 │                 Field Day API (Go)                  │
 │  contract middleware: route → auth → validate       │
 │  ┌──────┐┌──────────┐┌──────┐┌──────┐┌──────┐┌─────┐│
 │  │ user ││credential││reader││access││wallet││audit││
 │  └──────┘└──────────┘└──────┘└──────┘└──────┘└─────┘│
 │  embedded MQTT broker (reader commands)             │
 └───────────────▲──────────────────────┬──────────────┘
   REST /api/device │ MQTT              │ pgx
 ┌─────────────────┴──┐            ┌────▼───────┐
 │ NFC reader         │            │ PostgreSQL │
 │ STM32 + ESP32-C3   │            └────────────┘
 └────────────────────┘
```

## Decisions

| Decision | Why |
|---|---|
| **Modular monolith** | One deployable for a small team, but module boundaries (own tables, own queries, service interfaces) keep it splittable later. |
| **Contract first (OpenAPI 3.0.3)** | Four teams in different languages share one API. Server stubs and client SDKs are generated from the same file; CI lints it. 3.0.3 rather than 3.1 because the Go generator (oapi-codegen) fully supports 3.0. |
| **Auth & validation from the contract** | The `security` and schemas in `openapi.yaml` are enforced at runtime, so the documentation cannot drift from behaviour. |
| **sqlc + pgx** | Plain SQL, type-checked at generation time, no ORM magic. |
| **UUIDv7 ids** | Time-ordered (good index locality, cursor pagination by id) and safe to expose. |
| **HTTPS for requests, MQTT for commands** | A reader's questions ("open?") are request/response. Server-initiated work ("write this card") needs push; MQTT is native in ESP-IDF. The broker is embedded so there is nothing extra to run, and it reuses reader API keys. HTTP polling is a fallback. |
| **Readers use API keys, people use Casdoor** | Devices cannot do interactive login. Keys are random 256-bit, stored hashed, rotatable. |
| **DESFire recommended** | MIFARE Classic's Crypto1 is broken and its UID is clonable. Zones can require cryptographically verified credentials. |
| **HCE with HMAC challenge-response** | A phone credential's secret never leaves the phone after issuance; responses are bound to the reader. |
| **Integer minor units + locked ledger** | No float rounding; `SELECT … FOR UPDATE` makes concurrent charges safe; idempotency keys make retries safe. |
| **Problem details (RFC 9457)** | One error shape for every client, with stable `code` values. |

## Data model

- `users` — local profile linked to Casdoor `sub`.
- `credentials` — cards and phones; `uid` unique among non-revoked rows.
- `readers`, `reader_enrollments`, `reader_commands`.
- `zones`, `access_groups`, `access_group_members`, `access_grants`, `access_events`.
- `wallet_accounts`, `wallet_transactions` (append-only ledger).
- `audit_logs` — every state-changing admin request.

## Not yet built

- Offline operation for readers (signed allow-lists).
- DESFire key management service (key diversification and provisioning via SAM/SE050).
- Online top-up through a payment provider (admins top up today).
- Web panel (`apps/web`), Caddy reverse proxy and production deployment.
