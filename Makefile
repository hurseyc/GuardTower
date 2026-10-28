# GuardTower developer tasks. Run `make help` for a list.
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT  ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
DATE    ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS := -s -w -X github.com/hurseyc/guardtower/internal/version.Version=$(VERSION) \
	-X github.com/hurseyc/guardtower/internal/version.Commit=$(COMMIT) \
	-X github.com/hurseyc/guardtower/internal/version.Date=$(DATE)

.PHONY: help build web web-deps run dev test test-go test-web lint e2e fleet-up fleet-down seed traffic clean

help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-12s\033[0m %s\n", $$1, $$2}'

web-deps: ## Install frontend dependencies
	cd web && npm ci

web: ## Build the web UI into web/dist
	cd web && npm run build && touch dist/.gitkeep

build: web ## Build the guardtower binary into bin/
	CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o bin/guardtower ./cmd/guardtower

run: build ## Build and run with ./data as the data directory
	GT_DATA_DIR=./data ./bin/guardtower serve

dev: ## Run the Go server (port 8080) and Vite dev server (port 5173) together
	@echo "API on :8080, UI with hot reload on http://localhost:5173"
	@trap 'kill 0' INT TERM; GT_DATA_DIR=./data go run ./cmd/guardtower serve -log-level debug & (cd web && npm run dev) & wait

test: test-go test-web ## Run all unit tests

test-go: ## Go tests with race detector
	go test -race -count=1 ./...

test-web: ## Frontend unit tests (includes docs freshness checks)
	cd web && npx vitest run

lint: ## Go and frontend linters
	golangci-lint run ./...
	cd web && npx tsc -b && npm run lint

e2e: build ## Playwright end-to-end tests against a real dev fleet
	cd web && npx playwright test

fleet-up: ## Start 5 local AdGuard Home instances (no Docker needed)
	go run ./cmd/gtdev fleet up

fleet-down: ## Stop the local dev fleet
	go run ./cmd/gtdev fleet down

seed: ## Register the dev fleet with a running GuardTower (3+2 groups)
	go run ./cmd/gtdev seed

traffic: ## Send DNS traffic to the dev fleet
	go run ./cmd/gtdev traffic -duration 60s

clean: ## Remove build output
	rm -rf bin dist web/dist/assets web/dist/index.html
