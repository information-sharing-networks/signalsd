# Signalsd Testing

## UI Testing
There are no automated tests for the UI, beyond the testing of the backend API (signalsd) that it relies on. If you want to do some exploratory testing on the web app UI in a new dev installation, you can use: 

```bash
cd app
# only use for dev envs!
go run ./cmd/seed
```
... which will create a basic set of accounts and isns.  The siteadmin is `owner@gmail.com` (password 12345678901).  

## Unit Tests 

Unit tests are used to test a couple of areas:
- `app/internal/utils/utils_test.go` - URL validation and SSRF protection (ensures user submitted URLs are only GitHub URLs)
- `app/internal/server/request_limits_test.go` - Rate limiting and request size controls

## Integration Tests

The integration tests are designed to ensure that signal data is handled correctly and that authentication, authorization and privacy controls work as intended.

**integration test helper files `app/test/integration/`:**

- `env_setup.go` - starts the in-process test server and creates a fresh database for each test
- `helpers_data.go` - records created directly in the database (accounts, ISNs, signal types, permissions)
- `helpers_auth.go` - access tokens and login requests
- `helpers_responses.go` - response checks (`expectStatus`, `expectJSONResponse`, `expectErrorCode`)
- `helpers_signals.go` - the signal APIs (submit, search, withdraw, signal router, routing config)

Helpers used by more than one test file live in a `helpers_*.go` file. Helpers used by a single test file are at the bottom of that file.

### 1. Authentication & Authorization (`auth_test.go`)

These tests verify the authentication and authorization system by running database queries directly and inspecting generated tokens.

- ✅ JWT token structure and claims validation
- ✅ Role-based permissions (siteadmin, isnadmin, member)
- ✅ Explicit permission grants and ISN access control
- ✅ Service account batch handling and client credentials
- ✅ Login flows and refresh token rotation
- ✅ Disabled account handling


### 2. User & Service Account Registration (`account_test.go`)
Tests for user and service account registration via HTTP requests.

- ✅ User registration
- ✅ Login
- ✅ Password reset (admin generated link)
- ✅ Self-serve password change (authenticated users)
- ✅ Service account registration
- ✅ Service acccount credential reissue

### 3. OAuth (`oauth_test.go`)
Tests OAuth token generation and revocation via HTTP requests.

- ✅ Client credentials grant (service accounts)
- ✅ Refresh token grant (web users)
- ✅ Token revocation for both account types
- ✅ Cookie handling and rotation
- ✅ Error response validation

### 4. Signal Endpoints (`signal_submission_test.go`, `signal_search_test.go`)

Tests signal creation, search, and security controls via HTTP requests.

- ✅ Signal submission (successful and failed scenarios)
- ✅ Schema validation and correlation handling
- ✅ Multi-signal payload processing
- ✅ Disabled ISNs and signal types
- ✅ Signal search with authorization controls
- ✅ Public vs private ISN access
- ✅ Withdrawn signal handling
- ✅ Write-only account visibility
- ✅ Previous versions and correlated signals (`include_correlated`, `correlation_id`)
- ✅ Token validation (expired, malformed, missing)
- ✅ Cross-ISN data leakage prevention (`shared_signal_type_test.go`)
- ✅ Signal router and routing config (`signal_router_test.go`, `signal_routing_test.go`)
- ✅ Document signal types (`document_signal_type_test.go`)
- ✅ Postgres document store (`document_store_test.go`)
- ✅ Site-wide signal type details and the signal types added to each ISN (`signal_type_test.go`)


### 5. Batch Management (`batch_test.go`)

- ✅ Batch creation and automatic closure
- ✅ Service account submission requirements
- ✅ Batch validation and error handling

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
