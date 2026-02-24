.PHONY: dev build run test lint db-reset

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
