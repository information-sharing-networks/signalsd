# Docker-based Makefile for signalsd
# Uses tools installed in Docker containers instead of local installations

.PHONY: help psql check generate docs swag-fmt sqlc fmt vet lint security vuln test clean docker-up docker-down templ go-api go-ui db-migrate db-migrate-down check-containers go-all

export GO_VERSION := $(shell grep '^go ' app/go.mod | awk '{print $$2}')

# Docker compose service name
APP_SERVICE = app
DB_ACCOUNT = signalsd-dev 
DB_NAME = signalsd_admin

# Default target - show available commands
help:
	@echo 'Usage: make [target]'
	@echo ''
	@echo 'Go version: $(GO_VERSION) (from app/go.mod)'
	@echo ''
	@echo "Available targets:"
	@echo "  make docker-up       - Start Docker containers"
	@echo "  make docker-down     - Stop Docker containers"
	@echo "  make docker-build    - Build the app container"
	@echo "  make docker-up-db    - Start the database container (detached mode)"
	@echo "  make docker-down-db  - Stop the database container"
	@echo "  make docker-up-app   - Start the app container (detached mode)"
	@echo "  make docker-down-app - Stop the app container"
	@echo "  make go-all          - Start signalsd backend and integrated ui locally (expects docker db to be running)"
	@echo "  make go-api          - Start signalsd backend locally (expects docker db to be running)"
	@echo "  make go-ui           - Start ui in standalone mode (expects signalsd to be running on 8080)"
	@echo "  make test            - Run tests"
	@echo "  make generate        - Generate docs and code (swagger + sqlc + templ)"
	@echo "  make docs            - Generate swagger documentation"
	@echo "  make swag-fmt        - format swag comments"
	@echo "  make sqlc            - Generate sqlc code"
	@echo "  make templ           - Generate templ template code (UI)"
	@echo "  make check           - Run all pre-commit checks (recommended before committing)"
	@echo "  make fmt             - Format code"
	@echo "  make vet             - Run go vet"
	@echo "  make lint            - Run staticcheck"
	@echo "  make security        - Run gosec security analysis"
	@echo "  make db-migrate      - Run database migrations (up)"
	@echo "  make restart         - restart the docker app"
	@echo "  make logs            - follow docker logs"
	@echo "  make psql            - run psql agaist the dev database"
	@echo "  make clean           - Clean build artifacts"

# Docker management
docker-up:
	@echo "🐳 Starting Docker containers..."
	@echo "Using Go version: $(GO_VERSION)"
	@GO_VERSION=$(GO_VERSION) docker compose up

docker-down:
	@echo "🐳 Stopping Docker containers..."
	@docker compose down

docker-up-db:
	@echo "🐳 Starting database container (detached mode)..."
	@docker compose up db -d

docker-up-app:
	@echo "🐳 Starting app container (detached mode)..."
	@docker compose up app -d

docker-down-db:
	@echo "🐳 Stopping database container..."
	@docker compose down db

docker-down-app:
	@echo "🐳 Stopping app container..."
	@docker compose down app

docker-build:
	@echo "🐳 Building app container..."
	@echo "Using Go version: $(GO_VERSION)"
	@GO_VERSION=$(GO_VERSION) docker compose build app

docker-restart-app:
	@echo "🐳 Restart app container..."
	@docker compose restart app

logs:
	@echo "🐳 Following docker logs..."
	@docker compose logs -f app

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

# Generate templ templates
templ:
	@echo "🔄 Generating templ templates..."
	@docker compose exec $(APP_SERVICE) sh -c "cd /signalsd/app && templ generate"

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


# Run vulnerability scan
vuln:
	@echo "🔍 Running vulnerability scan..."
	@docker compose exec $(APP_SERVICE) sh -c "cd /signalsd/app && govulncheck ./..."


# Clean build artifacts
clean:
	@echo "🔄 Cleaning..."
	@sh -c "cd app && go clean -cache -testcache"
	@sh -c "cd app && rm ./signalsd"

# Database migrations (bonus commands using Docker)
db-migrate:
	@echo "🔄 Running database migrations..."
	@docker compose exec $(APP_SERVICE) bash -c 'cd /signalsd/app && goose -dir sql/schema postgres "$$DATABASE_URL" up'

db-migrate-down:
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

# Run api locally using docker db
go-api:
	@echo "🔄 Running local api + docker db"
	cd app && DATABASE_URL="postgres://signalsd-dev@localhost:15432/signalsd_admin?sslmode=disable" SECRET_KEY="secretkey" go run cmd/signalsd/main.go run api

# Run ui in standalone mode
go-ui:
	@echo "🔄 Running standalone ui"
	cd app && API_BASE_URL=http://localhost:8080 go run cmd/signalsd-ui/main.go

# Run api and integrated ui locally using docker db
go-all:
	@echo "🔄 Running local api + docker db"
	cd app && DATABASE_URL="postgres://signalsd-dev@localhost:15432/signalsd_admin?sslmode=disable" SECRET_KEY="secretkey" go run cmd/signalsd/main.go run all

# Generate all code and documentation
generate: docs sqlc swag-fmt templ

# Main target: run all checks before committing
check: generate fmt swag-fmt vet lint security vuln test
	@echo ""
	@echo "✅ All checks passed! Ready to commit."

# Run tests
test:
	@echo "🔄 Running tests..."
	@sh -c "cd app && go test ./..."
	@sh -c "cd app && go test -v -count=1 -tags=integration ./test/integration/"