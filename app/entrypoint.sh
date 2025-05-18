#!/bin/sh
set -eu

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
