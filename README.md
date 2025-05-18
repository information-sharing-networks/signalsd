# Information Sharing Networks
ISNs are networks that enable interested parties to share information. The information is shared by way of "signals".

# Signals

Signals are simple messages that can be exchanged between organisations to share data, indicate that an action has been taken or that something has been decided or agreed upon. Siganls are
- light-weight, with simple payloads and a straightforward version control system. 
- can be delivered as soon as a corresponding event occurs in the originating business process.
- can move in any direction, creating the potential for feedback loops.
  
# Reference Implementations
The [initial implementation](https://github.com/information-sharing-networks/isn-ref-impl) was a proof of concept and use to test the ideas as part of the UK govs Border Trade Demonstrator initiative (BTDs).  The BTDs established ISNs that were used by several goverment agencies and industry groups to make process improvements at the border by sharing supply chain information. 

The second version (work in progress) develops the ISN administration facilities and will scale to higher volumes of data.

There are three components
- an [API](https://nickabs.github.io/signalsd/) used to configure ISNs, register participants and deploy the data sharing infrastructure 
- an associated [framework agreement](https://github.com/information-sharing-networks/Framework) that establishes the responsibilities of the participants in an ISN
- a demonstration UI 

# Credits
Many thanks to Ross McDonald who came up with the concept and created the initial reference implemenation.

# go service dependencies
go 1.24 or above
PostgresSql@17 or above

# go development dependencies:
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
export SIGNALS_HOST=127.0.0.1
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
# drop all database objects
goose -dir sql/schema postgres $SIGNALS_DB_URL  down-to 0

# update the schema to the current version - run this after pulling code from the github repo
goose -dir sql/schema postgres $SIGNALS_DB_URL  up
```


# build and run
``` bash
cd app
go build ./cmd/signalsd/
./signalsd

# or
go run cmd/signalsd/main.go
```

# Api documentation
http://localhost:8080/docs

# Development
database alterations are made by adding files to sql/schema
001_foo.sql
002_bar.sql 
...

sql queries are kept in
`sql/queries`

run `sqlc generate` from the root of the project to regenerate the type safe go code after adding or altering any queries

# API docs
To generate the OpenApi docs:
```bash
swag init -g cmd/signalsd/main.go 
```

# tech overview
run the service and then see the [API docs](http://localhost:8080/docs)


![ERD](https://github.com/user-attachments/assets/07dad361-bbd7-4502-bc6c-6bb5ec575521)

