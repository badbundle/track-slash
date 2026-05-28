# Agent guide — track-slash

## Test coverage policy

**Aim for 100% branch coverage on business logic.** Branch coverage, not just line coverage — every condition, every error mapping, every state transition must be exercised.

What counts as business logic:

- Store CRUD and transactional flows (`internal/store/`)
- HTTP handlers and request validation (`internal/server/`)
- Domain enums, transitions, invariants (`internal/model/`)
- Realtime event routing (`internal/realtime/event.go`, `hub.go`)

What is exempt:

- `cmd/trackd/main.go` wiring
- `internal/config/` env parsing
- `internal/migrations/migrations.go` goose glue
- Pure defensive `return err` after a successful no-rows / known-pgcode check, where the only way to trigger it is a DB outage. Document these with a one-line comment so a reviewer can see they were considered.

When adding a feature:

1. Before merging, run `make test` with `DATABASE_URL` set and inspect `go tool cover -func=...` on the touched packages.
2. Every new branch in a handler, store method, or model transition needs a test case.
3. New error mappings (pg codes → sentinel errors) need a test that triggers the mapped code path through the public API, not the underlying pgx error directly.
4. New realtime entity/topic combinations need both a hub unit test and a listener integration test.

## How tests are organised

- Unit tests: `*_test.go` next to the code, no DB.
- Integration tests: `*_integration_test.go`, gated on `TEST_DATABASE_URL` or `DATABASE_URL`. Skip when env var is unset so `go test ./...` stays green without infra.
- Server tests use `httptest.NewServer(srv.Router())` against a real store and Postgres.
- Realtime tests subscribe to topics via `Hub.Subscribe` and assert events arrive via the same path production clients use.

## Running tests locally

```bash
make up                                     # boot Postgres on $POSTGRES_PORT
DATABASE_URL='postgres://track:track@localhost:5436/track?sslmode=disable' \
  go test -count=1 -coverprofile=/tmp/cover.out ./internal/...
go tool cover -func=/tmp/cover.out | sort
```

The repo's docker-compose maps `5432` on the container; `POSTGRES_PORT` in `.env` controls the host port (default `5432`, dev typically `5436`).

## Conventions worth knowing

- Sentinel errors `store.ErrNotFound` and `store.ErrConflict`; `writeStoreError` maps them to 404/409.
- Postgres error codes: `23503` (FK), `23505` (unique), `23514` (CHECK). Map these in the store, never let pgx errors leak to handlers.
- One transaction per store call when crossing more than one table or doing read-then-write under contention.
- Goose migrations are `+goose StatementBegin/End` per logical block; never inline multiple `CREATE`s without statement markers.
- Realtime events are emitted by the `track_emit_event` Postgres trigger. New tables that need realtime need a trigger and a topic. Sprint events ride the existing channel — see `0003_sprints.sql` for the pattern.

## When in doubt

Read the existing tests for the closest analogous feature. `internal/store/sprints_integration_test.go` and `internal/server/sprints_integration_test.go` are the current reference for full-coverage feature tests.
