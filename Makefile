DATABASE_URL ?= postgres://track:track@localhost:5432/track?sslmode=disable
PORT ?= 8080

.PHONY: run migrate up down db-logs test tidy build vet

run:
	DATABASE_URL='$(DATABASE_URL)' PORT='$(PORT)' go run ./cmd/trackd

build:
	go build ./...

vet:
	go vet ./...

test:
	go test ./...

tidy:
	go mod tidy

migrate:
	DATABASE_URL='$(DATABASE_URL)' go run ./cmd/trackd -migrate-only

up:
	docker compose up -d
	@echo "Waiting for postgres..."
	@until docker compose exec -T postgres pg_isready -U track -d track >/dev/null 2>&1; do sleep 1; done
	@echo "Postgres ready."

down:
	docker compose down

db-logs:
	docker compose logs -f postgres
