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
# Marked dirty when the tree has uncommitted changes, so an image never claims to be a
# commit it is not built from.
COMMIT_SHA := $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)$(shell test -n "$$(git status --porcelain 2>/dev/null)" && echo -dirty)
BUILD_DATE := $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
PKG        := github.com/erolbeyaz/kubby/internal/httpapi
LDFLAGS    := -s -w \
	-X '$(PKG).Version=$(VERSION)' \
	-X '$(PKG).CommitSHA=$(COMMIT_SHA)' \
	-X '$(PKG).BuildDate=$(BUILD_DATE)'

# Two registries, deliberately separate. Conflating them means asking the registry you
# publish to for the golang and node base images, which it does not have.
#
#   REGISTRY        where the BASE images are pulled from (ADR-027: mirror, proxy cache)
#   IMAGE_REGISTRY  where the built image is TAGGED and PUSHED
#
#   make release VERSION=0.9.0 IMAGE_REGISTRY=localhost:5000
#   make release VERSION=0.9.0 IMAGE_REGISTRY=docker.io/erolbeyaz
#   make docker  REGISTRY=my-mirror.local          # build through a mirror
#
# Credentials are never stored here — authenticate with `docker login`.
REGISTRY       ?= docker.io
IMAGE_REGISTRY ?= $(REGISTRY)

# The two tools the cluster terminal runs (ADR-094). Kept here as well as in the
# Dockerfile so `docker-verify` checks the image against the version this repo intends
# rather than against whatever the image happens to contain.
KUBECTL_VERSION ?= v1.36.4
HELM_VERSION    ?= v4.2.3
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
dev: check-tools one-dev db-up reset-embedded ## Run Postgres, the API (hot reload) and the Vite dev server
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

.PHONY: one-dev
one-dev: ## Refuse to start a second dev stack over a running one
	@# Two stacks race for :8080 and :5173, and both write the same binary while the
	@# other is executing it. The symptoms are wild — a server that answers with an old
	@# build, or one that fails where the same code works from a shell — and none of
	@# them point at the cause. Better to refuse than to debug that twice.
	@# pgrep -f matches against every process's whole command line, including the shell
	@# running this very check — the pattern is right there in its arguments. The guard
	@# then reported a running stack when there was none, and refused to start one. The
	@# exact-name form looks at the executable instead, which is what was meant.
	@if pgrep -x air >/dev/null 2>&1; then \
		echo "A dev stack is already running."; \
		echo "Stop it (Ctrl-C in its terminal, or 'make dev-stop') before starting another."; \
		exit 1; \
	fi

.PHONY: dev-stop
dev-stop: ## Stop a dev stack started in another terminal
	@# `pkill -f` matches whole command lines, so the pattern matched the shell running
	@# the pkill — it killed itself, make reported "Terminated", and whether the dev stack
	@# actually stopped was luck. The pids are collected first and this shell excluded.
	@pids=$$(pgrep -x air 2>/dev/null); \
	for pid in $$pids; do kill $$pid 2>/dev/null || true; done
	@pids=$$(pgrep -f 'node.*vite' 2>/dev/null | grep -v "^$$$$$$" || true); \
	for pid in $$pids; do kill $$pid 2>/dev/null || true; done
	@echo "Stopped." 

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
		-t $(IMAGE_REGISTRY)/$(IMAGE_REPO):$(TAG) \
		-t $(IMAGE_REGISTRY)/$(IMAGE_REPO):$(COMMIT_SHA) .
	@echo "Built $(IMAGE_REGISTRY)/$(IMAGE_REPO):$(TAG) and :$(COMMIT_SHA)"

.PHONY: config-export
config-export: ## Export clusters, users, grants and settings to an encrypted archive
	@test -n "$$KUBBY_BACKUP_PASSPHRASE" || \
		(echo "Set KUBBY_BACKUP_PASSPHRASE first. It is the only thing protecting the archive."; exit 1)
	@$(with_env) cd $(SERVER_DIR) && go run ./cmd/kubby-backup -export $(or $(OUT),kubby-$(shell date +%F).bak)

.PHONY: config-restore
config-restore: ## Restore an archive. IN=path is required; add DRY_RUN=1 to preview.
	@test -n "$(IN)" || (echo "Give the archive: make config-restore IN=kubby-2026-08-25.bak"; exit 1)
	@test -n "$$KUBBY_BACKUP_PASSPHRASE" || (echo "Set KUBBY_BACKUP_PASSPHRASE first."; exit 1)
	@$(with_env) cd $(SERVER_DIR) && go run ./cmd/kubby-backup -restore $(IN) $(if $(DRY_RUN),-dry-run,)

.PHONY: registry-up
registry-up: ## Start a local image registry on 127.0.0.1:5000
	@docker start kubby-registry 2>/dev/null || \
		docker run -d --name kubby-registry --restart unless-stopped \
			-p 127.0.0.1:5000:5000 \
			-v kubby-registry-data:/var/lib/registry \
			registry:3.0.0
	@until curl -sf http://localhost:5000/v2/ >/dev/null 2>&1; do sleep 1; done
	@echo "Registry ready on localhost:5000"

