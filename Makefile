# Docker-based Makefile for signalsd
# Uses tools installed in Docker containers instead of local installations

.PHONY: help psql check generate docs swag-fmt sqlc fmt vet lint security test clean docker-up docker-down

# Docker compose service name
APP_SERVICE = app
DB_ACCOUNT = signalsd-dev 
DB_NAME = signalsd_admin

# Default target - show available commands
help:
	@echo "Docker-based Makefile - uses tools from Docker containers"
	@echo ""
	@echo "Prerequisites:"
	@echo "  docker compose up    # Start containers first"
	@echo ""
	@echo "Available commands:"
	@echo "  make check           - Run all pre-commit checks (recommended before committing)"
	@echo "  make generate        - Generate docs and code (swagger + sqlc)"
	@echo "  make docs            - Generate swagger documentation"
	@echo "  make swag-fmt        - format swag comments"
	@echo "  make sqlc            - Generate sqlc code"
	@echo "  make fmt             - Format code"
	@echo "  make vet             - Run go vet"
	@echo "  make lint            - Run staticcheck"
	@echo "  make security        - Run gosec security analysis"
	@echo "  make test            - Run tests"
	@echo "  make migrate         - Run database migrations (up)"
	@echo "  make docker-up       - Start Docker containers"
	@echo "  make docker-down     - Stop Docker containers"
	@echo "  make restart         - restart the docker app"
	@echo "  make logs       	  - follow docker logs"
	@echo "  make psql            - run psql agaist the dev database"
	@echo "  make clean           - Clean build artifacts"

# Docker management
docker-up:
	@echo "🐳 Starting Docker containers..."
	@docker compose up -d

docker-down:
	@echo "🐳 Stopping Docker containers..."
	@docker compose down

restart:
	@echo "🐳 restarting app"
	@docker compose restart app

logs:
	@echo "🐳 openning docker logs"
	@docker compose logs -f

# Main target: run all checks before committing
check: generate fmt swag-fmt vet lint security test
	@echo ""
	@echo "✅ All checks passed! Ready to commit."

# Generate all code and documentation
generate: docs sqlc swag-fmt

# Generate swagger documentation
docs:
	@echo "🔄 Generating swagger documentation..."
	@docker compose exec $(APP_SERVICE) sh -c "cd /signalsd/app && swag init -g ./cmd/signalsd/main.go"

# Generate swagger documentation
swag-fmt:
	@echo "🔄 Formatting swag comments..."
	@docker compose exec $(APP_SERVICE) sh -c "cd /signalsd/app && swag fmt"

# Generate type-safe SQL code
sqlc:
	@echo "🔄 Generating sqlc code..."
	@docker compose exec $(APP_SERVICE) sh -c "cd /signalsd/app && sqlc generate"

# Format code
fmt:
	@echo "🔄 Formatting code..."
	@docker compose exec $(APP_SERVICE) sh -c "cd /signalsd/app && go fmt ./..."

# Run go vet
vet:
	@echo "🔄 Running go vet..."
	@docker compose exec $(APP_SERVICE) sh -c "cd /signalsd/app && go vet ./..."

# Run staticcheck linter
lint:
	@echo "🔄 Running staticcheck..."
	@docker compose exec $(APP_SERVICE) sh -c "cd /signalsd/app && staticcheck ./..."

# Run security analysis
security:
	@echo "🔄 Running security analysis..."
	@docker compose exec $(APP_SERVICE) sh -c "cd /signalsd/app && gosec -exclude-generated ./..."

# Run tests
test:
	@echo "🔄 Running tests..."
	@sh -c "cd app && go test ./..."
	@sh -c "cd app && go test -v -count=1 -tags=integration ./test/integration/"

# Clean build artifacts
clean:
	@echo "🔄 Cleaning..."
	@sh -c "cd app && go clean -cache -testcache"
	@sh -c "cd app && rm ./signalsd"

# Database migrations (bonus commands using Docker)
migrate:
	@echo "🔄 Running database migrations..."
	@docker compose exec $(APP_SERVICE) bash -c 'cd /signalsd/app && goose -dir sql/schema postgres "$$DATABASE_URL" up'

migrate-down:
	@echo "🔄 Rolling back database migrations..."
	@docker compose exec $(APP_SERVICE) bash -c 'cd /signalsd/app && goose -dir sql/schema postgres "$$DATABASE_URL" down'

# Check if containers are running
check-containers:
	@echo "🔍 Checking if containers are running..."
	@docker compose ps $(APP_SERVICE) | grep -q "Up" || (echo "❌ Containers not running. Run 'make docker-up' first." && exit 1)
	@echo "✅ Containers are running"

# Run psql on the docker database
psql:
	@echo "🔄 Running psql on dev database container"
	docker compose exec -it db psql -U $(DB_ACCOUNT) -d $(DB_NAME)
