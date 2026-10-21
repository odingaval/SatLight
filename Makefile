.PHONY: help dev seed test test-int test-ln lint openapi fmt vet vulncheck build docker-build docker-up docker-down migrate-up migrate-down

help:
	@echo "Satlight - Bitcoin-native marketplace for open-source contributions"
	@echo ""
	@echo "Available commands:"
	@echo "  dev           Start local development environment (API + worker + DB + Lightning)"
	@echo "  seed          Load sample projects and bounties"
	@echo "  test          Run unit tests"
	@echo "  test-int      Run integration tests (requires containers)"
	@echo "  test-ln       Run Lightning tests on regtest"
	@echo "  lint          Run golangci-lint, go vet, and govulncheck"
	@echo "  fmt           Format Go code"
	@echo "  vet           Run go vet"
	@echo "  vulncheck     Run govulncheck"
	@echo "  openapi       Validate OpenAPI specification"
	@echo "  build         Build API and worker binaries"
	@echo "  docker-build  Build Docker images"
	@echo "  docker-up     Start Docker Compose services"
	@echo "  docker-down   Stop Docker Compose services"
	@echo "  migrate-up    Run database migrations"
	@echo "  migrate-down  Rollback last migration"
	@echo "  generate      Generate code (SQLC, mocks, etc.)"

dev: docker-up
	@echo "Waiting for services to be ready..."
	@sleep 5
	@$(MAKE) migrate-up
	@echo "Starting API and worker..."
	@go run ./cmd/api & go run ./cmd/worker

seed:
	@echo "Seeding database with sample data..."
	@go run ./cmd/seed

test:
	go test -v -race -count=1 ./...

test-int:
	docker compose -f docker-compose.test.yml up --build --abort-on-container-exit --exit-code-from test

test-ln:
	@echo "Running Lightning integration tests on regtest..."
	@go test -v -tags=integration ./internal/lightning/...

lint:
	golangci-lint run ./...
	go vet ./...
	govulncheck ./...

fmt:
	gofmt -w .
	goimports -w .

vet:
	go vet ./...

vulncheck:
	govulncheck ./...

openapi:
	@echo "Validating OpenAPI spec..."
	@which swagger-codegen || (echo "swagger-codegen not installed" && exit 1)
	swagger-codegen validate -i openapi.yaml

build:
	CGO_ENABLED=1 go build -o bin/satlight-api ./cmd/api
	CGO_ENABLED=1 go build -o bin/satlight-worker ./cmd/worker

docker-build:
	docker compose build

docker-up:
	docker compose up -d
	@echo "Services started. API at http://localhost:8080, Web at http://localhost:3000"

docker-down:
	docker compose down -v

migrate-up:
	@echo "Running migrations..."
	@go run ./cmd/migrate up

migrate-down:
	@echo "Rolling back last migration..."
	@go run ./cmd/migrate down

generate:
	@echo "Generating code..."
	@which sqlc || (echo "sqlc not installed" && exit 1)
	sqlc generate
	@which mockgen || (echo "mockgen not installed" && exit 1)
	go generate ./...

clean:
	rm -rf bin/
	docker compose down -v --rmi local

.DEFAULT_GOAL := help