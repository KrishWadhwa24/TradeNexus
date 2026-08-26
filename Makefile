.PHONY: help deps up down logs run build tidy test fmt vet clean \
        service-start service-stop service-restart service-status service-logs

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

build: ## Build the API server binary to bin/tradenexus-server (for launchd/pm2, not `go run`)
	go build -o bin/tradenexus-server ./cmd/server

test: ## Run tests (rate-limiter test needs Redis up)
	go test ./...

fmt: ## Format code
	go fmt ./...

vet: ## Static checks
	go vet ./...

clean: ## Remove build artifacts
	rm -rf bin

# The server runs as a launchd agent (~/Library/LaunchAgents/com.tradenexus.server.plist)
# so it survives terminal close/crash. Manage it with these, not Ctrl-C.
service-start: build ## Rebuild + start the launchd-managed server (starts even if already stopped)
	launchctl bootstrap gui/$$(id -u) ~/Library/LaunchAgents/com.tradenexus.server.plist 2>/dev/null || true
	launchctl kickstart -k gui/$$(id -u)/com.tradenexus.server

service-stop: ## Stop the server and prevent it from auto-restarting until you start it again
	launchctl bootout gui/$$(id -u)/com.tradenexus.server

service-restart: build ## Rebuild + restart the running server (use after code changes)
	launchctl kickstart -k gui/$$(id -u)/com.tradenexus.server

service-status: ## Show whether the server is running (and its PID)
	launchctl print gui/$$(id -u)/com.tradenexus.server 2>&1 | grep -E "state|pid" || echo "not loaded — run 'make service-start'"

service-logs: ## Tail the running server's logs
	tail -f logs/server.out.log
