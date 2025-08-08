# Signalsd Testing

## Integration Tests

The integration tests are designed to ensure that signal data is handled correctly and that privacy controls work as intended.

**integration test files `app/test/integration/`:**
- `setup_test_env.go` - Test environment setup and server lifecycle management
- `auth_test.go` - Authentication and authorization system testing
- `http_test.go` - HTTP API endpoint testing with full request/response validation (signal submission & search) + CORS security
- `batch_test.go` - Service account batch lifecycle and validation testing
- `database.go` - Shared database test helpers

## Unit Tests 

Unit tests are used to test a couple of areas:
- `app/internal/server/utils/utils_test.go` - URL validation and SSRF protection (ensures user submitted URLs are only GitHub URLs)
- `app/internal/server/request_limits_test.go` - Rate limiting and request size controls


### 1. Authentication & Authorization (`auth_test.go`)

**What it tests:**

- ✅ JWT token structure - tokens are properly signed and parseable
- ✅ JWT claims (ISN permissions, role etc) match database 
- ✅ Token metadata - expiration, issued time, issuer, subject are correct
- ✅ Role-based permissions - owner gets write to all ISNs, admin gets write to own ISNs
- ✅ Explicit permission grants - member gets read access where granted
- ✅ Service account batch handling - service accounts require batch IDs for write permissions
- ✅ Signal type paths - correct signal type paths are included in permissions
- ✅ Disabled account handling
- ✅ Login - password validation, account status checks, access/refresh token generation, refresh token rotation
- ✅ service account authentication - client credentials validation, revoked and expired secrets


### 2. End-to-end Testing (`http_test.go`)

**What it tests:**

***Signals Creation:***
 - ✅ Successful submission
 - ✅ Failed submission due to validation errors
 - ✅ Failed submission due to authorization errors
 - ✅ Failed submission due to request errors (e.g. invalid JSON)
 - ✅ Signal versioning and reactivation of withdrawn signals
 - ✅ loading signals with Correlation IDs
 - ✅ Multi-signal payload processing with mixed success/failure scenarios

***Signal Search:***
 - ✅ Search results (with and without correlated signals)
 - ✅ Authorization errors
 - ✅ Request errors (e.g. invalid JSON)
 - ✅ Response structure and error response validation
 - ✅ public/priviate ISN visibility
 - ✅ Verifies withdrawn signals are excluded from search results by default
 - ✅ Tests that withdrawn signals can be included when explicitly requested


**Privacy & Security:**
- ✅ Tests that unauthorized users cannot submit or view signals on private ISNs
- ✅ Ensures proper error handling and correctly structured error responses
- ✅ Verifies private ISNs are not accessible via public endpoints
- ✅ Tests CORS configuration prevents unauthorized cross-origin access
- ✅ Middleware authentication and authorization functionality


### 3. Service Account Batch Management (`batch_test.go`)

**What it tests:**
- ✅ Service account batch lifecycle management
- ✅ Batch creation and automatic closure when new batch is created
- ✅ Service account signal submission requirements (must have active batch)
- ✅ Batch validation and error handling

**TODO**
  - **rate limiting integration tests** - unit tests, but actual behaviour is only tested indirectly (see perf tests, which can be set up to trigger the rate limiter)
  - **admin endpoints** - although the auth functionality is tested, there are no end-2-end http tests for the admin endpoints. Test manually when making changes to the handlers
  - **env setup** - each test sets up a fresh database.  This is convenient - because the tests are guaranteed to be isolated - but not very efficient. If the test are too slow then look at starting 1 db and clearing down before each test.

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
- **Local**: Uses the dev Docker Compose PostgreSQL container (port 15432)
- **CI**: Uses a GitHub Actions PostgreSQL service (port 5432)
- Each integration test creates a temporary database (`tmp_signalsd_integration_test`) and applies the migrations so the schema reflects the latest code
-  end-to-end HTTP tests also start the signalsd service on a random available port
- Database and server are cleaned up after each test completes
