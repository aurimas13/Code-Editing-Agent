# Common tasks. Run `make help` for a list.

.PHONY: help dev server web cli test test-integration mutation evals evals-live gen steps check build docker

help: ## Show this list
	@grep -E '^[a-z-]+:.*##' $(MAKEFILE_LIST) | awk -F':.*## ' '{printf "  %-18s %s\n", $$1, $$2}'

dev: ## Run the API and the website together (demo mode if ANTHROPIC_API_KEY is unset)
	@cd web && npm install --silent
	@trap 'kill 0' INT TERM; \
	ALLOWED_ORIGINS=http://localhost:3000 go run ./cmd/server & \
	(cd web && npm run dev) & \
	wait

server: ## Run only the API on :8080
	ALLOWED_ORIGINS=http://localhost:3000 go run ./cmd/server

web: ## Run only the website on :3000
	cd web && npm install --silent && npm run dev

cli: ## Chat with the agent in the terminal, in a scratch folder
	@mkdir -p /tmp/agent-workspace && cp -n internal/server/seed/* /tmp/agent-workspace/ 2>/dev/null || true
	go run ./cmd/agent -dir /tmp/agent-workspace

test: ## Go tests (with the race detector) and web tests
	go vet ./...
	go test -race ./...
	cd web && npm test

test-integration: ## Store and access-rule tests against a real database (see docs/DEPLOY.md)
	go test -count=1 -run Integration -v ./internal/store/

mutation: ## Break each safeguard in a scratch copy and check a test fails
	python3 scripts/mutation_check.py

evals: ## Run the deterministic eval suites and write the report
	go run ./cmd/evals

evals-live: ## Also run the live-model suite (needs ANTHROPIC_API_KEY, costs a few cents)
	go run ./cmd/evals -live

gen: ## Regenerate the website's data files from the Go source
	go run ./cmd/gen

steps: ## Regenerate tutorial checkpoints 01-05 from the finished file
	python3 scripts/make_guide_steps.py

check: steps gen ## Everything CI checks
	test -z "$$(gofmt -l .)"
	$(MAKE) test
	go run ./cmd/evals -out /tmp/evals.json -web ""
	cd web && npm run typecheck && npm run build

build: ## Build the three binaries into ./bin
	go build -trimpath -o bin/ ./cmd/agent ./cmd/server ./cmd/evals

docker: ## Build the API image
	docker build -t code-editing-agent .
