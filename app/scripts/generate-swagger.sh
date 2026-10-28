#!/bin/sh

# Generates the three swagger specs from the swag comments.
# Used by make docs, the docker compose app service and air, so the tag lists only need changing here.
#
# - docs/swagger.json (/docs): the client API - the tags clients use to exchange signals
# - docs/admin/swagger.json (/admin-docs): every other tag except the UI ones. A new tag goes here unless it is added to CLIENT_API_TAGS (and excluded from ADMIN_API_TAGS)
# - docs/ui/swagger.json (/ui-docs): the UI pages and HTMX actions
#
# The auth tags (OAuth 2.0 and User Authentication) are in both the client and admin specs.

set -e

# run from the app directory, wherever the script is called from
cd "$(dirname "$0")/.."

CLIENT_API_TAGS='OAuth 2.0,User Authentication,Service Accounts,ISN Discovery,Submit Signals,Search Signals,Batches'
ADMIN_API_TAGS='!HTMX Actions,!UI Pages,!Service Accounts,!ISN Discovery,!Submit Signals,!Search Signals,!Batches'
UI_TAGS='HTMX Actions,UI Pages'

go tool swag init -g ./cmd/signalsd/main.go --tags "$CLIENT_API_TAGS" --outputTypes json,yaml
go tool swag init -g ./cmd/signalsd/main.go --tags "$ADMIN_API_TAGS" -o ./docs/admin --outputTypes json,yaml
go tool swag init -g ./cmd/signalsd/main.go --tags "$UI_TAGS" -o ./docs/ui --outputTypes json,yaml
