#!/bin/sh

function launchLocal() {
    # entryfile for local dev docker app 
    ENV_FILE="/app/env/.env"
    mkdir -p $(dirname $ENV_FILE)

    if [ ! -f $ENV_FILE ]; then
        echo "export HOST=0.0.0.0" > $ENV_FILE
        echo "export SECRET_KEY=$(head /dev/urandom | tr -dc A-Za-z0-9 | head -c 64)" >> $ENV_FILE
        echo "export DATABASE_URL=postgres://signalsd@db:5432/signalsd_admin?sslmode=disable" >> $ENV_FILE
    fi

    . $ENV_FILE

    goose -dir sql/schema postgres $DATABASE_URL up
    
    exec /app/signalsd
}


function launchLocalDev() {

    echo creating signalsd user

    if ! getent group signalsd > /dev/null; then
        echo "creating signalsd group"
        addgroup -S signalsd  # Create a system group without a password
    fi

    if ! id -u signalsd > /dev/null 2>&1; then
        echo "creating signalsd user"
        adduser -S -G signalsd signalsd -h /home/signalsd -s /bin/bash signalsd 
    fi

    su - signalsd

    # use database in the docker container
    DATABASE_URL=postgres://signalsd-dev:@db:5432/signalsd_admin?sslmode=disable

    # configure http server inside docker to accept external requests
    HOST=0.0.0.0
    
    echo "export DATABASE_URL=$DATABASE_URL" > /home/signalsd/.bashrc

    echo "generate sqlc files"
    sqlc generate
    
    echo "creating swaggo documenation"
    swag init -g ./cmd/signalsd/main.go 

    echo "migrating database schema"
    goose -dir sql/schema postgres $DATABASE_URL up

    go run cmd/signalsd/main.go 
}

ENV=""

if [ -z "$DOCKER_ENV" ]; then
    echo "error: this script is only used inside docker" >&2
    exit 1
fi
while getopts "e:" arg; do
    if [ $arg = "e" ]; then
        export ENV=$OPTARG 
    fi
done

if [ "$ENV" != "local-dev" ] && [ "$ENV" != "local" ]; then
    echo "usage $0 -e environment (local, local-dev) " >&2
    exit 1
fi


if [ "$ENV" = "local" ]; then
    cd /app
    launchLocal
fi

if [ "$ENV" = "local-dev" ]; then
    cd /signalsd/app
    launchLocalDev
fi


