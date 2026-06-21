-include .env
export

POSTGRES_PORT ?= 5432
DATABASE_URL ?= postgres://track:track@localhost:$(POSTGRES_PORT)/track?sslmode=disable
TEST_DATABASE_NAME ?= track_test
TEST_DATABASE_URL ?= postgres://track:track@localhost:$(POSTGRES_PORT)/$(TEST_DATABASE_NAME)?sslmode=disable
PORT ?= 8080
SEED_USERNAME ?= demo
SEED_PASSWORD ?= correct-horse-battery
SEED_NAME ?= Demo User
SEED_PROJECT_PREFIX ?= DEMO

.PHONY: run migrate seed up down db-logs test tidy build vet

run:
	DATABASE_URL='$(DATABASE_URL)' PORT='$(PORT)' go run ./cmd/trackd

build:
	go build ./...

vet:
	go vet ./...

test:
	TEST_DATABASE_URL='$(TEST_DATABASE_URL)' go test ./...

tidy:
	go mod tidy

migrate:
	DATABASE_URL='$(DATABASE_URL)' go run ./cmd/trackd -migrate-only

seed:
	DATABASE_URL='$(DATABASE_URL)' go run ./cmd/seed -username='$(SEED_USERNAME)' -password='$(SEED_PASSWORD)' -name='$(SEED_NAME)' -project-prefix='$(SEED_PROJECT_PREFIX)'

up:
	docker compose up -d
	@echo "Waiting for postgres..."
	@until docker compose exec -T postgres pg_isready -U track -d track >/dev/null 2>&1; do sleep 1; done
	@if ! docker compose exec -T postgres psql -U track -d postgres -tAc "SELECT 1 FROM pg_database WHERE datname = '$(TEST_DATABASE_NAME)'" | grep -q 1; then \
		echo "Creating test database $(TEST_DATABASE_NAME)..."; \
		docker compose exec -T postgres createdb -U track '$(TEST_DATABASE_NAME)'; \
	fi
	@echo "Postgres ready. Databases: track, $(TEST_DATABASE_NAME)."

down:
	docker compose down

db-logs:
	docker compose logs -f postgres
