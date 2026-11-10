# field-day

NFC-based authentication platform by **AREL Embedded Systems Club**.
First use case: AC EV charging management.

> **Note:** This tech stack is **not final**. It is a proposal and open to discussion.

## Tech Stack

### Backend (Shared API for Web + Mobile)

* Language: Go
* Framework: Gin
* Architecture: Domain-Based Modular Monolith
* API: REST + JSON
* API Documentation: OpenAPI 3.1
* OpenAPI Linting: Redocly CLI
* API Docs UI: Scalar
* OpenAPI Code Generation: oapi-codegen
* Request Validation: go-playground/validator
* Authentication: Casdoor (OAuth 2.0 / OpenID Connect)
* OIDC / JWT Verification: coreos/go-oidc
* Authorization: RBAC
* Configuration: Environment variables (caarlos0/env)
* Logging: slog
* Live Reload: Air

### Database

* Database: PostgreSQL
* Database Driver: pgx
* SQL Code Generation: sqlc
* Database Migrations: golang-migrate
* Migration Safety: Squawk

### Web Frontend

* Build Tool: Vite (HMR + React Fast Refresh)
* Language: TypeScript
* UI Library: React
* Routing: TanStack Router
* Package Manager: Bun
* Styling: Tailwind CSS v4
* UI Components: shadcn/ui
* Icons: Lucide React
* Data Fetching: TanStack Query
* API Client Generation: Orval (from OpenAPI)
* Forms: React Hook Form
* Validation: Zod
* Authentication: Casdoor (OIDC + PKCE)
* OIDC Client: oidc-client-ts (react-oidc-context)

### Testing

* Backend: Go testing
* Assertions: testify
* Integration Testing: Testcontainers
* API Testing: Bruno
* Web Unit Testing: Vitest + React Testing Library
* Web E2E Testing: Playwright

### Mobile (iOS & Android)

* React Native + Expo, TypeScript, Expo Router
* NativeWind v5 + Tailwind CSS v4
* Lucide React Native
* TanStack Query, React Hook Form, Zod
* Casdoor (OIDC + PKCE), Expo AuthSession, Expo SecureStore
* EAS Build

### Embedded System (ESP32-C3)

* Language: C (ESP-IDF)
* NFC Reader: PN532
* Connectivity: Wi-Fi
* Communication: HTTPS + JSON

### Infrastructure & DevOps

* Repository: GitHub Monorepo
* Package Management: Bun + Go Modules
* Containerization: Docker + Docker Compose
* Reverse Proxy + Static Web Server: Caddy
* Web Image: Multi-stage build (Bun to Caddy)
* Backend Image: Multi-stage build (Go to distroless)
* CI/CD: GitHub Actions
* Git Hooks: Lefthook
* Linting: ESLint + golangci-lint
* Formatting: Prettier + gofmt

### Charging Integration

* Charging Protocol: TBD (pending charger hardware specifications)
* OCPP: Deferred
* Initial Integration: MockChargerAdapter
* NFC Authentication: Physical NFC card via ESP32-C3 + Go backend
* User Authentication: Casdoor
* Device Authentication: Device-specific credentials
