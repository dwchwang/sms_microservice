# VCS-SMS Makefile
# =============================================

.PHONY: help dev-up dev-down infra-up infra-down build test coverage seed logs clean rebuild-db

help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-20s\033[0m %s\n", $$1, $$2}'

# ── Infrastructure ──
infra-up: ## Start infrastructure containers (Postgres, Redis, ES, TCP Sim)
	docker-compose -f docker-compose.dev.yml up -d

infra-down: ## Stop infrastructure containers
	docker-compose -f docker-compose.dev.yml down

dev-up: ## Start infrastructure only (for local development)
	@echo "Starting infrastructure..."
	docker-compose -f docker-compose.dev.yml up -d
	@echo "Waiting for services to be healthy..."
	@sleep 10
	@echo "Infrastructure is ready."

dev-down: ## Stop dev environment
	docker-compose -f docker-compose.dev.yml down

# ── Build ──
build: ## Build all service Docker images
	docker-compose build

# ── Test ──
test: ## Run all tests with coverage
	@echo "=== Testing shared ==="
	cd shared && go test ./... -cover -count=1 -v
	@echo "=== Testing auth-service ==="
	cd auth-service && go test ./... -cover -count=1 -v
	@echo "=== Testing server-service ==="
	cd server-service && go test ./... -cover -count=1 -v
	@echo "=== Testing monitor-service ==="
	cd monitor-service && go test ./... -cover -count=1 -v
	@echo "=== Testing report-service ==="
	cd report-service && go test ./... -cover -count=1 -v
	@echo "=== Testing tcp-simulator ==="
	cd tcp-simulator && go test ./... -cover -count=1 -v
	@echo "All tests completed."

coverage: ## Generate HTML coverage reports
	@for svc in shared auth-service server-service monitor-service report-service tcp-simulator; do \
		echo ">>> $$svc coverage:"; \
		cd $$svc && go test ./... -coverprofile=coverage.out -covermode=atomic -count=1; \
		go tool cover -func=coverage.out | tail -1; \
		go tool cover -html=coverage.out -o coverage.html; \
		cd ..; \
	done
	@echo "Coverage reports generated."

# ── Mock Generation ──
mocks: ## Generate mocks with mockery
	@echo "Generating mocks..."
	@go install github.com/vektra/mockery/v2@latest 2>/dev/null || true
	mockery
	@echo "Mocks generated."

# ── Deps ──
deps: ## Install all dependencies
	cd shared && go mod tidy
	cd auth-service && go mod tidy
	cd server-service && go mod tidy
	cd monitor-service && go mod tidy
	cd report-service && go mod tidy
	cd tcp-simulator && go mod tidy

# ── Run ──
rebuild-cache: ## Rebuild the Redis monitor target projection from PostgreSQL
	docker compose exec server-service ./server-service rebuild-monitor-cache

seed: ## Seed 10.000 servers into PostgreSQL (run rebuild-cache afterwards)
	docker exec -it vcs-sms-postgres sh -c 'psql -U "$${POSTGRES_USER:-postgres}" -d "server_db" -f /seed/seed_10k_servers.sql'

logs: ## Tail all service logs
	docker-compose logs -f

logs-traefik: ## Tail traefik logs
	docker-compose logs -f traefik

logs-monitor: ## Tail monitor logs
	docker-compose logs -f monitor-service

# ── Clean ──
clean: ## Clean all containers, volumes, and build artifacts
	docker-compose down -v
	rm -rf logs/* uploads/*
	@echo "Cleaned."

rebuild-db: ## Rebuild database from scratch
	docker-compose down -v
	docker-compose up -d postgres
