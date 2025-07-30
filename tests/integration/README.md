# Integration Tests

## Overview

Integration tests for the authentication system.

### Prerequisites
The script uses the Docker development setup (see the main README for the project). The app and db containers are already configured with the dependencies needed to run these tests.
The script creates a temporary test database to run the tests

to start the app, use:
```sh
docker compose up 
```

### Usage

```bash
cd tests/integration

# Run all integration tests
./integration-tests.sh -r all

# Run a specific test and drop the test database on exit
./integration-tests.sh -r TestAccessTokenIntegration -d

# Skip database setup (faster for repeated runs)
./integration-tests.sh -s -r all

# Show usage statement
./integration-tests.sh -h
```


### Integration with Docker

The script works with your existing Docker setup (see the quickstart section of the main README for the project).  The app and db containers are already configured with the dependencies needed to run these tests.

## Test Structure

Integration tests are in `app/integration_test.go` and follow the pattern:

1. Connect to pre-created test database
2. Set up test data (accounts, ISNs, permissions)
3. Test complete authentication flows
4. Verify database state and JWT tokens
5. Clean up connections (database cleanup handled by script)


## HTTP Integration Tests

HTTP integration tests make real HTTP requests to a running signalsd Docker container, testing the complete end-to-end signal submission pipeline.

- **Complete signal submission pipeline** via real HTTP requests
- **Authentication and authorization** with JWT tokens
- **JSON schema validation** against GitHub-hosted schemas
- **Database persistence** verification
- **Error handling** for various failure scenarios
- **Batch processing** with multiple signals

### Test Scenarios

1. **Successful signal submission** - Valid payload, proper authentication
2. **Authentication failure** - Invalid/missing JWT tokens
3. **Schema validation failure** - Invalid signal content
4. **Multiple signals batch** - Batch processing verification
5. **Missing required fields** - Malformed request handling
6. **Empty signals array** - Edge case validation

### Environment Variables

The HTTP test script sets these environment variables:

- `TEST_DATABASE_URL` - Database connection for verification
- `TEST_HTTP_BASE_URL` - Target signalsd instance URL
