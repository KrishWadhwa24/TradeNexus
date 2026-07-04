.PHONY: help deps up down logs run tidy test fmt vet clean

help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | \
		awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-10s\033[0m %s\n", $$1, $$2}'

deps: ## Start Postgres + Redis (docker compose)
	docker compose up -d

up: deps ## Alias for deps

down: ## Stop containers (keeps data volume)
	docker compose down

nuke: ## Stop containers AND delete the Postgres data volume
	docker compose down -v

logs: ## Tail container logs
	docker compose logs -f

tidy: ## Resolve/download Go module dependencies
	go mod tidy

run: ## Run the API server (applies migrations on boot)
	go run ./cmd/server

test: ## Run tests (rate-limiter test needs Redis up)
	go test ./...

fmt: ## Format code
	go fmt ./...

vet: ## Static checks
	go vet ./...

clean: ## Remove build artifacts
	rm -rf bin
