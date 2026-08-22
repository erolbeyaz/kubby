# Kubby — all developer commands live here.
.DEFAULT_GOAL := help
SHELL := /bin/bash

SERVER_DIR := server
WEB_DIR    := web
DIST_DIR   := $(SERVER_DIR)/internal/webassets/dist

# Pinned tool versions (ADR-025 — no automatic upgrades).
AIR_VERSION      := v1.67.4
GOOSE_VERSION    := v3.27.3
GOLANGCI_VERSION := v2.13.1

# Build metadata surfaced by /version (compliance requirement).
VERSION    ?= dev
COMMIT_SHA := $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
BUILD_DATE := $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
PKG        := github.com/erolbeyaz/kubby/internal/httpapi
LDFLAGS    := -s -w \
	-X '$(PKG).Version=$(VERSION)' \
	-X '$(PKG).CommitSHA=$(COMMIT_SHA)' \
	-X '$(PKG).BuildDate=$(BUILD_DATE)'

# Image coordinates. Defaults target upstream Docker Hub; override for any mirror or
# private registry (ADR-027). Registry credentials are never stored here — authenticate
# with `docker login` or your CI's credential provider.
#   make docker REGISTRY=my-registry.local IMAGE_REPO=team/kubby
REGISTRY   ?= docker.io
IMAGE_REPO ?= kubby
TAG        ?= $(VERSION)

GOBIN := $(shell go env GOPATH)/bin

# Local development loads .env into the environment. In production these values come
# from a Kubernetes Secret, so the application itself never reads a dotenv file.
#
# A variable already present in the environment WINS: .env supplies defaults, it does
# not override what the caller asked for. Sourcing it the other way round silently
# undid explicit overrides — `make rotate-key` with a new key in the environment
# rotated to the old one and reported nothing to do.
define with_env
while IFS= read -r line || [ -n "$$line" ]; do \
  case "$$line" in ''|\#*) continue ;; *=*) ;; *) continue ;; esac; \
  key=$${line%%=*}; \
  eval "existing=\$${$$key+set}"; \
  [ -n "$$existing" ] || export "$$line"; \
done < .env 2>/dev/null || true;
endef

.PHONY: help
help: ## Show this help
	@grep -hE '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) \
		| awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-16s\033[0m %s\n", $$1, $$2}'

## ---------------------------------------------------------------- setup

.PHONY: setup
setup: ## Install dependencies and pinned tools
	cd $(SERVER_DIR) && go mod download
	cd $(WEB_DIR) && npm ci
	go install github.com/air-verse/air@$(AIR_VERSION)
	go install github.com/pressly/goose/v3/cmd/goose@$(GOOSE_VERSION)
	go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_VERSION)
	@echo "Setup complete. Enable the secret-scanning hook: git config core.hooksPath .githooks"

.PHONY: gen-key
gen-key: ## Generate a KUBBY_ENCRYPTION_KEY (copy the output into .env)
	@echo "KUBBY_ENCRYPTION_KEY=$$(openssl rand -base64 32)"

.PHONY: rotate-key
rotate-key: ## Rewrap stored secrets under a new key (see docs/ARCHITECTURE.md)
	@# Set KUBBY_ENCRYPTION_KEY to the new key, KUBBY_ENCRYPTION_KEY_PREVIOUS to the
	@# current one, and raise KUBBY_ENCRYPTION_KEY_VERSION. Add -dry-run to rehearse.
	@$(with_env) cd $(SERVER_DIR) && go run ./cmd/kubby-rotate-key $(ARGS)

## ---------------------------------------------------------------- develop

.PHONY: dev
dev: check-tools db-up reset-embedded ## Run Postgres, the API (hot reload) and the Vite dev server
	@echo ""
	@echo "  Kubby UI   http://localhost:5173   <- open this one"
	@echo "  Kubby API  http://localhost:8080   (API only in dev; it serves no UI)"
	@echo "  From Windows, if localhost fails: http://$$(hostname -I | awk '{print $$1}'):5173"
	@echo ""
	@$(with_env) \
	trap 'kill 0' EXIT INT TERM; \
	( cd $(SERVER_DIR) && $(GOBIN)/air ) & \
	( cd $(WEB_DIR) && npm run dev ) & \
	wait

