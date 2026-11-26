[Intro](#information-sharing-networks) |
[Running signalsd](#running-signalsd) |
[Contributing](CONTRIBUTING.md) |
[Technical Overview](#technical-overview)

![ci](https://github.com/information-sharing-networks/signalsd/actions/workflows/ci.yml/badge.svg)
![cd-staging](https://github.com/information-sharing-networks/signalsd/actions/workflows/cd-staging-aws.yml/badge.svg)
![cd-production](https://github.com/information-sharing-networks/signalsd/actions/workflows/cd-production-aws.yml/badge.svg)
[![Ask DeepWiki](https://img.shields.io/badge/Ask-DeepWiki-blue)](https://deepwiki.com/information-sharing-networks/signalsd)

# Information Sharing Networks
Information Sharing Networks (ISNs) give organisations a way to start sharing data with each other without having to build bespoke technology from scratch every time.

Members of a network exchange information through 'signals' - lightweight messages that pass information between participants.

## Signals

A signal is a notification: something happened in your business and you want to let other people know about it. For instance, when an order ships, a decision gets made or a review completes a signal goes out to whoever's been given access.

Each signal type contains only what's needed - there are no complex payloads carrying data that might never be used. Signals can be chained together to build up a timeline of related events and - if more detail becomes available later - you can add it to a signal that's already been sent.

Organisations can use signals to share things like:

- Events - order confirmations, delivery notifications, status updates
- Decisions - approvals, rejections, policy changes
- Analysis - audit results, risk assessments, compliance findings
- Verification - confirming that data from another organisation is accurate

## Setting Up Networks

The service handles all the ISN management - it's straightforward to deploy on public cloud infrastructure and doesn't require a dedicated technical team to keep running.

One of the reasons data sharing projects stall is that they try to solve everything at once - teams can spend months mapping out every conceivable use case across multiple organisations, negotiating complex agreements and building data models for scenarios that never actually happen.

ISNs take a different approach: you set up a network for one specific purpose - tracking shipments, sharing compliance data, coordinating approvals - and define only the signals you need. When new requirements arise, you add signal types as you go without breaking anything that's already in place.

The result is something you can get running quickly and that can grow as business relationships evolve.

## Integrating with existing systems
The service supports logged-in web users and system-to-system access via service accounts.  Authentication follows Oauth 2.0 standards and data is submitted and received as JSON over simple REST APIs.

Signal types are defined as JSON schemas and the service can (optionally) validate data against a registered schema prior to loading.

## Reference Implementations
The [initial implementation](https://github.com/information-sharing-networks/isn-ref-impl) was a proof of concept used as part of the UK government's _Border Trade Demonstrator_ (BTD) initiative. The BTD initiative established ISNs that were used by several government agencies and industry groups to improve processes at the border by sharing supply chain information.

This repo contains the second version of the service, which develops the ISN administration facilities and is designed to scale to higher volumes of data.

There are three components:
- an [API](https://signalsd-staging.corridorone.uk/docs) used to configure ISNs, register participants and deploy the data sharing infrastructure
- an admin [UI](app/internal/ui/README.md)
- an associated [framework agreement](https://github.com/information-sharing-networks/Framework) that establishes the responsibilities of the participants in an ISN

## Credits
Many thanks to [Ross McDonald](https://github.com/rossajmcd) who came up with the concept and created the initial reference implementation.

# Running signalsd

## Quick Start (Docker)

**Prerequisites**: [Docker Desktop](https://docs.docker.com/get-docker)

```bash
# Clone the repo
git clone https://github.com/information-sharing-networks/signalsd.git

# Start the service and database
cd signalsd
make docker-up
```
The service starts on [http://localhost:8080](http://localhost:8080) by default.

The docker compose file in the root of the repo (`docker-compose.yml`) starts the service and a PostgreSQL database (the containers are called `app` and `db` respectively).

The `make` commands below are defined in the `Makefile` in the root of the repo and wrap `docker compose` (setting `GO_VERSION` automatically from `app/go.mod`).

```bash
# Override default environment variables by setting them in your shell before running the command, for example:
PORT=8081 make docker-up

# Stop the service and database
make docker-down

# See all available make targets
make help
```

The API documentation is hosted as part of the service (at `/docs`) or you can refer to the [docs](https://signalsd.corridorone.uk/docs) for the current release.

If you want to change the code, see [CONTRIBUTING.md](CONTRIBUTING.md) for the development setup, tooling and workflow.

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

# **Optional configuration - defaults shown**
#
# Server Configuration 
HOST=0.0.0.0                          #  Bind address (default: 0.0.0.0)
PORT=8080                             #  Server port (default: 8080)
ENVIRONMENT=dev                       #  Options: dev, prod, test, perf, staging (default: dev)
LOG_LEVEL=debug                       #  Options: debug, info, warn, error (default: debug)
TRUSTED_PROXIES=1                     #  Number of reverse proxies in front of the service 

# Performance Tuning 
READ_TIMEOUT=15s                      #  HTTP read timeout 
WRITE_TIMEOUT=15s                     #  HTTP write timeout 
IDLE_TIMEOUT=60s                      #  HTTP idle timeout 
RATE_LIMIT_RPS=2500                   #  Requests per second (set to 0 to disable)
RATE_LIMIT_BURST=5000                 #  Burst allowance 
MAX_SIGNAL_PAYLOAD_SIZE=5242880       #  Max payload size (default: 5MB)
MAX_API_REQUEST_SIZE=65536            #  Max API request size (default: 64KB)
MAX_DOCUMENT_SIZE=20971520            #  Max document size (default: 20MB)
DOCUMENT_TRANSFER_TIMEOUT=2m          #  Time allowed to upload or download a document - used instead of READ_TIMEOUT/WRITE_TIMEOUT (default: 2m)

# Document storage
DOCUMENT_STORE=postgres               #  Where document content is stored. Options: postgres (default: postgres)

# Database Connection Pool (the default used are the same as those used by pgx )
DB_MAX_CONNECTIONS=4
DB_MIN_CONNECTIONS=0                  #  Allow scaling to zero (Cloud Run)
DB_MAX_CONN_LIFETIME=60m
DB_MAX_CONN_IDLE_TIME=30m
DB_CONNECT_TIMEOUT=5s
```
**Note**
DATABASE_URL and SECRET_KEY contain sensitive information - production versions should be managed via a secrets management system (for local docker environments these values are hardcoded in `docker-compose.yml`)

Set `TRUSTED_PROXIES` to match the number of proxy hops between the internet and the server
For a single load balancer (e.g. AWS ALB) the default of `1` is correct.
Add `1` for each additional proxy layer (e.g. a CDN in front of the ALB would require `2`).
This is used by the [chi](https://github.com/go-chi/chi) router's `ClientIPFromXFFTrustedProxies` middleware for correct client IP extraction from the `X-Forwarded-For` header

## User Interface
By default the signalsd service serves a basic web interface alongside the API. To run API-only, see [Service Configuration](#service-configuration); to modify or replace the UI, see the [UI Documentation](app/internal/ui/README.md).

## Getting Help
- Check the [API documentation](https://signalsd.corridorone.uk/docs)
- Open an [issue](https://github.com/information-sharing-networks/signalsd/issues) on GitHub


# Technical overview
The wiki provides a helpful overview of the technical aspects of the project (AI Generated):

[![Ask DeepWiki](https://img.shields.io/badge/Ask-DeepWiki-blue)](https://deepwiki.com/information-sharing-networks/signalsd)

The core design concepts are described in the three diagrams below.

## Auth
![auth-2026-10-04](https://github.com/user-attachments/assets/a42e7fe5-5707-4deb-9579-8f3170ae78fe)

## ISN config
![isn-config-2026-10-04](https://github.com/user-attachments/assets/d48dba0b-fc4a-4a4c-af0a-d3f5e234b9d0)

## Signal Load
![signals-load-2026-10-04](https://github.com/user-attachments/assets/cd0ad1a5-3dd3-4986-98c5-0a2d347bce71)

## Rate Limits
The service includes a shared rate limiter for all traffic regardless of source IP or user identity and protects all endpoints including auth, API, and admin routes.

This just provides basic protection against abuse - in a production environment you should configure your CDN/load balancer/reverse proxy with per-IP rate limiting.

## CI/CD overview
Github Workflows are used to automate checks and deployments.

CI checks run on every push to `main` and every pull request opened against it.

This app is currently deployed to AWS (see below for details):
- staging is deployed when CI passes on `main`
- production is deployed when a new version tag (e.g `v1.0.0`) is pushed.

The production workflow promotes the staging image built from the same commit being tagged. 

The CI workflow runs the same security, linting and application tests as `make check`
does locally, plus GitHub's CodeQL action.

See GitHub workflows in `.github/workflows/`

## Service Configuration

You can run multiple instances of the signalsd service. Optionally, you can run each instance in a different mode.  This enables you to, for example, run a separate service for admin and signal processing workloads.
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

The simplest configuration is to run containers that serve all endpoints.  This is the configuration used by the github actions CD pipeline - high volume implementations might benefit from splitting the admin and signals handling across multiple containers.

example of a more elaborate configuration
![advanced config (v0 7 2)](https://github.com/user-attachments/assets/88b8fe9b-1329-45d9-b96c-fd4dde831026)


# Cloud Deployment
The app currently deploys to AWS - the containers run on ECS Fargate and the database is RDS Postgres.

The app was deployed on GCP (CloudRun) and Neon Postgres until April 2026.  The Github Workflows are retained for reference but no longer run.

Overviews of the process to create the GCP and AWS infrastructure are here
- [AWS Deploy](deploy-AWS.md)
- [GCP Deploy](deploy-GCP.md)
