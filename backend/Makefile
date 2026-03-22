.PHONY: dev build run test lint db-reset db-dev db-prod db-query-dev db-query-prod

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

# Interactive psql shell to dev Cloud SQL
db-dev:
	@./scripts/db-query.sh dev "\\conninfo" && ./scripts/db-query.sh dev

# Interactive psql shell to prod Cloud SQL
db-prod:
	@./scripts/db-query.sh prod "\\conninfo" && ./scripts/db-query.sh prod

# Run a query against dev Cloud SQL: make db-query-dev Q="SELECT count(*) FROM users"
db-query-dev:
	@./scripts/db-query.sh dev "$(Q)"

# Run a query against prod Cloud SQL: make db-query-prod Q="SELECT count(*) FROM users"
db-query-prod:
	@./scripts/db-query.sh prod "$(Q)"
