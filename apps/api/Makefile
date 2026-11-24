# Field Day API — common tasks. Run from apps/api.
# Requirements: Go (version in go.mod), sqlc, Docker (for integration tests).

.DEFAULT_GOAL := help
SPEC := ../../api/openapi.yaml

.PHONY: help
help: ## List targets
	@grep -E '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) | awk 'BEGIN{FS=":.*?## "}{printf "  %-14s %s\n", $$1, $$2}'

.PHONY: gen
gen: ## Regenerate HTTP layer (OpenAPI) and DB layer (sqlc)
	go generate ./...
	sqlc generate

.PHONY: check-gen
check-gen: gen ## Fail if generated code is out of date (used by CI)
	git diff --exit-code -- internal/apigen internal/*/store

.PHONY: build
build: ## Build the binary into bin/
	go build -o bin/api ./cmd/api

.PHONY: run
run: ## Run against the local .env
	set -a && . ./.env && set +a && go run ./cmd/api

.PHONY: dev
dev: ## Run with live reload (needs `go install github.com/air-verse/air@latest`)
	set -a && . ./.env && set +a && air

.PHONY: test
test: ## Unit + integration tests (integration needs Docker)
	go test -race -count=1 ./...

.PHONY: test-unit
test-unit: ## Unit tests only
	go test -short -count=1 ./...

.PHONY: lint
lint: ## golangci-lint
	golangci-lint run ./...

.PHONY: migrate-up
migrate-up: ## Apply migrations using .env
	set -a && . ./.env && set +a && go run ./cmd/api migrate up

.PHONY: migrate-new
migrate-new: ## Create a migration pair: make migrate-new name=add_x
	@test -n "$(name)" || (echo "usage: make migrate-new name=add_x" && exit 1)
	@n=$$(ls migrations/*.up.sql | wc -l); v=$$(printf "%06d" $$((n+1))); \
	touch migrations/$${v}_$(name).up.sql migrations/$${v}_$(name).down.sql; \
	echo "created migrations/$${v}_$(name).{up,down}.sql"

.PHONY: docker
docker: ## Build the container image
	docker build -t field-day-api:local .
