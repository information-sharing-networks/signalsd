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

The UI uses the same configuration system as the main signalsd service. Key environment variables:

- `PORT`: UI server port (default: 3000)
- `HOST`: Server host (default: 0.0.0.0)
- `ENVIRONMENT`: Environment mode (dev/production)
- `SECRET_KEY`: Required for session management

## Authentication Flow

1. User submits login form via HTMX
2. UI calls signalsd API `/auth/login` endpoint
3. On success, JWT token is stored in HTTP-only cookie
4. Subsequent requests validate token with signalsd API
5. Logout clears the authentication cookie

## API Integration

The UI integrates with the existing signalsd API:

- **Login**: `POST /auth/login`
- **Token Validation**: `GET /api/isn` (used to validate tokens)
- **Documentation**: Redirects to `/docs` on the API server

## Design Principles

- **Simplicity**: Clean, readable code without unnecessary abstractions
- **Idiomatic Go**: Follows standard Go patterns and conventions
- **Maintainability**: Clear separation of concerns and straightforward structure
- **Integration**: Seamless integration with existing signalsd API
