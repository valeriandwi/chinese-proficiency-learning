# hanpath — HSK Chinese proficiency training app
#
# Everything a developer needs is behind a target here. `make dev` is the front
# door: infrastructure up, migrations applied, content imported, both processes
# running.

SHELL := /bin/bash
.DEFAULT_GOAL := help
GOTOOLCHAIN := local
export GOTOOLCHAIN
export PATH := $(PWD)/bin:$(PATH)

# Load .env for targets that need DATABASE_URL (godotenv does this for the
# binaries too; this is for the shell-level targets below).
ifneq (,$(wildcard .env))
include .env
export
endif

TEST_DATABASE_URL ?= postgres://hanpath:hanpath_dev_pw@127.0.0.1:5435/hanpath?sslmode=disable
CONTENT := $(wildcard content/*/*.yaml)

.PHONY: help
help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-22s\033[0m %s\n", $$1, $$2}'

# --- environment ------------------------------------------------------------

.PHONY: env
env: ## Create .env from .env.example if it does not exist
	@test -f .env || (cp .env.example .env && echo "created .env from .env.example")

# The dev database is an isolated LOCAL cluster started with pg_ctl — one process,
# no container runtime, on the same port the project has always used. Redis and
# Mailpit went with the containers because nothing here needs them: Redis is
# configuration only (the rate limiter lives in-process) and mail falls back to the
# log transport when SMTP_ADDR is unset. See docs/architecture.md.

# Prefer the newest Homebrew Postgres installed on this machine, but fall back to
# whatever initdb is on PATH so a differently installed cluster still works.
PG_PREFIX := $(shell brew --prefix postgresql@16 2>/dev/null || brew --prefix postgresql@15 2>/dev/null)
ifeq ($(wildcard $(PG_PREFIX)/bin/initdb),)
PG_BIN := $(dir $(shell command -v initdb))
else
# Trailing slash is load-bearing: every use below is "$(PG_BIN)initdb", so without
# it the path becomes ".../bininitdb" and the command silently does not exist.
PG_BIN := $(PG_PREFIX)/bin/
endif
PG_DATA := $(CURDIR)/.local/pgdata
PG_LOG  := $(CURDIR)/.local/postgres.log
PG_PORT := 5435
PG_URL  := 127.0.0.1:$(PG_PORT)

.PHONY: dev-db-init
dev-db-init: ## Create the local Postgres cluster (idempotent, once)
	@test -n "$(PG_BIN)" || (echo "no initdb found: install Postgres (brew install postgresql@16)"; exit 1)
	@mkdir -p $(CURDIR)/.local
	@test -f $(PG_DATA)/PG_VERSION || $(PG_BIN)initdb -D $(PG_DATA) -U hanpath --auth=trust --encoding=UTF8 --locale=C 2>&1 | tail -2
	@echo "cluster data: .local/pgdata (binaries: $(PG_BIN))"

.PHONY: dev-db-up
dev-db-up: dev-db-init ## Start the local Postgres cluster
	@$(PG_BIN)pg_ctl -D $(PG_DATA) -l $(PG_LOG) -o "-p $(PG_PORT) -c listen_addresses=127.0.0.1 -k /tmp" start >/dev/null 2>&1 || true
	@for i in $$(seq 1 60); do $(PG_BIN)pg_isready -h 127.0.0.1 -p $(PG_PORT) -U hanpath -d postgres >/dev/null 2>&1 && break; sleep 0.5; done
	@$(PG_BIN)pg_isready -h 127.0.0.1 -p $(PG_PORT) -U hanpath -d postgres >/dev/null 2>&1 || \
		(echo "postgres did not start on $(PG_URL) after 30s; last log lines:"; tail -5 $(PG_LOG); exit 1)
	@$(PG_BIN)createdb -h 127.0.0.1 -p $(PG_PORT) -U hanpath hanpath 2>/dev/null || true
	@echo "postgres ready on $(PG_URL) (log: .local/postgres.log)"

.PHONY: dev-db-down
dev-db-down: ## Stop the local cluster (keeps data)
	@$(PG_BIN)pg_ctl -D $(PG_DATA) stop -m fast >/dev/null 2>&1 || true
	@echo "postgres stopped (data kept in .local/pgdata)"

.PHONY: dev-db-status
dev-db-status: ## Show whether the local cluster is accepting connections
	@$(PG_BIN)pg_isready -h 127.0.0.1 -p $(PG_PORT) -U hanpath -d hanpath || true

.PHONY: dev-db-reset
dev-db-reset: ## Stop the cluster and delete its data directory
	@$(PG_BIN)pg_ctl -D $(PG_DATA) stop -m immediate >/dev/null 2>&1 || true
	@rm -rf $(PG_DATA)
	@echo "cluster data deleted; the next dev-db-up recreates it" 

# --- build and run ----------------------------------------------------------

.PHONY: build
build: ## Build the api, worker and contentctl binaries into ./bin
	go build -o bin/api ./cmd/api
	go build -o bin/worker ./cmd/worker
	go build -o bin/contentctl ./cmd/contentctl

.PHONY: migrate
migrate: ## Apply database migrations
	go run ./cmd/contentctl migrate

.PHONY: run-api
run-api: ## Run the API (rebuilds, so it never serves stale code)
	go run ./cmd/api

.PHONY: run-worker
run-worker: ## Run the background worker
	go run ./cmd/worker

.PHONY: dev
dev: env dev-db-up migrate content-import ## Infrastructure, schema and content, ready to run
	@echo
	@echo "ready. In separate terminals:"
	@echo "  make run-api      # http://127.0.0.1:8081"
	@echo "  make run-worker"
	@echo "  make web-dev      # http://localhost:5174"

# --- content ----------------------------------------------------------------

.PHONY: content-validate
content-validate: ## Validate every authored content file (no database needed)
	go run ./cmd/contentctl validate $(foreach f,$(CONTENT),--file $(f))

.PHONY: content-import
content-import: ## Import every authored content file
	go run ./cmd/contentctl import $(foreach f,$(CONTENT),--file $(f))

.PHONY: content-gen 
content-gen: ## Generate levels 2-5 from the published HSK 2.0 word lists
	@test -d .local/wordlists || (echo "missing .local/wordlists/*.json — the fetch command is in content/hsk2.0/VOCABULARY-LICENCE.txt" && exit 1)
	go run ./cmd/contentgen -levels 2,3,4,5

.PHONY: content-list 
content-list: ## List imported curricula
	go run ./cmd/contentctl list

# --- quality ----------------------------------------------------------------

.PHONY: test
test: ## Unit tests (no database required)
	go test ./... -count=1

.PHONY: test-integration
test-integration: dev-db-up migrate ## Integration tests against the real database
	HANPATH_TEST_DATABASE_URL="$(TEST_DATABASE_URL)" go test ./integration/ -count=1 -v

.PHONY: test-race
test-race: ## Unit tests with the race detector
	go test -race ./... -count=1

.PHONY: smoke
smoke: ## HTTP smoke test against a running API (BASE_URL=... to override)
	./scripts/smoke.sh

.PHONY: vet
vet: ## go vet across every package
	go vet ./...

.PHONY: fmt
fmt: ## Format Go sources
	gofmt -w .

.PHONY: lint
lint: vet fmt ## Vet and format
	@test -z "$$(gofmt -l .)" || (echo "unformatted files:" && gofmt -l . && exit 1)

.PHONY: check
check: lint test ## The pre-commit gate: format, vet, unit tests
	@echo "check passed"

# --- web --------------------------------------------------------------------

.PHONY: web-install
web-install: ## Install web dependencies and copy the stroke-order data they need
	cd web && pnpm install
	$(MAKE) web-hanzi-data

.PHONY: web-hanzi-data
web-hanzi-data: ## Copy stroke-order data for the characters the content uses
	cd web && pnpm run hanzi-data

.PHONY: web-dev
web-dev: ## Run the SPA dev server (proxies /api to the Go API)
	cd web && pnpm dev

.PHONY: web-build
web-build: ## Type-check and build the SPA
	cd web && pnpm build

.PHONY: web-test
web-test: ## Run the SPA unit tests
	cd web && pnpm test

.PHONY: openapi-types
openapi-types: ## Regenerate the SPA's API types from openapi/openapi.yaml
	cd web && pnpm exec openapi-typescript ../openapi/openapi.yaml -o src/api/schema.ts

.PHONY: clean
clean: ## Remove build output
	rm -rf bin web/dist
