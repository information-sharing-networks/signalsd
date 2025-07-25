[Intro](#information-sharing-networks) |
[Developer Guide](#developer-guide) |
[Technical Overview](#technical-overview)

![ci](https://github.com/information-sharing-networks/signalsd/actions/workflows/ci.yml/badge.svg)
 ![cd-staging](https://github.com/information-sharing-networks/signalsd/actions/workflows/cd-staging.yml/badge.svg)
 ![cd-production](https://github.com/information-sharing-networks/signalsd/actions/workflows/cd-production.yml/badge.svg)

# Information Sharing Networks

Information Sharing Networks (ISNs) let organisations create new data sharing arrangements without building complex custom technology solutions each time. 

Information Sharing Networks (ISNs) enable organisations to establish data sharing arrangements efficiently, without the need to develop complex, customised technology solutions each time. 

Participants exchange information through 'signals' - simple messages that facilitate information transfer between network members.

## Signals
Signals notify authorised organisations when key events occur within your business processes. For example, when an order ships, a decision is made, or a review is completed, the corresponding signal is sent immediately to authorised participants.

Each signal contains minimal data and follows straightforward formatting rules. These signals can be linked to form a timeline of related events, and the system handles version control so you can add more detail to previously issued signals when new information emerges.

Organisations can use signals to share:
- Events: Order confirmations, delivery notifications, status updates
- Decisions: Approvals, rejections, policy amendments
- Analysis: Audit results, risk assessments, compliance findings
- Verification: Confirmation of the accuracy of data from other organisations


## Setting Up Networks
This service provides the ISN management facilities. It is easy to deploy the service to public cloud infrastructure and is designed to be operated with minimal technical support.

The service makes it easy to establish new networks and to control the data that can be shared and who can acceess it.

Many data sharing initiatives encounter difficulties because they attempt to anticipate every possible need across multiple organisations, requiring extensive planning, complex agreements, and data models designed to cover scenarios that may never materialise.

ISNs work differently: a network is set up for a specific business purpose - such as tracking shipments, sharing compliance data, or coordinating approvals - and only the necessary signals for that purpose are defined. As new requirements emerge, additional signal types can be introduced without disrupting existing processes.

This approach enables rapid implementation of effective data sharing, while maintaining flexibility as business relationships evolve.

## Reference Implementations
The [initial implementation](https://github.com/information-sharing-networks/isn-ref-impl) was a proof of concept used as part of the UK government's Border Trade Demonstrator (BTD) initiative. The BTDs established ISNs that were used by several government agencies and industry groups to improve processes at the border by sharing supply chain information.

This repo contains the second version (work in progress) - it develops the ISN administration facilities and is designed to scale to higher volumes of data.

There are three components:
- an [API](https://signalsd.btddemo.org/docs) used to configure ISNs, register participants and deploy the data sharing infrastructure
- an associated [framework agreement](https://github.com/information-sharing-networks/Framework) that establishes the responsibilities of the participants in an ISN
- a demonstration UI

## Credits
Many thanks to [Ross McDonald](https://github.com/rossajmcd) who came up with the concept and created the initial reference implementation.

# Developer Guide

## Environment Variables
The service has sensible defaults for all configuration values. You only need to set environment variables to override the defaults.

```bash
# Required (for production and local dev environments)
DATABASE_URL=postgres://user:password@host:port/database?sslmode=disable # note production urls must use ssl.
SECRET_KEY=your-random-secret-key-here  # Generate with: openssl rand -base64 64

# Server Configuration (all optional - defaults shown)
HOST=0.0.0.0                    # Bind address (default: 0.0.0.0)
PORT=8080                       # Server port (default: 8080)
ENVIRONMENT=dev                 # Options: dev, prod, test, perf, staging (default: dev)
LOG_LEVEL=debug                 # Options: debug, info, warn, error (default: debug)

# Performance Tuning (all optional - defaults shown)
READ_TIMEOUT=15s                # HTTP read timeout (default: 15s)
WRITE_TIMEOUT=15s               # HTTP write timeout (default: 15s)
IDLE_TIMEOUT=60s                # HTTP idle timeout (default: 60s)
RATE_LIMIT_RPS=100              # Requests per second (default: 100, set to 0 to disable)
RATE_LIMIT_BURST=20             # Burst allowance (default: 20)
MAX_SIGNAL_PAYLOAD_SIZE=5242880 # Max payload size (default: 5MB)
MAX_API_REQUEST_SIZE=65536      # Max API request size (default: 64KB)

# Security - list sites that are allowed to use the service
ALLOWED_ORIGINS=*               # CORS origins (default: *, comma-separated for multiple)

# Database Connection Pool (the default used are the same as those used by pgx )
DB_MAX_CONNECTIONS=4
DB_MIN_CONNECTIONS=0
DB_MAX_CONN_LIFETIME=60m
DB_MAX_CONN_IDLE_TIME=30m
DB_CONNECT_TIMEOUT=5s
```

**Note**: In the Docker development environment, DATABASE_URL and SECRET_KEY are automatically configured with development-appropriate defaults.

## Environment-Specific Configuration Examples

### Performance Testing Configuration
For load testing and performance evaluation:
```bash
# Performance testing environment
ENVIRONMENT=perf
DB_MAX_CONNECTIONS=50
DB_MIN_CONNECTIONS=5
DB_MAX_CONN_LIFETIME=30m
DB_MAX_CONN_IDLE_TIME=15m
RATE_LIMIT_RPS=0                # Disable rate limiting for testing
MAX_SIGNAL_PAYLOAD_SIZE=10485760 # 10MB for larger test payloads

go run cmd/signalsd/main.go --mode all
```

### Production Configuration
These are the settings used for the Neon.tech production deployment:
```bash
DB_MAX_CONNECTIONS=25           
DB_MIN_CONNECTIONS=0            # Allow scaling to zero (Cloud Run)
DB_MAX_CONN_LIFETIME=120m  
DB_MAX_CONN_IDLE_TIME=20m
DB_CONNECT_TIMEOUT=10s

go run cmd/signalsd/main.go --mode all
```


## Quick Start (Docker Development Environment)

**Prerequisites**: [Docker Desktop](https://docs.docker.com/get-docker) installed on your system.

Clone the repo:
```bash
git clone https://github.com/information-sharing-networks/signalsd.git
cd signalsd
```

Using the development environment:
```bash
# Start the service
docker compose up

# Stop the service
docker compose down

# Restart the app container to compile and run the latest code
# Note: This will rerun sqlc, swag, goose, etc
docker compose restart app

# For environment variable changes, use up instead of restart
# This recreates the container with the new environment variables

# start the http server on a different port:
PORT=8081 docker compose up app

# Performance testing configuration
ENVIRONMENT=perf DB_MAX_CONNECTIONS=50 DB_MIN_CONNECTIONS=5 RATE_LIMIT_RPS=0 docker compose up app

# Production-like configuration
ENVIRONMENT=prod DB_MAX_CONNECTIONS=25 DB_CONNECT_TIMEOUT=10s RATE_LIMIT_RPS=200 docker compose up app

# Rebuild the image when you change:
# - dockerfile_inline content
# - go.mod/go.sum (new dependencies)
# - Go tool versions (goose, sqlc, swag)
docker compose up --build app

# start a shell in the app container
docker exec -it signalsd-app /bin/bash

# Run individual tools inside the container:

# Generate API docs
docker compose exec app sh -c "cd /signalsd/app && swag init -g ./cmd/signalsd/main.go"

# Run database migrations
docker compose exec app bash -c 'cd /signalsd/app && goose -dir sql/schema postgres "$DATABASE_URL" up'

# Generate type-safe SQL code
docker compose exec app sh -c "cd /signalsd/app && sqlc generate"

# Connect to the postgres database
docker exec -it signalsd-db psql -U signalsd-dev -d signalsd_admin

# run the go app locally and use the docker postgres database 
DATABASE_URL="postgres://signalsd-dev@localhost:15432/signalsd_admin?sslmode=disable" SECRET_KEY="mysecretkey" go run cmd/signalsd/main.go --mode all

# Stop and remove the environment completely
docker compose down --rmi local -v
```

The service starts on [http://localhost:8080](http://localhost:8080) by default.

The API documentation is hosted as part of the service or you can refer to the [latest released docs](https://signals.btddemo.org/docs)

## Troubleshooting Docker Development Environment

### Tool Version Issues
If you encounter errors or missing features in development tools (goose, sqlc, swag), this is likely due to Docker layer caching using older tool versions.

**Solution**: Force rebuild the Docker image to get the latest tool versions:
```bash
docker compose up --build
```


## Local Development Setup
### Prerequisites (macOS)
Install the following:
- Go 1.24 or above
- PostgreSQL@17 or above

### Go Development tools
the following dependencies are used when devloping the service:
```bash
go install github.com/pressly/goose/v3/cmd/goose@latest    # database migrations
go install github.com/sqlc-dev/sqlc/cmd/sqlc@latest       # type safe code for SQL queries
go install github.com/swaggo/swag/cmd/swag@latest         # generates OpenAPI specs from go comments
```

### Quality Assurance Tools
```bash
go install honnef.co/go/tools/cmd/staticcheck@latest  # static analysis
go install github.com/securecode/gosec/v2/cmd/gosec@latest  # security analysis
```

### Environment Variables
```bash
# local dev service config
DATABASE_URL="postgres://username:@localhost:5432/signalsd_admin?sslmode=disable"  # On macOS, username is your login username
SECRET_KEY=your_random_secret_key_here  # Generate with: openssl rand -base64 64
HOST=127.0.0.1
```

The secret key is used to sign the JWT access tokens used by the service.

### PostgreSQL Database Setup (macOS)
```bash
# 1. Install and start PostgreSQL server
brew install postgresql@17
brew services start postgresql@17  # Use "start" to register the service to start at login

# 2. Connect to PostgreSQL server
psql postgres

# 3. Create the service database
CREATE DATABASE signalsd_admin;

# 4. Configure your connection
export DATABASE_URL="postgres://$(whoami):@localhost:5432/signalsd_admin?sslmode=disable"
```

### Database Management
database schema migration is managed by [goose](https://github.com/pressly/goose):

Schema changes are made by adding files to `app/sql/schema`:
```
001_foo.sql
002_bar.sql
...
```
Goose usage:
```sh
# Update the schema to the current version (this command applies any new migrations).  If you are developing locally, run this after pulling code from the GitHub repo (for docker users the migration is applied automatically whenever you restart the app container)
goose -dir app/sql/schema postgres $DATABASE_URL up

# to reset the database to the initial state, dropping all database objects with
goose -dir app/sql/schema postgres $DATABASE_URL down-to 0
```

### Build and Run locally
```bash
cd app
go build ./cmd/signalsd/
./signalsd -mode all

# Or run directly
go run cmd/signalsd/main.go -mode all

# Configure the service environment
PORT=8081 go run cmd/signalsd/main.go -mode all

# Performance testing with custom database pool settings
ENVIRONMENT=perf DB_MAX_CONNECTIONS=50 DB_MIN_CONNECTIONS=5 RATE_LIMIT_RPS=0 go run cmd/signalsd/main.go -mode all

# Production-like settings
ENVIRONMENT=prod DB_MAX_CONNECTIONS=25 DB_CONNECT_TIMEOUT=10s go run cmd/signalsd/main.go -mode all
```

## API Documentation
To generate the OpenAPI docs:
```bash
swag init -g cmd/signalsd/main.go
```
The docs are hosted as part of the signalsd service: [API docs](http://localhost:8080/docs)


## SQL Queries
SQL queries are kept in `app/sql/queries`.

Run `sqlc generate` from the root of the project to regenerate the type safe Go code after adding or altering any queries.


## Getting Help
- Check the [API documentation](https://information-sharing-networks.github.io/signalsd/app/docs/index.html)
- Review logs: `docker compose logs -f`
- Open an [issue](https://github.com/information-sharing-networks/signalsd/issues) on GitHub


# Technical overview
## Auth
![auth.0.4.0](https://github.com/user-attachments/assets/643ec71a-f037-4a7e-9497-6023d9100e69)

## ISN config
![ISN config v0 5 0](https://github.com/user-attachments/assets/2be326f2-f4d0-485e-aeed-28076383cd8e)


## Signals
![signals](https://github.com/user-attachments/assets/49efaa13-d25a-4ce6-8829-990bd8038716)

## Rate Limits
The service includes a shared rate limiter for all traffic regardless of source IP or user identity and protects all endpoints including auth, API, and admin routes.

This just provides basic protection against abuse - in a production environment you should configure your CDN/load balancer/reverse proxy with per-IP rate limiting.

## CI/CD overview
Github actions are used to automate checks and deployments.

CI checks are run whenever there is a push to main.

The service is deployed to staging whenever there is a push to main and depolyed to production whenever a new version tag (e.g v1.0.0) is pushed. See below for details on using the build script to trigger a new production release.

![CI:CD (v0 11)](https://github.com/user-attachments/assets/6e39cd4b-1fc5-441f-a875-e51c814525ad)

See GitHub Actions workflows in `.github/workflows/`

### Creating a Release
```bash
# 1. Test and prepare
git checkout main && git pull origin main
cd app && go test ./... && cd ..

# 2. Create and push version tag; build locally with version info
build.sh -t patch|minor|major
```

## Cloud Deployment

This service is deployed to Google Cloud Run.  Google handles HTTPS, firewall, load balancing and autoscaling. The service will scale to zero when not in use.

**Note: This is pre-production software and the cloud deployment should only be used with data that you don't mind being deleted or seen by other people.**

## Service Mode Configuration

You can run multiple instances of the signalsd service, each in a different mode.  This enables you to, for example, run a separate service for admin and signal processing workloads.
The service mode is specified using the `-mode` command line flag:

- **`all`**: Serves all endpoints 
- **`admin`**: Serves only admin API endpoints (excludes signal exchange)
- **`signals`**: Serves signal exchange endpoints (both read and write operations)
- **`signals-read`**: Serves only signal read operations 
- **`signals-write`**: Serves only signal write operations

```sh
PORT=8080 go run cmd/signalsd/main.go --mode admin
PORT=8081 go run cmd/signalsd/main.go --mode signals-read
PORT=8082 go run cmd/signalsd/main.go --mode signals-write
```

## Deployment Configurations
### Basic config
The simplest configuration is to run containers that serve all endpoints.  This is the configuration used by the github actions CD pipeline but - although fine for testing - it is not recommended for production use.

![deploy.0.2.0](https://github.com/user-attachments/assets/942384a7-ccd7-4abb-b2a7-a9e293e23a10)


### Separate Admin vs Signals containers config
It is a good idea to separate the admin api container from the signals exchange containers: although they still share a common database, it will ensure that admin requests are not blocked when there are a large number of concurrent signal processing requests.

See .github/workflow/cd-multi.yml for the github actions pipeline that can be used to deploy this configuration.

To use this configuration, set up Google Cloud Load Balancer with path-based routing to direct traffic to the signals and admin instances:

```yaml
- match:
    path:
      regex: "^/api/isn/.*/signal_types/.*/signals.*"
  route:
    cluster: signalsd-signals

- match:
    prefix: "/"
  route:
    cluster: signalsd-admin
```

### Advanced Config
A more advanced configuration is to separate read and write operations. This would be useful if using a read-only database replica for readers:

```yaml
# load balance config:
# Write operations
- match:
    path:
      regex: "^/api/isn/.*/signal_types/.*/signals$"
    method: "POST"
  route:
    cluster: signalsd-signals-write

# Read operations
- match:
    path:
      regex: "^/isn/.*/signal_types/.*/signals/.*"
    method: "GET"
  route:
    cluster: signalsd-signals-read

# Admin operations
- match:
    prefix: "/"
  route:
    cluster: signalsd-admin
```
![advanced config (v0 7 2)](https://github.com/user-attachments/assets/88b8fe9b-1329-45d9-b96c-fd4dde831026)


## Google Cloud Run Setup

The steps to set up this environment in Google Cloud are below.  This is the configuration used by the github actions CD pipelines.

prerequisites: set up an empty prod and/or staging DB on your postgres provider - you will need the connection URLs and a random secret key for each environment.

### 1. Create Google Cloud Resources
- Create a project called `signalsd`
- Create an artifact registry called `signalsd`

### 2. Create Service Accounts (IAM > Service Accounts)
- `cloud-run-deploy`
- `cloud-run-runtime`

### 3. Configure cloud-run-deploy Account
The `cloud-run-deploy` account will:
- Build an image each time there is a push on main
- Push the image to the artifact registry (the image is tagged with the commit id and 'latest')
- Create a container based on the latest image and deploy to Cloud Run (the container will run under the cloud-run-runtime account)

### 4. Set Service Account Permissions
- `cloud-run-deploy` needs the **Artifact Registry Writer** and **Cloud Run Admin** roles
- The `cloud-run-runtime` account does not need access to any of the Google APIs and therefore doesn't have any roles. You do however need to configure it to allow the cloud-run-deploy account to use it:

  **IAM > Service Accounts > cloud-run-runtime account > manage details > Principals with access > grant access**

  Add the cloud-run-deploy account email address - give it the **Service Account User** role.

### 5. Download Service Account Key
Download a JSON key for the cloud-run-deploy account:
**IAM > service accounts > cloud-run-deploy account > keys > add key > Create New**

### 6. Configure GitHub Secrets and Variables
The deployment workflows (`.github/workflows`) are configured to use GitHub variables for non-sensitive configuration and GitHub secrets for sensitive configuration.


#### Required GitHub Secrets
Set up GitHub secrets in your fork of the repo:
**repo > settings > secrets and variables > actions > secrets tab > new repository secret**

You will need three secrets:
- `GCP_CREDENTIALS` (upload the contents of the JSON key downloaded earlier)
- `DATABASE_URL` (URL of your postgres service - we are using Neon.tech, but any provider that supports current postgres versions should work)
- `SECRET_KEY` (random secret key for your app - used by the signalsd server to sign JWT tokens)

if you are deploying to a staging environment you need to create a separate database and two additional secrets:
- `STAGING_DATABASE_URL`
- `STAGING_SECRET_KEY`

#### DNS
google will automatically assign a *.run.app domain name to your Gcloud run service(s) and these can be mapped to custom domains using Google Cloud run DNS (see https://cloud.google.com/run/docs/mapping-custom-domains)

#### Optional GitHub Variables 
For production tuning, you can set GitHub variables:
**repo > settings > secrets and variables > actions > variables tab > new repository variable**

if you don't set these variables, app defautls are used.

Recommended production variables:
```
# Database Pool Configuration
DB_MAX_CONNECTIONS=25
DB_MIN_CONNECTIONS=0
DB_MAX_CONN_LIFETIME=120m
DB_MAX_CONN_IDLE_TIME=20m
DB_CONNECT_TIMEOUT=10s

# Performance Configuration
RATE_LIMIT_RPS=200
RATE_LIMIT_BURST=50

# Server Configuration
READ_TIMEOUT=30s
WRITE_TIMEOUT=30s
IDLE_TIMEOUT=120s
```

### 7. Cost Considerations
Note that at the time of writing this service operates within the free-tiers offered by Google and Neon.Tech, but you should check the current rules to be sure.

That's it!



