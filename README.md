# track-slash

track-slash is a fast, open, API-first issue tracker built as a single Go application backed by PostgreSQL.

## Development

You will need Go 1.26.3 and Docker.

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
make down
```

## Issues

Issues are tracked in [track-slash](https://trackslash.com/badbundle/projects/TRACK), not GitHub Issues.

## License

[MIT](LICENSE)
