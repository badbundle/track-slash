# track-slash

track-slash is a fast, open, API-first issue tracker built as a single Go application backed by PostgreSQL.

## Development

You will need Go 1.26.3, Node.js 20 or newer, and Docker.

```sh
cp .env.example .env
make up
make run
```

The app will be available at [http://localhost:8080](http://localhost:8080). Run `make seed` to add demo data.

Other useful commands:

```sh
make test
make build
make assets
make assets-check
make down
```

Frontend dependencies are pinned in `package-lock.json`. Generated CSS and JavaScript are committed under `internal/server/static` so the Go binary and Docker image do not need Node.js at runtime. Run `make assets` after changing templates, Tailwind source, or frontend dependencies; CI runs `make assets-check` to catch stale output.

## Issues

Issues are tracked in [track-slash](https://trackslash.com/badbundle/projects/TRACK), not GitHub Issues.

## License

[MIT](LICENSE)
