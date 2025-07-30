#!/bin/bash
# Integration test runner for signalsd
# Runs integration tests against a real PostgreSQL database in Docker

usage() {
    echo "usage: $0 [ -h -s -c ] [ -n regexp ]
    -h: show usage
    -s: skip database setup (this will delete the database content rather than dropping and recreating it)
    -c: clean up the test database after running the tests
    -n: run only tests matching the regexp
    "
    exit $1
}

cleanup() {
    docker compose -f ../../docker-compose.yml exec -it db psql -U $POSTGRES_USER -d postgres -c "DROP DATABASE IF EXISTS $DB_NAME;"
}

check_docker_is_running() {
    if ! docker compose -f ../../docker-compose.yml ps app db >/dev/null 2>&1; then
        return 1
    fi
    return 0
}

drop_and_recreate_db() {
    docker compose -f ../../docker-compose.yml exec -it db psql -U $POSTGRES_USER -d postgres -c "DROP DATABASE IF EXISTS $DB_NAME;"  && \
    docker compose -f ../../docker-compose.yml exec -it db psql -U $POSTGRES_USER -d postgres -c "CREATE DATABASE $DB_NAME;"
}

delete_data() {
    docker compose -f ../../docker-compose.yml exec -it db psql -U $POSTGRES_USER -d $DB_NAME -c "DELETE FROM accounts CASCADE;"
}

run_migration() {
    docker compose -f ../../docker-compose.yml exec app bash -c "cd /signalsd/app && goose -dir sql/schema postgres postgres://${POSTGRES_USER}:@db:5432/${DB_NAME}?sslmode=disable up"
}

# main
if [[ ! -z "$ENVIRONMENT" && "$ENVIRONMENT" != "dev" ]]; then
    echo "error: this script should not be run on $ENVIRONMENT environments" >&2
    exit 1
fi

POSTGRES_USER="signalsd-dev"
POSTGRES_PASSWORD=""
DB_NAME="signalsd_integration_test"
HOST=localhost
PORT=15432
SKIP_DB_SETUP=""
CLEANUP=""
FUNCTION_NAME=Integration

while getopts "hscn:" opt; do
    case $opt in
        h) usage 0 ;;
        s) SKIP_DB_SETUP=true ;;
        c) CLEANUP=true ;;
        n) FUNCTION_NAME="$OPTARG" ;;
        \?) usage 1 ;;
    esac
done

if ! check_docker_is_running; then
    echo "error: docker app is not running. Please start the docker app and try again." >&2
    exit 1
fi

export TEST_DATABASE_URL="postgres://${POSTGRES_USER}:${POSTGRES_PASSWORD}@${HOST}:${PORT}/${DB_NAME}?sslmode=disable"

echo "🚀 Running signalsd integration tests on $TEST_DATABASE_URL"

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
# Run the integration tests from the app directory where go.mod is located
cd ../../app && go test -v ./test/integration -run "$FUNCTION_NAME" -timeout 30s && cd -
err=$?

if [ "$CLEANUP" ];then
    echo "⚙️ Cleaning up test database..."
    cleanup
fi

if [ $err -ne 0 ]; then
    echo "❌ Integration tests failed"
    exit 1
else
    echo "✅ Integration tests completed"
fi
