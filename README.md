
# prerequisites
go 1.24 or above
PostgresSql@17 or above

go dependenicies:
 [goose](https://github.com/pressly/goose)
``` bash
go install github.com/pressly/goose/v3/cmd/goose@latest #database migrations 
```

# config
set the following env variables
``` bash
# sample Signals service config
export SIGNALS_DB_URL="postgres://nick:@localhost:5432/signals_admin?sslmode=disable"
export SIGNALS_ENVIRONMENT=dev
export SIGNALS_SECRET_KEY="CiymRYs6eAUEe1ktXhIdjO46e75Yvbjwx+sbYBvMOAITHJKJsMG2CMlM/xWO3ISn9FLsSi4w1lUpx2mv3I5HRQ=="
export SIGNALS_PORT=8080
export SIGNALS_LOG_LEVEL=debug
```

the secret key is used to sign the JWT access tokens used by the service.  You can create a strong key using
``` bash
openssl rand -base64 64
```
# database setup (mac)
``` bash
# 1 install and start postgresql server
brew install postgresql@17
brew services run postgresql@17 # use "brew servcies start" to register the service to start at login

# 2 connect to postgres server
psql postgres

# 3  and create the service database:  CREATE DATABASE signals_admin;

# 4 configure your connection 
export SIGNALS_DB_URL="postgres://user:@localhost:5432/signals_admin?sslmode=disable"
```

# database migrations
the database schema is managed by [goose](https://github.com/pressly/goose)
```
goose -dir sql/schema postgres $SIGNALS_DB_URL  down-to 0
goose -dir sql/schema postgres $SIGNALS_DB_URL  up
```


# build and run
``` bash
go build ./cmd/signalsd/
./signalsd

# or
go run cmd/signalsd/main.go
```
