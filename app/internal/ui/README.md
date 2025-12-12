# Signalsd UI

A simple web user interface for managing Information Sharing Networks (ISNs) built with Go, HTMX, and templ. By default the UI runs as an integrated service within signalsd. Alternatively the backend can be run on its own and used with a different UI if required.

## Architecture

```
internal/ui/
├── server/server.go            # HTTP server setup and routing
├── handlers/                   # HTTP request handlers - render pages and calls the UI api
├── client/client.go            # Client for calling signalsd backend API
├── auth/auth.go                # Authentication service for API integration
├── auth/middleware.go          # authentication middleware
├── types/types.go              # Shared type definitions
├── config/config.go            # Configuration management (standalone mode)
├── templates/*.templ           # templ HTML templates
└── templates/*.go              # Generated Go code from templ templates 
```
The steps to add a new interactive page to the UI are:
1. Create Client methods to call the relevant signalsd API endpoints
2. Add a new page template (`.templ` file) for the new feature
3. Add a new handler function in the appropriate handlers/*.go file to render the page
4. Add new ui-api handler functions in the appropriate handlers/*.go file to handle any interactions required in the page (use HTMX to make partial page updates)
5. Add routes for the page and ui-api calls in `server/server.go`, using the appropriate authentication middleware

###  Integrated UI (Default)

The default integrated mode (`signalsd run all`) is the simplest way to run the ui, everything runs on the same domain/port:

```
┌─────────────────────────────────────┐
│        signalsd binary              │
│  ┌─────────────┐  ┌─────────────┐   │
│  │     API     │  │  Default UI │   │
│  │   :8080     │  │   (Go/HTMX) │   │
│  └─────────────┘  └─────────────┘   │
└─────────────────────────────────────┘
         Same Domain (localhost:8080)
```
The integrated UI is built into the signalsd binary and available on the same port as the API (default: 8080).  The integrated UI runs automatically when using docker:

```bash
# start database
docker compose up db

# start app (including integrated ui)
docker compose up app
```

Login using `http://localhost:8080/login`

When using docker, the app runs with a live reload server (air) so you can develop the UI locally and see changes immediately. 

if you want to run the app locally (not in docker) you can use the below.  Note there is no live reload so you will need to regenerate the templates and stop and start the app after making changes.


```bash

# start the app locally
make go-all # note expects the docker database to be running

# or specify your own local database
SECRET_KEY=your-secret DATABASE_URL="postgres://signalsd-dev:@localhost:5432/signalsd_admin?sslmode=disable" signalsd run all

```

Switch to standalone mode when you need container separation or want to replace the UI

### Standalone UI 

```
┌─────────────────┐    ┌─────────────────┐
│   Your Custom   │    │   signalsd API  │
│      UI         │    │     :8080       │
│  (React/Vue/etc)│    │                 │
│     :3000       │    │                 │
└─────────────────┘    └─────────────────┘
         │                       │
         └───────────────────────┘
                   │
         ┌─────────────────┐
         │ Reverse Proxy   │
         │ (nginx/Caddy)   │
         │    :80/443      │
         └─────────────────┘
           Same Domain
           (Required for HttpOnly cookies)
```

The standalone mode requires a reverse proxy so that the client sees a single domain/port (The refresh token authentication will not work without it due to cross-origin cookie restrictions).

⚠️ If you run the UI in standalone mode in dev, the login will work but automatic token refresh will fail because the refresh token cookie cannot be sent cross-port. Users will be logged out after 30 minutes.

***Production Setup***

**Prod and staging deployment** these environments currently run the integrated ui/signalsd service.

To deploy a standalone UI you will need to build and deploy two containers:

**Step 1**: Deploy containers separately
```bash
# API container
 go build -o signalsd cmd/signalsd/main.go 
signalsd run api

# UI container
cd app &&  go build -o signalsd-ui cmd/signalsd-ui/main.go 
signalsd-ui
```

**Step 2**: Configure reverse proxy (nginx example)
```nginx
server {
    listen 80;
    server_name yourdomain.com;

    # UI requests
    location / {
        proxy_pass http://ui-container:3000;
    }

    # API requests
    location /api/ {
        proxy_pass http://api-container:8080;
    }

    # OAuth requests
    location /oauth/ {
        proxy_pass http://api-container:8080;
    }
}
```
## Configuration

### Integrated Mode (Default)
When running as integrated UI (`signalsd run all`), the UI uses signalsd's configuration. No separate configuration needed.

### Standalone Mode (Development/Custom Deployments)

When running as a separate service, the UI has its own configuration:

- `PORT`: UI server port (default: 3000)
- `HOST`: Server host (default: 0.0.0.0)
- `ENVIRONMENT`: Environment mode (dev/test/perf/staging/prod, default: dev)
- `LOG_LEVEL`: Logging level (default: debug)
- `API_BASE_URL`: Base URL of the signalsd API (default: http://localhost:8080)
- `READ_TIMEOUT`: HTTP read timeout (default: 15s)
- `WRITE_TIMEOUT`: HTTP write timeout (default: 15s)
- `IDLE_TIMEOUT`: HTTP idle timeout (default: 60s)

**No database or secret configuration required** - the UI calls the signalsd API for all data operations.


# Auth
All authentication and authorization is handled by the signalsd API.  

Authorization is via the server supplied JWT access tokens.  The UI decodes the tokens to:
1. determine the user's role and permissions so that it can improve the UX (e.g., hiding pages that the user does not have access to)
2. to establish the expiry time so that it can refresh the token before it expires. 

The UI does not need create tokens and does not need to know the SECRET_KEY used by the server.

# Development

## Prerequisites

1. Have Go 1.25+ installed
2. Set up database (see main signalsd README)

## Template Development
if developing locally, install templ and air (live reload): 
```bash
go install github.com/a-h/templ/cmd/templ@latest
go install github.com/air-verse/air@latest
```
you can then run the app locally with live reload (the below command is using the docker signalsd database)
```bash
cd app
DATABASE_URL="postgres://signalsd-dev@localhost:15432/signalsd_admin?sslmode=disable" SECRET_KEY="mysecretkey" air
```

the easiest approach is to use docker for both the app and db.  Air will handle live reloads when you change the templates - just `docker compose up` to get going. 

You can manually generate the templates code with:
```bash
#For docker users
make templ 

# For local users
cd app && templ generate
```