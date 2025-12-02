# Contributing to signalsd

## Approach

This project prefers code that is easy to understand, even if it means some repetition, and avoids abstractions that don't solve current problems.

## Before You Start

- Read the [README](README.md) 
- Check existing [issues](https://github.com/information-sharing-networks/signalsd/issues) to see if your idea is already being discussed
- For significant changes, open an issue first to discuss the approach

## Development Setup

Use the Docker development environment:

```bash
git clone https://github.com/information-sharing-networks/signalsd.git
cd signalsd
docker compose up
```

The service runs on http://localhost:8080 with API docs at `/docs`.

**Note**: Even when using Docker, you should have Go 1.25.4 or above installed locally for code editing, linting, and running local tests. See the [Local Development Setup](README.md#local-development-setup-macos) section in the README for details.

## Testing

Nearly all the testing is done via integration tests that live in `app/test/integration/`.  See the [integration testing guide](app/test/integration/README.md) for more information.

Performance tests are in `test/perf/` - see the [performance testing guide](test/perf/README.md).


## Pull Requests

1. **Keep changes focused** - one logical change per PR
2. **Write clear commit messages** - explain what and why, not how
3. **Update documentation** if you change APIs or behavior
4. **Keep code clear and readable** so it is easy for other developers to understand
5. **Follow existing patterns** so the change is consistent with the codebase

### PR Checklist

Run all the pre-commit checks using `make`:
```bash
make check
```

Or check each item individually:
- [ ] Db migrations have been applied (`make migrate` or `goose up`)
- [ ] Generated code is up to date (`make generate`)
- [ ] Code is formatted (`make fmt` or `go fmt ./...`)
- [ ] No linting errors (`make vet` or `go vet ./...`)
- [ ] No security errors (`make lint` or `staticcheck ./...`)
- [ ] No security vulnerabilities (`make security` or `gosec ./...`)
- [ ] Tests pass (`make test` or `go test ./... && go test -v -tags=integration ./test/integration/`)

## Database Changes

- Add migrations to `app/sql/schema/`
- SQL queries in `app/sql/queries/`
- Run `make sqlc` (or `sqlc generate`) to update Go code
- Test migrations work both up and down

## API Changes

- Update Swagger annotations in handler code
- Run `make docs` (or `swag init -g cmd/signalsd/main.go`) to regenerate docs

## Release Process

Releases are automated when version tags are pushed. Use the build script:

```bash
./build.sh -t patch|minor|major
```

This automatically handles versioning, testing, and triggering deployment.
