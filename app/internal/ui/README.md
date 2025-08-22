# Signalsd UI

A simple web user interface for managing Information Sharing Networks (ISNs) built with Go, HTMX, and templ.

## Features

- **Simple Navigation**: Login/logout, dashboard, and API documentation access
- **Authentication Integration**: Integrates with the existing signalsd API for user authentication
- **Responsive Design**: Built with Tailwind CSS for a clean, responsive interface
- **HTMX Interactions**: Dynamic form submissions and navigation without full page reloads

## Architecture

The UI is structured following Go best practices with clear separation of concerns:

```
internal/ui/
├── server.go          # HTTP server setup and routing
├── handlers.go        # HTTP request handlers
├── auth.go           # Authentication service for API integration
├── templates.templ   # templ templates for HTML generation
└── templates_templ.go # Generated Go code from templ templates
```

## Technology Stack

- **Go**: Backend server and HTTP handling
- **templ**: Type-safe HTML templating
- **HTMX**: Dynamic frontend interactions
- **Tailwind CSS**: Styling and responsive design
- **Chi Router**: HTTP routing (inherited from main signalsd project)

## Development

### Prerequisites

1. Ensure the main signalsd API is running (typically on port 8080)
2. Have Go 1.24+ installed
3. Install templ: `go install github.com/a-h/templ/cmd/templ@latest`

### Running the UI

```bash
# Generate templ templates
make templ-generate

# Run in development mode
make ui-dev

# Or build and run
make ui-build
./signalsd-ui
```

The UI server will start on port 3000 by default (configurable via PORT environment variable).

### Template Development

When modifying `.templ` files, regenerate the Go code:

```bash
make templ-generate
```

## Configuration

The UI has its own simplified configuration system, separate from the main signalsd API. Key environment variables:

- `PORT`: UI server port (default: 3000)
- `HOST`: Server host (default: 0.0.0.0)
- `ENVIRONMENT`: Environment mode (dev/test/perf/staging/prod, default: dev)
- `LOG_LEVEL`: Logging level (default: debug)
- `API_BASE_URL`: Base URL of the signalsd API (default: http://localhost:8080)
- `READ_TIMEOUT`: HTTP read timeout (default: 15s)
- `WRITE_TIMEOUT`: HTTP write timeout (default: 15s)
- `IDLE_TIMEOUT`: HTTP idle timeout (default: 60s)

**No database or secret configuration required** - the UI is a simple web server that calls the signalsd API.

## Authentication Flow

1. **Login**: User submits form → UI calls `/api/auth/login`
2. **Token Storage**:
   - API returns access token (30 min) in JSON response
   - API automatically sets refresh token (30 days) as HTTP-only cookie
   - UI stores access token in its own HTTP-only cookie
3. **Authentication Check**: UI validates access token with API
4. **Automatic Refresh**: When access token expires:
   - UI detects 401 response from API
   - UI calls `/oauth/token?grant_type=refresh_token` with both tokens
   - API returns new access token + rotates refresh token cookie
   - User stays logged in seamlessly
5. **Logout**: Clears both access token and refresh token cookies

## API Integration

The UI integrates with the existing signalsd API:

- **Login**: `POST /api/auth/login`
- **Token Validation**: `GET /api/isn` (used to validate tokens)
- **Documentation**: Redirects to `/docs` on the API server

## Design Principles

- **Simplicity**: Clean, readable code without unnecessary abstractions
- **Idiomatic Go**: Follows standard Go patterns and conventions
- **Maintainability**: Clear separation of concerns and straightforward structure
- **Integration**: Seamless integration with existing signalsd API
