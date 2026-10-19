# Every target here is runnable from a clean clone. `make demo` needs a browser; nothing needs a cluster,
# a cloud account or a credential.
SHELL := /bin/bash
.DEFAULT_GOAL := help

.PHONY: help
help: ## List the targets that matter
	@grep -hE '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) | awk 'BEGIN{FS=":.*?## "}{printf "  \033[36m%-14s\033[0m %s\n", $$1, $$2}'

.PHONY: build
build: ## Build the orchestrator
	go build -o bin/orchestrator ./cmd/orchestrator

.PHONY: test
test: ## Unit tests: no browser needed for most, no network, no credentials
	go test -race ./internal/...

.PHONY: test-drivers
test-drivers: ## The drivers: a real browser, and a fake adb for the Android path
	go test -race -v ./internal/driver/ -run 'TestBrowserDrivesAPage|TestErrorsNameTheSelector|TestADB'

.PHONY: coverage
coverage: ## Coverage per package, and a total
	@go test -cover ./internal/... 2>&1 | tee /tmp/uito-cover.txt
	@echo "total: $$(go test -coverprofile=/tmp/uito.cov ./internal/... >/dev/null 2>&1; go tool cover -func=/tmp/uito.cov | tail -1)"

COMPOSE ?= docker compose

.PHONY: up
up: ## Start the browser sidecar and the fixture app in the background, and wait for both
	@$(COMPOSE) down --remove-orphans >/dev/null 2>&1 || true
	@$(COMPOSE) up -d --build browser fixture >/dev/null
	@ok=0; for i in $$(seq 1 60); do \
	  if curl -fsS http://127.0.0.1:19222/json/version >/dev/null 2>&1; then ok=1; break; fi; \
	  sleep 0.5; \
	done; \
	if [ $$ok -eq 1 ]; then echo "  the browser sidecar answers (published on 127.0.0.1:19222)"; else \
	  echo "  the browser sidecar never answered:"; $(COMPOSE) logs browser 2>&1 | tail -6 | sed 's/^/    /'; $(COMPOSE) down >/dev/null 2>&1; exit 1; fi
	@$(COMPOSE) ps

.PHONY: down
down: ## Stop the containers
	@$(COMPOSE) down --remove-orphans

.PHONY: compose-suite
compose-suite: up ## Run the whole suite in containers, against the browser sidecar
	@$(COMPOSE) up --build --abort-on-container-exit --exit-code-from suite suite
	@$(COMPOSE) down --remove-orphans >/dev/null 2>&1 || true

.PHONY: demo
demo: ## Serve the fixture app and run the whole suite against it
	@./scripts/demo.sh

.PHONY: suite
suite: ## Run the suite against any URL: make suite URL=http://localhost:3000
	@./scripts/suite.sh "$(URL)"

.PHONY: visual
visual: ## What a screenshot assertion catches that a text assertion cannot
	@./scripts/measure-visual.sh

.PHONY: flake
flake: ## The same checks written two ways, with the flake rate of each
	@./scripts/measure-flake.sh

.PHONY: flakes
flakes: ## A real flake: a timeout too short against a timeout long enough
	@./scripts/measure-flakes-true.sh

.PHONY: policy
policy: ## Quarantine and release, in transitions you can reproduce
	@./scripts/demo-policy.sh

.PHONY: airtest
airtest: build ## Read an Airtest log: make airtest LOG=path/to/log.txt
	@test -n "$(LOG)" || { echo "usage: make airtest LOG=path/to/log.txt"; exit 1; }
	./bin/orchestrator airtest --log "$(LOG)"

.PHONY: capture
capture: ## Capture a template: make capture URL=... SELECTOR=... OUT=file.png
	@./bin/orchestrator capture --url "$(URL)" --selector "$(SELECTOR)" --out "$(OUT)"

.PHONY: policy-show
policy-show: ## Show the committed flake history in a readable form
	@./bin/orchestrator policy --state state/quarantine.json

.PHONY: fmt
fmt: ## Format, reporting what changed
	@gofmt -l . | grep -v '^$$' | sed 's/^/not formatted: /' || true
	@test -z "$$(gofmt -l . | grep -v '^$$')" || (echo "run gofmt -w ."; exit 1)

.PHONY: vet
vet: ## go vet
	@go vet ./...

.PHONY: lint
lint: fmt vet ## fmt + vet

.PHONY: chart
chart: ## Lint the chart and render every branch
	@./scripts/chart-check.sh

.PHONY: image
image: ## Build the image and prove it runs non-root and serves the fixture app
	@./scripts/image.sh

.PHONY: diagram
diagram: ## Regenerate the figure and check it against its sources
	@python3 docs/figures/ui-test-orchestrator-flow.generator.py
	@bash ~/.pi/agent/scripts/diagram-freshness.sh 2>/dev/null | grep -E "FRESH|STALE|TOUCHED|NO-META" | sed 's/^/  /'

.PHONY: clean
clean: ## Remove build output and reports
	@rm -rf bin report report-visual && echo "cleaned"
