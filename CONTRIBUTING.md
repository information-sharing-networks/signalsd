# Contributing to signalsd

## Approach

This project prefers code that is easy to understand, even if it means some repetition, and avoids abstractions that don't solve current problems.

## Before You Start

- Read the [README](README.md)
- Check existing [issues](https://github.com/information-sharing-networks/signalsd/issues) to see if your idea is already being discussed
- For significant changes, open an issue first to discuss the approach

## Development Setup

**Prerequisites**: [Docker Desktop](https://docs.docker.com/get-docker) and [Go](https://go.dev/doc/install). 

Start the service and database as described in the [README Quick Start](README.md#quick-start-docker):

```bash
git clone https://github.com/information-sharing-networks/signalsd.git
cd signalsd
make docker-up
```

By default, the service runs on http://localhost:8080 with API docs at `/docs`.

The docker compose file mounts the local repo directory into the app container, so you can edit code locally and see the changes in the container.

When the app container starts, it applies any pending database migrations and then runs [Air](https://github.com/air-verse/air), which watches for file changes, runs code generation (`swag`, `sqlc` and `templ`), and rebuilds and restarts the service. You can customise Air by editing `app/.air.toml`.

Rebuild the dev image when the Go version in `app/go.mod` or the inline Dockerfile in `docker-compose.yml` changes:

```bash
make docker-build
```

### Common tasks

With the containers running (`make docker-up`):

```bash
make docker-down     # Stop the database and app 
make docker-up       # Start the database and app
make test            # Run integration and unit tests (race detector disabled)
make check           # Run the pre-commit checks: codegen, formatting, linting, security checks and race-enabled tests
make db-migrate-up   # Apply pending database migrations
make db-migrate-redo # Roll back the latest migration and reapply it
make help            # See all available make targets
```

### Development Tools

The app uses the following go tools:
- goose - database migrations
- sqlc - type safe SQL queries
- swag - OpenAPI from go comments
- staticcheck - linter
- gosec - security analysis
- templ - ui templates
- govulncheck - vulnerability scanner
- Air - live reload

These tools are defined in a separate module, `app/tools/go.mod`, so that their dependency trees stay out of the app's `app/go.mod`. They are run from the `app` directory with `go tool -modfile=tools/go.mod <tool>` (the Makefile targets do this for you) and are compiled when the app container starts.

### Upgrading Go

The Go version is set by the `go` line in `app/go.mod` (the docker image and the CI builds use the same version). To upgrade:

1. Update the `go` line in `app/go.mod`
2. `make docker-build` - rebuild the image with the new toolchain
3. `make update-tools` - upgrade the go tools (older versions may not support the new Go release)
4. `make check` - confirm everything passes
5. Commit changes

Other developers will need to rebuild their image (`make docker-build`) and upgrade their local Go installation.

## Database Changes

Database schema migration is managed using [goose](https://github.com/pressly/goose). Schema changes are made by adding files to `app/sql/schema/`:
```
001_foo.sql
002_bar.sql
...
```
Pending migrations are applied when the app container starts, but not when Air rebuilds the app.  

To work on a new migration:

1. Add the migration file to `app/sql/schema/`, with `-- +goose Up` and `-- +goose Down` sections
2. `make db-migrate-up` - apply it to your dev database
3. `make db-migrate-redo` - after each edit, roll back the latest migration and reapply it (This runs your Down section)
4. `make test` - the integration tests create an empty database and apply all the migrations from scratch.

goose will not reapply a migration that has already run, so editing an applied migration has no effect until you redo it.

`make db-migrate-down` rolls back all migrations to version 0.

**Warning**: `make db-migrate-down` deletes all the data in your dev database.

SQL queries are kept in `app/sql/queries/`. Run `make sqlc` to regenerate the type safe Go code after adding or altering any queries (this runs automatically when Air rebuilds the app).

## API Changes

- Update Swagger annotations in handler code
- Run `make docs` to regenerate the OpenAPI docs (this runs automatically when Air rebuilds the app)

## User Interface

UI templates are built with [templ](https://templ.guide/). Run `make templ` to regenerate template code after editing `.templ` files (this runs automatically when Air rebuilds the app). For details on modifying, replacing or disabling the UI, see the [UI Documentation](app/internal/ui/README.md).

## Testing

Nearly all the testing is done via integration tests that live in `app/test/integration/`.

- `make test` runs the unit and integration tests
- `make test-race` runs the unit and integration tests with the race detector (slow)
- `make check` runs code generation, formatting, linting, security checks, and `make test-race`

For information about the testing strategy and how to run individual tests, see the [integration testing guide](app/test/integration/README.md).

## Production Image

The production image ([app/Dockerfile](app/Dockerfile)) is a statically compiled Go binary on the [distroless](https://github.com/GoogleContainerTools/distroless) `static-debian13:nonroot` base. The local Docker environment uses a different (Alpine-based) image, so a few things behave differently in production:

- **No shell or tools** - there is no `sh`, `curl` or package manager, so `docker exec -it <container> sh` will not work. To debug, temporarily swap the base image for the `:debug-nonroot` tag, which adds a busybox shell.
- **Only CA certificates and timezone data are included** - the CA certificates are required (signalsd fetches JSON schemas from GitHub over TLS).

### Base image updates

The base image is pinned by digest and Dependabot opens a PR when Google publishes a new one. These are usually a Debian point release or a `tzdata`/`ca-certificates` update.

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

CI runs the same checks (plus GitHub's CodeQL action) on every pull request against `main`.
