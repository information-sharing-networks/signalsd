#!/bin/sh

function launchLocal() {
    # entryfile for local dev docker app 
    goose -dir sql/schema postgres $DATABASE_URL up
    
    exec /app/signalsd --mode all
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


