[Intro](#information-sharing-networks) |
[Developer Guide](#developer-guide) |
[Technical Overview](#technical-overview)

![ci](https://github.com/information-sharing-networks/signalsd/actions/workflows/ci.yml/badge.svg)
 ![cd-staging](https://github.com/information-sharing-networks/signalsd/actions/workflows/cd-staging.yml/badge.svg)
 ![cd-production](https://github.com/information-sharing-networks/signalsd/actions/workflows/cd-production.yml/badge.svg)

# Information Sharing Networks

Information Sharing Networks (ISNs) let organisations create new data sharing arrangements without building complex custom technology solutions each time. 

Participants exchange information through 'signals' - simple messages that allow the transfer of information between network members.

## Signals
Signals notify authorised organisations when key events occur within your business processes.  For example, when an order ships, a decision is made, or a review is completed, the corresponding signal is sent immediately to authorised participants.

Each signal type contains minimal data and follows straightforward formatting rules. Signals can be linked to form a timeline of related events and the system handles version control so that more detail can be added to previously issued signals when new information emerges.

Organisations can use signals to share:
- Events: Order confirmations, delivery notifications, status updates
- Decisions: Approvals, rejections, policy amendments
- Analysis: Audit results, risk assessments, compliance findings
- Verification: Confirmation of the accuracy of data from other organisations

## Setting Up Networks
This service provides the ISN management facilities. It is easy to deploy the service to public cloud infrastructure and is designed to be operated with minimal technical support.

The service makes it easy to establish new networks and to control the data that can be shared and who can acceess it.

Many data sharing initiatives encounter difficulties because they attempt to anticipate every possible need across multiple organisations, requiring extensive planning, complex agreements, and data models designed to cover scenarios that may never materialise.

ISNs are different: a network is set up for a specific business purpose - such as tracking shipments, sharing compliance data, or coordinating approvals - and only the necessary signals for that purpose are defined. As new requirements emerge, additional signal types can be introduced without disrupting existing processes.

This approach enables rapid implementation of effective data sharing, while maintaining flexibility as business relationships evolve.

## Integrating with existing systems
The service supports logged-in web users and system-to-system access via service accounts.  Authentication follows Oauth 2.0 standards and data is submitted and received as JSON over simple REST APIs.

Signal types are defined as JSON schemas and the service can (optionally) validate data against the registered schema prior to loading.

## Reference Implementations
The [initial implementation](https://github.com/information-sharing-networks/isn-ref-impl) was a proof of concept used as part of the UK government's Border Trade Demonstrator (BTD) initiative. The BTDs established ISNs that were used by several government agencies and industry groups to improve processes at the border by sharing supply chain information.

This repo contains the second version of the service, which develops the ISN administration facilities and is designed to scale to higher volumes of data.

There are three components:
- an [API](https://signalsd.corridorone.uk/docs) used to configure ISNs, register participants and deploy the data sharing infrastructure
- a demonstration [UI](app/internal/ui/README.md)
- an associated [framework agreement](https://github.com/information-sharing-networks/Framework) that establishes the responsibilities of the participants in an ISN

## Credits
Many thanks to [Ross McDonald](https://github.com/rossajmcd) who came up with the concept and created the initial reference implementation.

# Developer Guide

## Quick Start (Docker)

**Prerequisites**:

to run the service you need [Docker Desktop](https://docs.docker.com/get-docker)

If you are planning to change the software you will also need to install [Go](https://go.dev/doc/install)

**Running the app**:
```bash
# Clone the repo
git clone https://github.com/information-sharing-networks/signalsd.git

# Start the service and database
cd signalsd
make docker-up
```
The service starts on [http://localhost:8080](http://localhost:8080) by default.

The docker compose file in the root of the repo (`docker-compose.yml`) starts the service and a PostgreSQL database (the containers are called `app` and `db` respectively).

The docker compose file mounts the local repo directory into the app container, so you can edit code locally and see the changes in the container.  The app container has all the tools you need to test and run the service.

the `make` commands below are a shortcut for the corresponding `docker compose` commands.  They are defined in the `Makefile` in the root of the repo.  If you prefer you can use `docker compose` directly.

```bash
# you can override default environment variables by setting them in your shell before running the command, for example:
PORT=8081 make docker-up

# start the database container only
make docker-up-db 

# start the app container only
make docker-up-app 

# Rebuild the image when there is a change to:
# - Dockerfile
# - go.mod/go.sum (new dependencies)
make docker-build

# Stop the service and database
make docker-down

# see all available make targets
make help
```

The API documentation is hosted as part of the service or you can refer to the [docs](https://signals.corridorone.uk/docs) for the current release.

## Environment Variables
The service has sensible defaults for all configuration values when running locally.
You only need to set environment variables to override the defaults.
A sample config is below:

```bash
# **Required in production**
#
# Database URL - note production urls must use ssl.
DATABASE_URL=postgres://user:password@host:port/database?sslmode=require
# Secret key used by the server to sign JWT tokens
SECRET_KEY=your-random-secret-key
# Base URL for user facing links, e.g one-time password links (default: http://localhost:8080)
PUBLIC_BASE_URL=https://your-server-domain.com 
# CORS origins - list sites that are allowed to use the API
ALLOWED_ORIGINS=https://your-ui-domain.com  #  use a pipe seperated list for multiple sites

# **Optional configuration - defaults shown**
#
# Server Configuration 
HOST=0.0.0.0                          #  Bind address (default: 0.0.0.0)
PORT=8080                             #  Server port (default: 8080)
ENVIRONMENT=dev                       #  Options: dev, prod, test, perf, staging (default: dev)
LOG_LEVEL=debug                       #  Options: debug, info, warn, error (default: debug)

# Performance Tuning 
READ_TIMEOUT=15s                      #  HTTP read timeout 
WRITE_TIMEOUT=15s                     #  HTTP write timeout 
IDLE_TIMEOUT=60s                      #  HTTP idle timeout 
RATE_LIMIT_RPS=2500                   #  Requests per second (set to 0 to disable)
RATE_LIMIT_BURST=5000                 #  Burst allowance 
MAX_SIGNAL_PAYLOAD_SIZE=5242880       #  Max payload size (default: 5MB)
MAX_API_REQUEST_SIZE=65536            #  Max API request size (default: 64KB)

# Database Connection Pool (the default used are the same as those used by pgx )
DB_MAX_CONNECTIONS=4
DB_MIN_CONNECTIONS=0                  #  Allow scaling to zero (Cloud Run)
DB_MAX_CONN_LIFETIME=60m
DB_MAX_CONN_IDLE_TIME=30m
DB_CONNECT_TIMEOUT=5s
```
**Note**
DATABASE_URL and SECRET_KEY contain sensitive information - production versions should be managed via a secrets management system (for local docker environments these values are set in `docker-compose.yml`)

### Development Tools

The app uses the following go tools:
- goose - database migrations
- sqlc - type safe SQL queries
- swag - OpenAPI from go comments
- staticcheck - linter
- gosec - security analysis
- templ - ui templates
- govulncheck - vulnerability scanner
- Air - live reload

These tools are defined in `app/tools.go` and installed as part of the docker image.

The Docker app is started with Air which will restart the service whenever you save changes to the code. 
Air is configured to automatically run code generation (templ, sqlc and swag) and applies any pending database migrations before restarting the signalsd server.
You can customise the air config by editing `.air.toml` (see the [air docs](https://github.com/cosmtrek/air) for more information).

If you need to run these tools individually, you can use the Makefile for common tasks:

```bash
# Start containers first
make docker-up

# Then...
make check    # Run all pre-commit checks and generate sqlc code and api docs
make generate # Generate sqlc code and api docs
make migrate  # Run database migrations
...
```

## Database Schema Management
database schema migration is managed using [goose](https://github.com/pressly/goose).  

Schema changes are made by adding files to `app/sql/schema`:
```
001_foo.sql
002_bar.sql
...
```
For docker users the migration are applied automatically whenever you restart the app container (use `make migrate` to run mannually). 

## API Documentation
To generate the OpenAPI docs:
```bash
make docs
```
For docker users, the docs are automatically created when Air live reload restarts the app container.

## SQL Queries
SQL queries are kept in `app/sql/queries`.

Run `make sqlc` to regenerate the type safe Go code after adding or altering any queries (runs automatically for docker users)

## Testing
`make test` will run the unit and integration tests.

`make check` will run all the security, linting, unit and integration tests.

For information about the testing strategy and how to run indvidual tests, see the [Integration Testing Documentation](app/test/integration/README.md).

details on performance testing are in [Performance Testing Documentation](test/perf/README.md).

## User Interface
By default the signalsd service starts with a basic web interface. If you want to modify, replace or disable the UI see the [UI Documentation](app/internal/ui/README.md).

## Getting Help
- Check the [API documentation](https://signalsd.corridorone.uk/docs)
- Review logs: `make logs`
- Open an [issue](https://github.com/information-sharing-networks/signalsd/issues) on GitHub


# Technical overview
## Auth
![Auth-2026-03-18](https://github.com/user-attachments/assets/7eff5976-25c8-4b7b-972b-fbe3d261a1ab)

## ISN config
![ISN config v0 5 0](https://github.com/user-attachments/assets/2be326f2-f4d0-485e-aeed-28076383cd8e)


## Signal Load
![SignalsLoad-2026-03-18](https://github.com/user-attachments/assets/130eede1-5b6a-4ca6-97fc-28ce0e8fb194)

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
make check

# 2. Create and push version tag; build locally with version info
build.sh -t patch|minor|major
```

## Cloud Deployment

This service is deployed to Google Cloud Run.  Google handles HTTPS, firewall, load balancing and autoscaling. The service will scale to zero when not in use.

**Note: This is pre-production software and the cloud deployment should only be used with data that you don't mind being deleted or seen by other people.**

## Service Mode Configuration

You can run multiple instances of the signalsd service, each in a different mode.  This enables you to, for example, run a separate service for admin and signal processing workloads.
The service mode is specified using the `run` command with one of the following arguments:

- **`all`**: Serves all API endpoints + UI
- **`api`**: Serves all API endpoints 
- **`admin`**: Serves only admin API endpoints (excludes signal exchange)
- **`signals`**: Serves signal exchange endpoints (both read and write operations)
- **`signals-read`**: Serves only signal read operations 
- **`signals-write`**: Serves only signal write operations

```sh
PORT=8080 go run cmd/signalsd/main.go run all
PORT=8081 go run cmd/signalsd/main.go run signals-read
PORT=8082 go run cmd/signalsd/main.go run signals-write
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
      regex: "^/api/isn/.*/signal-types/.*/signals.*"
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
      regex: "^/api/isn/.*/signal-types/.*/signals$"
    method: "POST"
  route:
    cluster: signalsd-signals-write

# Read operations
- match:
    path:
      regex: "^/api/isn/.*/signal-types/.*/signals/.*"
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

You need to create two github environment variables: 
- `PUBLIC_BASE_URL` (e.g. https://yourdomain.com) to hold the public base url for your app
- `ALLOWED_ORIGINS` (e.g. https://yourdomain.com) to hold a pipe-separated list of allowed CORS origins

If you are deploying to a staging environment you need to create separate variable to hold these values:
- `STAGING_PUBLIC_BASE_URL` (e.g. https://staging.yourdomain.com)
- `STAGING_ALLOWED_ORIGINS` (e.g. https://staging.yourdomain.com)

The public base URL is used by the back-end server when generating password reset and service account setup links.

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
RATE_LIMIT_RPS=2000
RATE_LIMIT_BURST=5000

# Server Configuration
READ_TIMEOUT=30s
WRITE_TIMEOUT=30s
IDLE_TIMEOUT=120s
```