[Intro](#information-sharing-networks) |
[Try it out](#try-it-out) |
[Developers](#developers) |
[Technical Overview](#service-overview) 


# Information Sharing Networks
ISNs are networks that enable interested parties to share information. The information is shared in the form of "signals".

## Signals

Signals are simple messages that can be exchanged between organisations to share data, indicate that an action has been taken or that something has been decided or agreed upon. Siganls are
- light-weight, with simple payloads and a straightforward version control system. 
- can be delivered as soon as a corresponding event occurs in the originating business process.
- can move in any direction, creating the potential for feedback loops.
  
## Reference Implementations
The [initial implementation](https://github.com/information-sharing-networks/isn-ref-impl) was a proof of concept and use to test the ideas as part of the UK govs Border Trade Demonstrator initiative (BTDs).  The BTDs established ISNs that were used by several goverment agencies and industry groups to make process improvements at the border by sharing supply chain information. 

The second version (work in progress) develops the ISN administration facilities and will scale to higher volumes of data.

There are three components
- an [API](https://nickabs.github.io/signalsd/app/docs/index.html) used to configure ISNs, register participants and deploy the data sharing infrastructure 
- an associated [framework agreement](https://github.com/information-sharing-networks/Framework) that establishes the responsibilities of the participants in an ISN
- a demonstration UI 

## Credits
Many thanks to Ross McDonald who came up with the concept and created the initial reference implemenation.

# Try it out
You can run the service on your laptop without installing any additional software using Docker
Visit [Docker's website](https://docs.docker.com/get-docker) to download and install Docker for your operating system.
Follow the installation instructions specific to your OS.

1. [download](https://github.com/nickabs/signalsd/archive/refs/heads/main.zip) the source code (signals-main.zip)
2. unzip signals-main.zip

Then from the command line 
```
cd signals-main
docker compose up 
```
You can then use the service at [http://localhost:8080](http://localhost:8080)


To stop and remove the service, run this command from the same directory:
```
docker compose down --rmi local -v
```

# Developers
The http service is written in go and has the following development dependencies 
- [goose](https://github.com/pressly/goose) **database migrations**
- [sqlc](https://github/sqlc-dev/sqlc) **type safe code for SQL queries**
- [swaggo](https://github.com/swaggo/swag) **generates OpenApi specs from go comments**

The service uses a Postgresql@17 database. Instructions on installing the dependencies are below or, if you prefer, you can use the docker local dev environment which has all the dependencies pre-installed. 

## Docker local development environment
First, clone the repo. There is only one required env variable for the docker env:
```
export SIGNALS_SECRET_KEY="" # add a random secret key here (used to sign the JWT tokens used in the service)
```

Follow the instructions below to run the signalsd service.  This service handles 
- user registration
- ISN configuration
- running the receivers and retrievers that marshal the exchange of signal over the ISN

The API documentation is hosted as part of the service (alternatively you can see the documenation [here](https://nickabs.github.io/signalsd/app/docs/index.html))

```sh
cd signalsd
docker compose -f docker-compose.dev.yml up -d
Docker compose logs -f

# your local repo directory is mounted inside the container
# to test your changes, restart the app container:
docker compose restart app

#... this will regenerate  the sqlc code, rebuild the swagger API documents and recompile and run the signalsd service based on your latest changes.

# to stop the service and database 
docker compose -f docker-compose.dev.yml down

# to stop and remove all docker related images and storage:
docker compose -f docker-compose.dev.yml down --rmi local -v 
```
the service starts on [http://localhost:8080](http://localhost:8080)

To query the database, either connect to the docker app container and run the preinstalled psql client
```sh
docker exec -it -u signalsd signalsd-app-dev bash
psql postgres://signalsd-dev@localhost:15432/signalsd_admin?sslmode=disable
```

...or connect with a local postgres client
```sh
export SIGNALS_DB_URL=postgres://signalsd-dev:@localhost:15432/signalsd_admin?sslmode=disable
psql $SIGNALS_DB_URL
```

## Developer local installation
(Mac)

install
- go 1.24 or above
- PostgresSql@17 or above

go dev dependencies:
``` bash
go install github.com/pressly/goose/v3/cmd/goose@latest #database migrations 
go install github.com/sqlc-dev/sqlc/cmd/sqlc@latest #type safe code for SQL queries
go install github.com/swaggo/swag/cmd/swag@latest #generates OpenApi specs from go comments

```

set the following env variables
``` bash
# sample Signals service config
export SIGNALS_DB_URL="postgres://username:@localhost:5432/signalsd_admin?sslmode=disable" # on mac, username is your login username
export SIGNALS_ENVIRONMENT=dev
export SIGNALS_SECRET_KEY="" # add your random secret key here
export SIGNALS_PORT=8080
export SIGNALS_LOG_LEVEL=debug
export SIGNALS_HOST=127.0.0.1
```

the secret key is used to sign the JWT access tokens used by the service.  You can create a strong key using
``` bash
openssl rand -base64 64
```

**local postgres database setup (mac)**
``` bash
# 1 install and start postgresql server
brew install postgresql@17
brew services run postgresql@17 # use "brew servcies start" to register the service to start at login

# 2 connect to postgres server
psql postgres

# 3  and create the service database:  CREATE DATABASE signalsd_admin;

# 4 configure your connection 
export SIGNALS_DB_URL="postgres://user:@localhost:5432/signalsd_admin?sslmode=disable"
```

**database migrations**
the database schema is managed by [goose](https://github.com/pressly/goose)
```
# drop all database objects
goose -dir app/sql/schema postgres $SIGNALS_DB_URL  down-to 0

# update the schema to the current version - run this after pulling code from the github repo
goose -dir app/sql/schema postgres $SIGNALS_DB_URL  up
```


**build and run**
``` bash
cd app
go build ./cmd/signalsd/
./signalsd

# or
go run cmd/signalsd/main.go
```

## API docs
To generate the OpenApi docs:
```bash
swag init -g cmd/signalsd/main.go 
```
The docs are hosted as part of the signalsd service: [API docs](http://localhost:8080/docs)

## Database 
database schema alterations are made by adding files to sql/schema
001_foo.sql
002_bar.sql 
...
goose will run the changes in the order the files are sorted.

sql queries are kept in
`app/sql/queries`

run `sqlc generate` from the root of the project to regenerate the type safe go code after adding or altering any queries



# Service overview
technical overview
## Auth

![auth](https://github.com/user-attachments/assets/018cedf6-ba96-41ee-b3da-6d7cca08fb27)

## ISN config
![ISN Config](https://github.com/user-attachments/assets/b2491f77-f461-405e-8c21-2226695092b9)

## Signals
![signals](https://github.com/user-attachments/assets/ff387ce1-e023-4c48-9b2b-a59d8625efc4)



