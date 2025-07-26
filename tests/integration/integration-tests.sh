#!/bin/bash
# HTTP integration test runner for signalsd

usage() {
    echo "usage: $0 [ -h -s -d -v ] [ -p port ]  -r regexp|all

    -r: run tests matching the regexp (use <all> to run all tests)

    options:
    -h: show help
    -s: skip database setup (this will delete the database content rather than dropping and recreating it)
    -d: drop the test database after running the tests
    -p: port number to use in the base URL for HTTP tests (default:8080 - http://localhost:8080)
    -v: verbose output from go test
    "
    exit $1
}

drop_db() {
    docker compose -f $DOCKER_COMPOSE_FILE exec -it db psql -U $POSTGRES_USER -d postgres -c "DROP DATABASE IF EXISTS $DB_NAME;"  
}

check_docker_is_running() {
    if ! docker compose -f $DOCKER_COMPOSE_FILE ps app db >/dev/null 2>&1; then
        return 1
    fi
    return 0
}

check_signalsd_is_running() {
    if ! curl -s -f "${BASE_URL}/health/live" >/dev/null 2>&1; then
        return 1
    fi
    return 0
}

drop_and_recreate_db() {
    docker compose -f $DOCKER_COMPOSE_FILE exec -it db psql -U $POSTGRES_USER -d postgres -c "DROP DATABASE IF EXISTS $DB_NAME;"  && \
    docker compose -f $DOCKER_COMPOSE_FILE exec -it db psql -U $POSTGRES_USER -d postgres -c "CREATE DATABASE $DB_NAME;"
}

delete_data() {
    docker compose -f $DOCKER_COMPOSE_FILE exec -it db psql -U $POSTGRES_USER -d $DB_NAME -c "DELETE FROM accounts CASCADE;"
}

run_migration() {
    docker compose -f $DOCKER_COMPOSE_FILE exec app bash -c "cd /signalsd/app && goose -dir sql/schema postgres postgres://${POSTGRES_USER}:@db:5432/${DB_NAME}?sslmode=disable up"
}

# main

# command line arguments
SKIP_DB_SETUP=""
DROP_TEST_DB=""
FUNCTION_NAME=""
PORT=""
VERBOSE_FLAG=""

while getopts "hsdvr:p:" opt; do
    case $opt in
        h)
            usage 0
            ;;
        s)
            SKIP_DB_SETUP=1
            ;;
        d)
            DROP_TEST_DB=1
            ;;
        r)
            if [ "$OPTARG" == "all" ]; then
                FUNCTION_NAME=".*"
            else
                FUNCTION_NAME="$OPTARG"
            fi
            ;;
        p)
            PORT="$OPTARG"
            ;;
        v)
            VERBOSE_FLAG="-v"
            ;;
        \?)
            echo "Invalid option: -$OPTARG" >&2
            usage 1
            ;;
    esac
done

DOCKER_COMPOSE_FILE="$(pwd)/../../docker-compose.yml"
if [ ! -f "$DOCKER_COMPOSE_FILE" ]; then
    echo "error: docker-compose.yml not found at $DOCKER_COMPOSE_FILE. This script should be run from tests/integration" >&2
    exit 1
fi

if [ -z "$FUNCTION_NAME" ]; then
    echo "error: no tests specified - use the -r option to specify the tests to run" >&2
    usage 1
fi

# Configuration
POSTGRES_USER="signalsd-dev"
POSTGRES_PASSWORD=""
HOST="localhost"
POSTGRES_PORT="15432"
DB_NAME="signalsd_integration_test"
if [ "$PORT" ]; then
    BASE_URL="http://${HOST}:${PORT}"
else
    BASE_URL="http://localhost:8080"
fi

echo "🚀 Running signalsd integration tests"
# todo echo "📍 Target URL: $BASE_URL"

if ! check_docker_is_running; then
    echo "❌ Docker containers are not running. Please start them with:"
    echo "   docker compose -f $DOCKER_COMPOSE_FILE up -d"
    exit 1
fi

# Check if signalsd is responding
if ! check_signalsd_is_running "$BASE_URL"; then
    echo "❌ signalsd is not responding at $BASE_URL"
    echo "   Make sure the signalsd container is running"
    exit 1
fi

export TEST_DATABASE_URL="postgres://${POSTGRES_USER}:${POSTGRES_PASSWORD}@${HOST}:${POSTGRES_PORT}/${DB_NAME}?sslmode=disable"
export TEST_HTTP_BASE_URL="$BASE_URL"
export TEST_SECRET_KEY="${TEST_SECRET_KEY:-dev-container-secret-key-12345}"

echo "Database: $TEST_DATABASE_URL"

echo
if [ "$SKIP_DB_SETUP" ]; then
    echo "⚙️ deleting data on $DB_NAME database"
    delete_data
    if [ $? -ne 0 ]; then
        echo "❌ Failed to delete data"
        exit 1
    fi
else
    echo "⚙️ dropping and recreating database: $DB_NAME"
    drop_and_recreate_db
    if [ $? -ne 0 ]; then
        echo "❌ Failed to (re)create database $DB_NAME"
        exit 1
    fi
    echo
    echo "⚙️ running migrations on $DB_NAME database"
    run_migration
    if [ $? -ne 0 ]; then
        echo "❌ Failed to apply migrations"
        exit 1
    fi
fi

echo
echo "⚙️️ Running integration tests..."
echo
cd ../../app && go test $VERBOSE_FLAG ./test/integration -run "$FUNCTION_NAME" -timeout 60s
test_result=$?


if [ "$DROP_TEST_DB" ];then
    echo
    echo "⚙️ dropping the test $DB_NAME database..."
    drop_db
fi

echo
if [ $test_result -ne 0 ]; then
    echo "❌ integration tests failed"
    exit 1
else
    echo "✅ integration tests completed successfully!"
fi
