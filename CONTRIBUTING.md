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
make docker-up
```

The service runs on http://localhost:8080 with API docs at `/docs`.

**Note**: Even when using Docker, you should have Go installed locally for code editing and running local tests.

## Production Image

The production image ([app/Dockerfile](app/Dockerfile)) is a statically compiled Go binary on the [distroless](https://github.com/GoogleContainerTools/distroless) `static-debian13:nonroot` base. The local Docker environment uses a different (Alpine-based) image, so a few things behave differently in production:

- **No shell or tools** - there is no `sh`, `curl` or package manager, so `docker exec -it <container> sh` will not work and health checks must not depend on shell commands. To debug, temporarily swap the base image for the `:debug-nonroot` tag, which adds a busybox shell.
- **Non-root user** - the service runs as `nonroot` (uid 65532). Any files or mounted volumes it needs to write must be writable by that user.
- **Only CA certificates and timezone data are included** - the CA certificates are required (signalsd fetches JSON schemas from GitHub over TLS).

### Base image updates

The base image is pinned by digest and Dependabot opens a PR when Google publishes a new one. These are usually a Debian point release or a `tzdata`/`ca-certificates` update. 

## Testing

Nearly all the testing is done via integration tests that live in `app/test/integration/`.  See the [integration testing guide](app/test/integration/README.md) for more information.

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

## Database Changes

- Add migrations to `app/sql/schema/`
- SQL queries in `app/sql/queries/`
- Run `make sqlc` (or `sqlc generate`) to update Go code
- Test migrations work both up and down

## API Changes

- Update Swagger annotations in handler code
- Run `make docs` 

