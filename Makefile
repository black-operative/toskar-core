.DEFAULT_GOAL := help

COMMIT := $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
LDFLAGS := -X github.com/yeixio/yggdrasil-core/internal/version.Commit=$(COMMIT)

.PHONY: help start ui frontend daemon run-daemon run-web all tidy test vet fmt lint ci quality quality-real test-cluster package-headless screenshots appstore-screenshots icons

help: ## Show targets
	@echo "Yggdrasil Core"
	@echo ""
	@echo "Start the app:"
	@echo "  make start"
	@echo ""
	@echo "Then open http://127.0.0.1:7331"
	@echo ""
	@echo "Targets:"
	@awk 'BEGIN {FS = ":.*?## "} /^[a-zA-Z0-9_-]+:.*?## / {printf "  %-18s %s\n", $$1, $$2}' $(MAKEFILE_LIST)

start: ui daemon ## Build the web UI and daemon, then run it
	@echo "Open http://127.0.0.1:7331"
	TOSKAR_WEB_UI_DIR="$(CURDIR)/web/dist" ./bin/toskar

run-daemon: start ## Alias of start

ui: ## Install web dependencies and build web/dist
	cd web && pnpm install && pnpm build

frontend: ## Install web dependencies, run web tests, and build web/dist
	cd web && pnpm install && pnpm test && pnpm build

daemon: ## Build bin/toskar and bin/toskarctl
	go build -ldflags "$(LDFLAGS)" -o bin/toskar ./cmd/daemon
	go build -ldflags "$(LDFLAGS)" -o bin/toskarctl ./cmd/devctl

run-web: ## Start the Vite dev server on http://127.0.0.1:5173
	cd web && pnpm dev

all: tidy test frontend ## Tidy modules, run Go tests, and build the frontend

tidy: ## Run go mod tidy
	GOSUMDB=off go mod tidy

fmt: ## Format Go files with gofmt
	gofmt -w $$(find . -name '*.go' \
		-not -path './web/*' \
		-not -path './.gocache/*' \
		-not -path './vendor/*' \
		-not -path '*/node_modules/*')

vet: ## Run go vet
	go vet ./...

lint: ## Run golangci-lint and web ESLint
	golangci-lint run ./...
	cd web && pnpm lint

test: ## Run Go tests
	go test ./...

ci: fmt lint vet test frontend ## Run the local CI checks

quality: ## Run the quality test set against the stub model
	go test ./tests/quality -count=1 -v

quality-real: ## Run the quality test set against a running daemon (TOSKAR_QUALITY_URL, default http://127.0.0.1:7331)
	TOSKAR_QUALITY_URL=$${TOSKAR_QUALITY_URL:-http://127.0.0.1:7331} go test ./tests/quality -count=1 -v -timeout 60m

test-cluster: ## Run the Docker cluster check
	chmod +x scripts/cluster-e2e.sh
	./scripts/cluster-e2e.sh

package-headless: ## Build a headless package for this machine
	chmod +x scripts/build/package-headless.sh
	./scripts/build/package-headless.sh

icons: ## Render the Linux icon set from docs/brand/logo
	./scripts/brand/render-icons.sh

screenshots: ## Recapture README stills and the demo walkthrough from fake data
	chmod +x scripts/capture-screenshots.sh
	./scripts/capture-screenshots.sh

appstore-screenshots: ## iPhone, iPad, and Mac App Store PNGs from fake data
	chmod +x scripts/capture-screenshots.sh
	STORE_ONLY=1 ./scripts/capture-screenshots.sh