.PHONY: registry-list
registry-list: ## Show what the local registry holds
	@curl -s http://localhost:5000/v2/_catalog
	@echo
	@curl -s http://localhost:5000/v2/$(IMAGE_REPO)/tags/list
	@echo

.PHONY: release
release: ## Build a versioned image and push it. VERSION is required.
	@test "$(VERSION)" != "dev" || (echo "Set a version: make release VERSION=0.9.0"; exit 1)
	@echo "==> building $(IMAGE_REGISTRY)/$(IMAGE_REPO):$(VERSION) ($(COMMIT_SHA))"
	docker build \
		--build-arg REGISTRY=$(REGISTRY) \
		--build-arg VERSION=$(VERSION) \
		--build-arg COMMIT_SHA=$(COMMIT_SHA) \
		--build-arg BUILD_DATE=$(BUILD_DATE) \
		-t $(IMAGE_REGISTRY)/$(IMAGE_REPO):$(VERSION) .
	@$(MAKE) --no-print-directory docker-verify TAG=$(VERSION)
	@echo "==> pushing"
	docker push $(IMAGE_REGISTRY)/$(IMAGE_REPO):$(VERSION)
	@echo
	@echo "Pushed $(IMAGE_REGISTRY)/$(IMAGE_REPO):$(VERSION)"
	@echo "Put this in deploy/compose/.env:"
	@echo "  KUBBY_IMAGE=$(IMAGE_REGISTRY)/$(IMAGE_REPO):$(VERSION)"
	@# No `latest` tag on purpose: it cannot say which version is running and it makes
	@# rolling back impossible.

.PHONY: tag
tag: ## Tag the current commit as a release. VERSION is required.
	@test "$(VERSION)" != "dev" || (echo "Set a version: make tag VERSION=0.9.0"; exit 1)
	@# A tag on a dirty tree points at a commit that does not contain what was built.
	@test -z "$$(git status --porcelain)" || \
		(echo "The working tree has uncommitted changes. Commit them before tagging."; \
		 git status --short; exit 1)
	@git tag -a v$(VERSION) -m "Kubby v$(VERSION)"
	@echo "Tagged v$(VERSION) at $$(git rev-parse --short HEAD)"
	@echo "Push it when you are ready:  git push origin v$(VERSION)"

.PHONY: smoke-audit
smoke-audit: ## Verify the audit sinks against real Elasticsearch and Loki
	@echo "==> starting receivers"
	docker compose --profile observability up -d
	@echo "==> waiting for Elasticsearch"
	@until curl -sf http://localhost:9200/_cluster/health >/dev/null; do sleep 3; done
	@echo "==> waiting for Loki"
	@until curl -sf http://localhost:3100/ready 2>/dev/null | grep -q '^ready'; do sleep 3; done
	@echo "==> pushing"
	cd $(SERVER_DIR) && KUBBY_TEST_ELASTICSEARCH=http://localhost:9200 \
		KUBBY_TEST_LOKI=http://localhost:3100 \
		TZ=UTC go test ./internal/audit/ -run Live -count=1 -v
	@echo "OK — both receivers accepted and returned what was sent"
	@echo "     Kibana  http://localhost:5601"
	@echo "     Grafana http://localhost:3000"

.PHONY: docker-verify
docker-verify: ## Check the built image is what it claims: right tools, right user, no shell
	@echo "==> kubectl"
	@out=$$(docker run --rm --entrypoint /usr/local/bin/kubectl $(IMAGE_REGISTRY)/$(IMAGE_REPO):$(TAG) \
		version --client=true 2>&1); \
	case "$$out" in *"$(KUBECTL_VERSION)"*) ;; \
		*) echo "kubectl is missing or not $(KUBECTL_VERSION) — the cluster terminal cannot run:"; \
		   echo "$$out"; exit 1 ;; esac
	@echo "==> helm"
	@out=$$(docker run --rm --entrypoint /usr/local/bin/helm $(IMAGE_REGISTRY)/$(IMAGE_REPO):$(TAG) \
		version --short 2>&1); \
	case "$$out" in *"$(HELM_VERSION)"*) ;; \
		*) echo "helm is missing or not $(HELM_VERSION) — the cluster terminal cannot run helm:"; \
		   echo "$$out"; exit 1 ;; esac
	@echo "==> non-root"
	@test "$$(docker inspect $(IMAGE_REGISTRY)/$(IMAGE_REPO):$(TAG) --format '{{.Config.User}}')" = "65532:65532" \
		|| (echo "the image does not run as uid 65532"; exit 1)
	@echo "==> no shell"
	@! docker run --rm --entrypoint /bin/sh $(IMAGE_REGISTRY)/$(IMAGE_REPO):$(TAG) -c true 2>/dev/null \
		|| (echo "a shell is reachable in the image; it must not be"; exit 1)
	@echo "OK — tools present, runs as 65532, no shell"

.PHONY: docker-push
docker-push: ## Push both tags (authenticate with `docker login` first)
	docker push $(IMAGE_REGISTRY)/$(IMAGE_REPO):$(TAG)
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
