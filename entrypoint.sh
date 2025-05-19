#!/bin/sh
set -eu

function launchLocal() {
    # entryfile for local dev docker app 
    ENV_FILE="/app/env/.env"
    mkdir -p $(dirname $ENV_FILE)

    if [ ! -f $ENV_FILE ]; then
        echo "export SIGNALS_HOST=0.0.0.0" > $ENV_FILE
        echo "export SIGNALS_SECRET_KEY=$(head /dev/urandom | tr -dc A-Za-z0-9 | head -c 64)" >> $ENV_FILE
        echo "export SIGNALS_DB_URL=postgres://signalsd@db:5432/signalsd_admin?sslmode=disable" >> $ENV_FILE
    fi

    . $ENV_FILE

    goose -dir sql/schema postgres $SIGNALS_DB_URL up
    exec /app/signalsd
}

function launchLocalDev() {
   
    echo "installing go dependencies..." 

    echo gooose
    go install github.com/pressly/goose/v3/cmd/goose@latest 

    echo sqlc
    go install github.com/sqlc-dev/sqlc/cmd/sqlc@latest 

    echo swag
    go install github.com/swaggo/swag/cmd/swag@latest 
    
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
    SIGNALS_DB_URL=postgres://signalsd-dev:@db:5432/signalsd_admin?sslmode=disable

    # configure http server inside docker to accept external requests
    SIGNALS_HOST=0.0.0.0
    
    echo "export SIGNALS_DB_URL=$SIGNALS_DB_URL" > /home/signalsd/.bashrc

    echo "generate sqlc files"
    sqlc generate
    
    echo "creating swaggo documenation"
    swag init -g ./cmd/signalsd/main.go 

    echo "migrating database schema"
    goose -dir sql/schema postgres $SIGNALS_DB_URL up

    go run cmd/signalsd/main.go 
}

ENV=""
while getopts "e:r" arg; do
  case $arg in
    e) export ENV=$OPTARG ;;
  esac
done

if [ "$ENV" != "local-dev" ] && [ "$ENV" != "local" ] ; then
    echo "usage $0 -e environment (local or local-dev) [ -r (restart local-dev signalsd)]" 
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
