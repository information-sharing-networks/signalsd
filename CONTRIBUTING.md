# Contributing to signalsd

## Approach

This project values code that is easy to understand, even if it means some repetition, and avoids abstractions that don't solve current problems.

## Before You Start

- Read the [README](README.md) to understand the project
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

## Code Style

- **Go formatting**: Use `go fmt` (enforced by CI)
- **Simple solutions**: Prefer straightforward code over complex abstractions
- **Explicit over implicit**: Make dependencies and behavior clear

## Testing

Write tests for new functionality:

```bash
cd app
go test ./...
```

Performance tests are in `test/perf/` - see the [performance testing guide](test/perf/README.md).

## Pull Requests

1. **Keep changes focused** - one logical change per PR
2. **Write clear commit messages** - explain what and why, not how
3. **Update documentation** if you change APIs or behavior
4. **Keep code clear and readable** so it is easy for other developers to understand
5. **Follow existing patterns** so the change is consistent with the codebase

### PR Checklist

- [ ] Tests pass (`go test ./...`)
- [ ] Code is formatted (`go fmt ./...`)
- [ ] No linting errors (`go vet ./...`)
- [ ] no security errors (`staticcheck ./...`)
- [ ] Documentation updated if needed

## Database Changes

- Add migrations to `app/sql/schema/`
- SQL queries in `app/sql/queries/`
- Run `sqlc generate` to update Go code
- Test migrations work both up and down

## API Changes

- Update Swagger annotations in handler code
- Run `swag init -g cmd/signalsd/main.go` to regenerate docs

## Getting Help

- Check the [API documentation](https://information-sharing-networks.github.io/signalsd/app/docs/index.html)
- Review existing code for patterns
- Open an issue for questions or discussion

## Release Process

Releases are automated when version tags are pushed. Use the build script:

```bash
./build.sh -t patch|minor|major
```

This handles versioning, testing, and triggering deployment.
