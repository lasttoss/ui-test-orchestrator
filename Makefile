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
