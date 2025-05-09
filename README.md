
# go service dependencies
go 1.24 or above
PostgresSql@17 or above

# go development dependenicies:
 [goose](https://github.com/pressly/goose)
 [sqlc](https://github/sqlc-dev/sqlc)
 [swaggo](https://github.com/swaggo/swag)
``` bash
go install github.com/pressly/goose/v3/cmd/goose@latest #database migrations 
go install github.com/sqlc-dev/sqlc/cmd/sqlc@latest #type safe code for SQL queries
go install github.com/swaggo/swag/cmd/swag@latest #generates OpenApi specs from go comments

```

# environment config
set the following env variables
``` bash
# sample Signals service config
export SIGNALS_DB_URL="postgres://username:@localhost:5432/signals_admin?sslmode=disable"
export SIGNALS_ENVIRONMENT=dev
export SIGNALS_SECRET_KEY="" # add your secret key here
export SIGNALS_PORT=8080
export SIGNALS_LOG_LEVEL=debug
```

the secret key is used to sign the JWT access tokens used by the service.  You can create a strong key using
``` bash
openssl rand -base64 64
```

# local postgres database setup (mac)
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

# Development
database alterations are made by adding files to sql/schema
001_foo.sql
002_bar.sql 
...

new sql queries are added in 
sql/queries

use `sqlc generate` from the root of the project to regenerate the type safe go code after adding or altering any queries

# API docs
generate OpenApi docs
```bash
swag init -g cmd/signalsd/main.go # from module root
```