.PHONY: reset-embedded
reset-embedded: ## Drop the embedded frontend so :8080 cannot serve a stale build
	@# A previous `make build` leaves a working but outdated UI embedded in the binary.
	@# In dev that is worse than no UI at all: :8080 would silently serve an older
	@# version than :5173, and the difference is invisible until something is missing.
	@rm -rf $(DIST_DIR)
	@mkdir -p $(DIST_DIR)
	@touch $(DIST_DIR)/.gitkeep
	@printf '%s\n' \
		'<!doctype html><meta charset="utf-8"><title>Kubby — development</title>' \
		'<body style="font-family:system-ui;background:#131313;color:#e8e8e8;padding:3rem">' \
		'<h1 style="color:#10b981">Kubby is running in development mode</h1>' \
		'<p>This port serves the API only. The user interface is on ' \
		'<a style="color:#10b981" href="http://localhost:5173">http://localhost:5173</a>.</p>' \
		'<p style="color:#9a9a9a">Run <code>make build</code> to embed the interface into the binary.</p>' \
		> $(DIST_DIR)/index.html

.PHONY: check-tools
check-tools: ## Verify the tools `make dev` needs are installed
	@missing=0; \
	for tool in air goose; do \
	  if [ ! -x "$(GOBIN)/$$tool" ]; then echo "missing tool: $$tool"; missing=1; fi; \
	done; \
	if [ ! -d "$(WEB_DIR)/node_modules" ]; then echo "missing: web/node_modules"; missing=1; fi; \
	if [ ! -f .env ]; then echo "missing: .env (cp .env.example .env, then make gen-key)"; missing=1; fi; \
	if [ -f .env ] && ! grep -qE '^KUBBY_ENCRYPTION_KEY=.+' .env; then echo ".env: KUBBY_ENCRYPTION_KEY is empty (run: make gen-key)"; missing=1; fi; \
	if [ -f .env ] && ! grep -qE '^KUBBY_DB_PASSWORD=.+' .env; then echo ".env: KUBBY_DB_PASSWORD is empty"; missing=1; fi; \
	if [ $$missing -ne 0 ]; then echo ""; echo "Run: make setup"; exit 1; fi

.PHONY: dev-api
dev-api: check-tools db-up ## Run only the API with hot reload
	@$(with_env) cd $(SERVER_DIR) && $(GOBIN)/air

.PHONY: dev-web
dev-web: ## Run only the Vite dev server
	cd $(WEB_DIR) && npm run dev

.PHONY: db-up
db-up: ## Start PostgreSQL
	docker compose up -d postgres
	@echo "Waiting for PostgreSQL..."
	@until docker compose exec -T postgres pg_isready -U kubby >/dev/null 2>&1; do sleep 1; done
	@echo "PostgreSQL is ready."

.PHONY: db-down
db-down: ## Stop PostgreSQL
	docker compose down

## ---------------------------------------------------------------- build

.PHONY: build
build: build-web build-server ## Build the frontend into a single Go binary

.PHONY: build-web
build-web: ## Build the frontend bundle into the embed directory
	cd $(WEB_DIR) && npm run build
	rm -rf $(DIST_DIR)
	mkdir -p $(DIST_DIR)
	cp -r $(WEB_DIR)/dist/. $(DIST_DIR)/
	# go:embed needs the directory to exist on a fresh clone, so the placeholder is
	# tracked and must survive every rebuild.
	touch $(DIST_DIR)/.gitkeep

.PHONY: build-server
build-server: ## Compile the server binary
	cd $(SERVER_DIR) && CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o bin/kubby ./cmd/kubby
	@echo "Built $(SERVER_DIR)/bin/kubby ($(VERSION) $(COMMIT_SHA))"

