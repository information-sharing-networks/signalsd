# Signalsd Testing 

## Integration Tests

The integration tests are designed to ensure that signal data is handled correctly and that privacy controls work as intended.

**integration test files `app/test/integration/`:**
- `setup_test_env.go` - Test environment setup and server lifecycle management
- `auth_test.go` - Authentication and authorization system testing
- `http_test.go` - HTTP API endpoint testing with full request/response validation (signal submission & search) + CORS security
- `database.go` - Shared database test helpers

## Unit Tests 

Unit tests are used to test a couple of areas:
- `app/internal/server/utils/utils_test.go` - URL validation and SSRF protection (ensures user submitted URLs are only GitHub URLs)
- `app/internal/server/request_limits_test.go` - Rate limiting and request size controls


### 1. Authentication & Authorization (`auth_test.go`)

**What it tests:**

- ✅ Database queries work correctly
- ✅ JWT token structure - tokens are properly signed and parseable
- ✅ JWT claims (ISN permissions, role etc) match database 
- ✅ Token metadata - expiration, issued time, issuer, subject are correct
- ✅ Role-based permissions - owner gets write to all ISNs, admin gets write to own ISNs
- ✅ Explicit permission grants - member gets read access where granted
- ✅ Service account batch handling - service accounts require batch IDs for write permissions
- ✅ Signal type paths - correct signal type paths are included in permissions


### 2. HTTP API Testing (`http_test.go`)

**What it tests:**
- ✅ Signal submission pipeline end-to-end
- ✅ Signal search functionality with authorization controls
- ✅ Schema validation and data integrity (includes test schema retrieval from GitHub)
- ✅ Response structure and error response validation
- ✅ Authentication failures and authorization errors
- ✅ Batch processing with mixed success/failure scenarios
- ✅ CORS security configuration and enforcement

**Privacy & Security Coverage:**
- ✅ Tests that unauthorized users cannot submit signals
- ✅ Ensures proper error handling error responses
- ✅ Verifies private ISNs are not accessible via public endpoints 
- ✅ Tests CORS configuration prevents unauthorized cross-origin access


**TODO**
  - **rate limiting integration tests** - only unit tests exist
  - **withdrawn signal privacy** - no tests ensure withdrawn signals don't leak data
  - **correlation ID handling** - no tests ensure correlation IDs don't leak across permission boundaries
  - **admin endpoints** - currently there are no automated tests for the admin endpoints. Test manually when making changes to the handlers

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
