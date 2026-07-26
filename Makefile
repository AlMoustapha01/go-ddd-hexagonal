.DEFAULT_GOAL := help
BINARY       := bin/api
PKG          := ./...
DATABASE_URL ?= postgres://app:app@localhost:5432/app?sslmode=disable
# Database used by the adapter contract tests. When unset, those tests skip.
TEST_DATABASE_URL ?= $(DATABASE_URL)

.PHONY: help
help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | \
		awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-18s\033[0m %s\n", $$1, $$2}'

.PHONY: tidy
tidy: ## Sync go.mod / go.sum
	go mod tidy

.PHONY: build
build: ## Compile the API binary into ./bin
	go build -o $(BINARY) ./cmd/api

.PHONY: run
run: ## Run the API (in-memory driver by default)
	go run ./cmd/api

.PHONY: test
test: ## Run all tests (Postgres contract tests skip without a database)
	go test -race -count=1 $(PKG)

.PHONY: test-db
test-db: ## Start only the Postgres service the contract tests need
	docker compose up -d db

.PHONY: test-integration
test-integration: ## Run all tests INCLUDING the Postgres contract (needs test-db)
	TEST_DATABASE_URL="$(TEST_DATABASE_URL)" go test -race -count=1 $(PKG)

.PHONY: cover
cover: ## Run tests with coverage report
	go test -race -coverprofile=coverage.out $(PKG)
	go tool cover -func=coverage.out

.PHONY: vet
vet: ## Run go vet
	go vet $(PKG)

.PHONY: lint
lint: ## Run golangci-lint (install: https://golangci-lint.run/usage/install/)
	@if ! command -v golangci-lint >/dev/null 2>&1; then \
		echo "golangci-lint not found. Install it: https://golangci-lint.run/usage/install/"; exit 1; \
	fi
	golangci-lint run ./...

.PHONY: fmt
fmt: ## Format the code
	gofmt -s -w .

.PHONY: migrate-up
migrate-up: ## Apply DB migrations (needs golang-migrate + DATABASE_URL)
	migrate -path migrations -database "$(DATABASE_URL)" up

.PHONY: migrate-down
migrate-down: ## Roll back one migration
	migrate -path migrations -database "$(DATABASE_URL)" down 1

.PHONY: docker-up
docker-up: ## Start db + migrate + api via docker compose
	docker compose up --build

.PHONY: docker-down
docker-down: ## Stop and remove compose stack + volumes
	docker compose down -v

.PHONY: clean
clean: ## Remove build artifacts
	rm -rf bin coverage.out coverage.html