.PHONY: docker
docker: ## Build the container image (override REGISTRY / IMAGE_REPO for your registry)
	docker build \
		--build-arg REGISTRY=$(REGISTRY) \
		--build-arg VERSION=$(VERSION) \
		--build-arg COMMIT_SHA=$(COMMIT_SHA) \
		--build-arg BUILD_DATE=$(BUILD_DATE) \
		-t $(REGISTRY)/$(IMAGE_REPO):$(TAG) \
		-t $(REGISTRY)/$(IMAGE_REPO):$(COMMIT_SHA) .
	@echo "Built $(REGISTRY)/$(IMAGE_REPO):$(TAG) and :$(COMMIT_SHA)"

.PHONY: docker-push
docker-push: ## Push both tags (authenticate with `docker login` first)
	docker push $(REGISTRY)/$(IMAGE_REPO):$(TAG)
	docker push $(REGISTRY)/$(IMAGE_REPO):$(COMMIT_SHA)

## ---------------------------------------------------------------- quality

.PHONY: test
test: ## Run all tests (server runs under TZ=UTC, ADR-026)
	cd $(SERVER_DIR) && TZ=UTC go test -race ./...
	cd $(WEB_DIR) && npm run test -- --run

.PHONY: lint
lint: ## Run linters and type checks
	cd $(SERVER_DIR) && gofmt -l . | (! grep .) || (echo "gofmt issues above"; exit 1)
	cd $(SERVER_DIR) && go vet ./...
	@test -x $(GOBIN)/golangci-lint || (echo "golangci-lint missing; run: go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_VERSION)"; exit 1)
	cd $(SERVER_DIR) && $(GOBIN)/golangci-lint run ./...
	cd $(WEB_DIR) && npm run lint && npm run typecheck

.PHONY: security
security: ## Secret, dependency and image scanning (full suite lands in Faz 10)
	docker run --rm -v "$(PWD):/repo" -w /repo zricethezav/gitleaks:latest detect --redact --verbose
	cd $(SERVER_DIR) && go run golang.org/x/vuln/cmd/govulncheck@latest ./... || true
	cd $(WEB_DIR) && npm audit --audit-level=high || true

## ---------------------------------------------------------------- migrations

# Rendered from .env. Recipes using it are prefixed with @ so the password is never
# echoed to the terminal.
GOOSE_ARGS := -dir $(SERVER_DIR)/migrations postgres "$(shell grep -E '^KUBBY_DB_' .env 2>/dev/null | sed -e 's/KUBBY_DB_HOST=/host=/' -e 's/KUBBY_DB_PORT=/port=/' -e 's/KUBBY_DB_NAME=/dbname=/' -e 's/KUBBY_DB_USER=/user=/' -e 's/KUBBY_DB_PASSWORD=/password=/' -e 's/KUBBY_DB_SSLMODE=/sslmode=/' -e 's/KUBBY_DB_MAX_CONNS=.*//' | tr '\n' ' ')"

.PHONY: migrate-up
migrate-up: ## Apply all pending migrations
	@$(GOBIN)/goose $(GOOSE_ARGS) up

.PHONY: migrate-down
migrate-down: ## Roll back one migration
	@$(GOBIN)/goose $(GOOSE_ARGS) down

.PHONY: migrate-status
migrate-status: ## Show migration status
	@$(GOBIN)/goose $(GOOSE_ARGS) status

.PHONY: migrate-new
migrate-new: ## Create a migration: make migrate-new NAME=add_users
	@test -n "$(NAME)" || (echo "Usage: make migrate-new NAME=add_users"; exit 1)
	$(GOBIN)/goose -dir $(SERVER_DIR)/migrations create $(NAME) sql

.PHONY: clean
clean: ## Remove build artefacts
	rm -rf $(SERVER_DIR)/bin $(WEB_DIR)/dist $(DIST_DIR)/assets $(DIST_DIR)/index.html
	touch $(DIST_DIR)/.gitkeep
