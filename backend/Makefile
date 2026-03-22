.PHONY: dev build run test lint db-reset db-dev db-prod

# Build the server binary
build:
	go build -o bin/server ./cmd/server

# Run the server
run: build
	./bin/server

# Run with hot reload (requires: go install github.com/air-verse/air@latest)
dev:
	air

# Run tests
test:
	go test ./... -v -race

# Run linter (requires: go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest)
lint:
	golangci-lint run

# Reset the database (drops volume and recreates)
db-reset:
	docker compose -f ../docker-compose.yml down -v
	docker compose -f ../docker-compose.yml up -d

# Connect to dev Cloud SQL via Auth Proxy (runs proxy in background, connects psql, cleans up on exit)
db-dev:
	@echo "Starting Cloud SQL Auth Proxy for dev..."
	@cloud-sql-proxy tardi-dev-488420:us-central1:tardi-db-dev --port 5433 & \
	PROXY_PID=$$!; \
	sleep 2; \
	echo "Connecting to dev database..."; \
	psql "postgres://tardi@localhost:5433/tardi"; \
	kill $$PROXY_PID 2>/dev/null

# Connect to prod Cloud SQL via Auth Proxy (runs proxy in background, connects psql, cleans up on exit)
db-prod:
	@echo "Starting Cloud SQL Auth Proxy for prod..."
	@cloud-sql-proxy tardi-prod-488420:us-central1:tardi-db-prod --port 5434 & \
	PROXY_PID=$$!; \
	sleep 2; \
	echo "Connecting to prod database..."; \
	psql "postgres://tardi@localhost:5434/tardi"; \
	kill $$PROXY_PID 2>/dev/null
