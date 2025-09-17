# Signalsd Testing

## Unit Tests 

Unit tests are used to test a couple of areas:
- `app/internal/server/utils/utils_test.go` - URL validation and SSRF protection (ensures user submitted URLs are only GitHub URLs)
- `app/internal/server/request_limits_test.go` - Rate limiting and request size controls

## Integration Tests

The integration tests are designed to ensure that signal data is handled correctly and that authentication, authorization and privacy controls work as intended.

**integration test helper files `app/test/integration/`:**

- `setup_test_env.go` - Test environment setup and server lifecycle management
- `database.go` - database query test helpers

### 1. Authentication & Authorization (`auth_test.go`)

These tests verify the authentication and authorization system by running database queries directly and inspecting generated tokens.

- ✅ JWT token structure and claims validation
- ✅ Role-based permissions (owner, admin, member)
- ✅ Explicit permission grants and ISN access control
- ✅ Service account batch handling and client credentials
- ✅ Login flows and refresh token rotation
- ✅ Disabled account handling


### 2. User & Service Account Registration (`register_login_reset_test.go`)
Tests for user and service account registration via HTTP requests.

- ✅ User registration 
- ✅ Login 
- ✅ Password reset (admin generated link)
- ✅ Service account registration
- ✅ Service acccount credential reissue

### 3. OAuth (`oauth_test.go`)
Tests OAuth token generation and revocation via HTTP requests.

- ✅ Client credentials grant (service accounts)
- ✅ Refresh token grant (web users)
- ✅ Token revocation for both account types
- ✅ Cookie handling and rotation
- ✅ Error response validation

### 4. Signal Endpoints (`signal_test.go`)

Tests signal creation, search, and security controls via HTTP requests.

- ✅ Signal submission (successful and failed scenarios)
- ✅ Schema validation and correlation handling
- ✅ Multi-signal payload processing
- ✅ Signal search with authorization controls
- ✅ Public vs private ISN access
- ✅ Withdrawn signal handling
- ✅ Token validation (expired, malformed, missing)
- ✅ Cross-ISN data leakage prevention


### 5. Batch Management (`batch_test.go`)

- ✅ Batch creation and automatic closure
- ✅ Service account submission requirements
- ✅ Batch validation and error handling

**TODO**
- Rate limiting integration tests (unit tests exist)
- ISN admin endpoints (auth is tested but HTTP handling not yet implemented)

### 6. CORS (`cors_test.go`)

- ✅ Origin validation and enforcement
- ✅ Public vs protected endpoint policies

## Running the tests
```bash
# Start the development database
docker compose up db

# run tests from app directory
cd app

# Run integration tests
go test -tags=integration ./test/integration/

# Enable detailed HTTP request/response logging
ENABLE_SERVER_LOGS=true go test -v -tags=integration ./test/integration/

# Run unit tests
go test ./...
```

### Test Environment
- **Local**: Uses dev Docker PostgreSQL (port 15432)
- **CI**: Uses GitHub Actions PostgreSQL (port 5432)
- Each test creates a temporary database with latest migrations
- HTTP tests start signalsd on a random port
- Database and server are cleaned up after each test
