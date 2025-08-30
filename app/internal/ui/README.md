# Signalsd UI

A simple web user interface for managing Information Sharing Networks (ISNs) built with Go, HTMX, and templ. By default the UI runs as an integrated service within signalsd. Alternatively the backend can be run on its own and used with a different UI if required.

## Architecture

```
internal/ui/
├── server.go          # HTTP server setup and routing
├── handlers.go        # HTTP request handlers
├── auth.go            # Authentication service for API integration
├── types.go           # Shared type definitions
├── middleware.go      # authentication middleware
├── client.go.         # Client for calling signalsd API
├── config.go          # Configuration management (standalone mode)
├── templates.templ    # templ HTML templates
└── templates_templ.go # Generated Go code from templ templates
```


###  Integrated UI (Default)

The standard integrated mode (`-mode all`) is the simplist, everything runs on the same domain/port:

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
running the integrated service:

```bash
# Generate templ templates
make templ-generate

# start databasae
docker compose up db

# start app with docker
docker compose up app

#... or run signalsd locally with integrated UI (the below command uses the signalsd docker db)
SECRET_KEY=your-secret DATABASE_URL="postgres://signalsd-dev:@localhost:15432/signalsd_admin?sslmode=disable" signalsd --mode all

The integrated UI runs on the same port as the API (default: 8080).
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

The standalone mode requires a reverse proxy so that the client sees a single domain/port (The refresh token authentication will  not work without it).

⚠️ If you run the UI in standalone mode in dev, the login will work but automatic token refresh will fail because the refresh token cookie cannot be sent cross-port. Users will be logged out after 30 minutes.

***Production Setup***

**Step 1**: Deploy containers separately
```bash
# API container
signalsd --mode api

# UI container
signalsd --mode ui
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
When running as integrated UI (`--mode all` or `--mode ui`), the UI uses signalsd's configuration. No separate configuration needed.

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
you can then rund the app locally run with live reload (the below command is using the docker signalsd database)
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