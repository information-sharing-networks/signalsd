//go:build integration

package integration

// test environment setup and server lifecycle management.
//
// the end-2-end tests start the signalsd http server with a temporary database and runs tests against it.
// Each test creates an empty temporary database and applies all the migrations so the schema reflects the latest code. The database is dropped after each test.
//
// By default the server logs are not included in the test output, you can enable them with:
//
//	ENABLE_SERVER_LOGS=true go test -tags=integration -v ./test/integration
//
import (
	"context"
	"database/sql"
	"fmt"
	"net"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"

	"github.com/information-sharing-networks/signalsd/app/internal/auth"
	"github.com/information-sharing-networks/signalsd/app/internal/database"
	"github.com/information-sharing-networks/signalsd/app/internal/logger"
	"github.com/information-sharing-networks/signalsd/app/internal/server"
	signalsd "github.com/information-sharing-networks/signalsd/app/internal/server/config"
	"github.com/information-sharing-networks/signalsd/app/internal/server/isns"
	"github.com/information-sharing-networks/signalsd/app/internal/server/schemas"
)

// testEnv provides access to test db and server for integration tests
type testEnv struct {
	baseURL     string
	pool        *pgxpool.Pool
	queries     *database.Queries
	authService *auth.AuthService
	shutdown    func()
}

type databaseConfig struct {
	userAndPassword string
	dbname          string
	host            string
	port            int
}

func (d *databaseConfig) connectionURL() string {
	return fmt.Sprintf("postgres://%s@%s:%d/%s?sslmode=disable",
		d.userAndPassword, d.host, d.port, d.dbname)
}

func (d *databaseConfig) WithDatabase(dbname string) *databaseConfig {
	return &databaseConfig{
		userAndPassword: d.userAndPassword,
		host:            d.host,
		port:            d.port,
		dbname:          dbname,
	}
}

func localDatabaseConfig() *databaseConfig {
	return &databaseConfig{
		userAndPassword: "signalsd-dev:",
		dbname:          "tmp_signalsd_integration_test",
		host:            "localhost",
		port:            15432,
	}
}

func ciDatabaseConfig() *databaseConfig {
	return &databaseConfig{
		userAndPassword: "postgres:postgres",
		dbname:          "tmp_signalsd_integration_test",
		host:            "localhost",
		port:            5432,
	}
}

var testServerConfig = struct {
	secretKey   string
	environment string
	logLevel    string
}{
	secretKey:   "test-secret-key-12345",
	environment: "test",
	logLevel:    "debug",
}

const (

	// Test signal type
	testSignalTypeDetail = "Simple test signal type for integration tests"
	testSchemaURL        = "https://github.com/information-sharing-networks/signalsd_test_schemas/blob/main/2025.05.13/integration-test-schema.json"
	testReadmeURL        = "https://github.com/information-sharing-networks/signalsd_test_schemas/blob/main/2025.05.13/README.md"
	testSchemaContent    = `{"type": "object", "properties": {"test": {"type": "string"}}, "required": ["test"], "additionalProperties": false }`
)

// setupTestDatabase creates an empty test db, applies migrations and returns a connection pool
// the function auto-detects if it is running in CI (github actions) and uses the appropriate database config
func setupTestDatabase(t *testing.T) *pgxpool.Pool {

	ctx := context.Background()
	var config *databaseConfig

	if os.Getenv("GITHUB_ACTIONS") == "true" {
		config = ciDatabaseConfig()
	} else {
		config = localDatabaseConfig()
	}

	postgresConfig := config.WithDatabase("postgres")

	// connect to the postgres database to create the test database
	postgresConnectionURL := postgresConfig.connectionURL()

	postgresPoolConfig, err := pgxpool.ParseConfig(postgresConnectionURL)
	if err != nil {
		t.Fatalf("Failed to parse postgres database URL: %v", err)
	}

	postgresPool, err := pgxpool.NewWithConfig(ctx, postgresPoolConfig)
	if err != nil {
		t.Fatalf("Unable to create postgres connection pool: %v", err)
	}

	if err := postgresPool.Ping(ctx); err != nil {
		t.Fatalf("Can't ping PostgreSQL server %s", postgresConnectionURL)
	}

	_, err = postgresPool.Exec(ctx, "DROP DATABASE IF EXISTS "+config.dbname)
	if err != nil {
		t.Fatalf("DROP DATABASE IF EXISTS Failed : %v", err)
	}

	_, err = postgresPool.Exec(ctx, "CREATE DATABASE "+config.dbname)
	if err != nil {
		t.Fatalf("CREATE DATABASE Failed : %v", err)
	}

	// Close the postgres pool
	t.Cleanup(func() {
		postgresPool.Close()
	})

	// drop the test database when the test is complete
	t.Cleanup(func() {
		_, err := postgresPool.Exec(ctx, "DROP DATABASE "+config.dbname)
		if err != nil {
			t.Fatalf("Failed to drop test database: %v", err)
		}
	})

	// connect to the new database
	testDatabaseURL := config.connectionURL()
	testDatabasePool := setupDatabaseConn(t, testDatabaseURL)

	// Apply database migrations
	if err := runDatabaseMigrations(t, testDatabasePool); err != nil {
		t.Fatalf("Failed to apply database migrations: %v", err)
	}
	// Convert pgx pool to database/sql interface that Goose expects
	var db *sql.DB = stdlib.OpenDBFromPool(testDatabasePool)
	defer db.Close()

	if err := goose.SetDialect("postgres"); err != nil {
		t.Fatalf("failed to set goose dialect: %v", err)
	}

	// Apply migrations from the sql/schema directory
	migrationDir := "../../sql/schema"
	if err := goose.Up(db, migrationDir); err != nil {
		t.Fatalf("failed to apply migrations: %v", err)
	}

	t.Logf("Database ready: %s", config.dbname)

	return testDatabasePool
}

func setupDatabaseConn(t *testing.T, databaseURL string) *pgxpool.Pool {
	t.Helper()

	ctx := context.Background()
	poolConfig, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		t.Fatalf("Failed to parse database URL: %v", err)
	}

	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		t.Fatalf("Unable to create connection pool: %v", err)
	}

	t.Cleanup(func() {
		pool.Close()
	})

	return pool
}

// runDatabaseMigrations applies all pending Goose migrations to the test database
func runDatabaseMigrations(t *testing.T, pool *pgxpool.Pool) error {
	t.Helper()

	// Convert pgx pool to database/sql interface that Goose expects
	var db *sql.DB = stdlib.OpenDBFromPool(pool)
	defer db.Close()

	if err := goose.SetDialect("postgres"); err != nil {
		return fmt.Errorf("failed to set goose dialect: %w", err)
	}

	// Apply migrations from the sql/schema directory
	migrationDir := "../../sql/schema"
	if err := goose.Up(db, migrationDir); err != nil {
		return fmt.Errorf("failed to apply migrations: %w", err)
	}

	return nil
}

// startInProcessServer starts the signalsd server in-process for testing - returns the testEnv with base URL and shutdown function
// when using public isns be sure to load the test data before starting the server (the public isn cache is not dynamic and is only populated at startup)
// if you are not testing a function that generates end user facing urls then send an empty string for publicBaseURL
//
// For tests that need to create test data before starting the server (e.g., to populate public ISN cache),
// use setupTestDatabaseAndEnv instead, create your test data, then call startServerWithEnv.
func startInProcessServer(t *testing.T, publicBaseURL string) *testEnv {
	t.Helper()

	testEnv := &testEnv{}

	t.Log("Starting in-process server...")

	// server config
	var (
		ctx       = context.Background()
		port      = findFreePort(t)
		logLevel  = logger.ParseLogLevel("none")
		testDB    = setupTestDatabase(t)
		testDBURL = testDB.Config().ConnString()
	)

	if os.Getenv("ENABLE_SERVER_LOGS") == "true" {
		logLevel = logger.ParseLogLevel("debug")
	}

	// Set environment variables before calling NewServerConfig
	testEnvVars := map[string]string{
		"SECRET_KEY":   testServerConfig.secretKey,
		"DATABASE_URL": testDBURL,
		"ENVIRONMENT":  testServerConfig.environment,
		"LOG_LEVEL":    testServerConfig.logLevel,
		"PORT":         fmt.Sprintf("%d", port),
	}

	// Save original env vars and set test values
	originalEnvVars := make(map[string]string)
	for key, value := range testEnvVars {
		originalEnvVars[key] = os.Getenv(key)
		os.Setenv(key, value)
	}

	// Restore original environment variables when test completes
	t.Cleanup(func() {
		for key, original := range originalEnvVars {
			if original != "" {
				os.Setenv(key, original)
			} else {
				os.Unsetenv(key)
			}
		}
	})

	cfg, corsConfigs, err := signalsd.NewServerConfig()
	if err != nil {
		t.Fatalf("Failed to load configuration: %v", err)
	}

	cfg.ServiceMode = "all"

	// publicBaseURL is only used when generating end user facing links like password reset - default to the test env url
	if publicBaseURL != "" {
		cfg.PublicBaseURL = publicBaseURL
	}

	testEnv.pool = testDB
	testEnv.queries = database.New(testDB)
	testEnv.authService = auth.NewAuthService(cfg.SecretKey, testServerConfig.environment, testEnv.queries)

	schemaCache := schemas.NewSchemaCache()
	if err := schemaCache.Load(ctx, testEnv.queries); err != nil {
		t.Logf("Warning: Failed to load schema cache: %v", err)
	}

	publicIsnCache := isns.NewPublicIsnCache()
	if err := publicIsnCache.Load(ctx, testEnv.queries); err != nil {
		t.Logf("Warning: Failed to load public ISN cache: %v", err)
	}

	appLogger := logger.InitLogger(logLevel, testServerConfig.environment)

	serverInstance := server.NewServer(
		testDB,
		testEnv.queries,
		testEnv.authService,
		cfg,
		corsConfigs,
		appLogger,
		schemaCache,
		publicIsnCache,
	)

	// Create a cancellable context for server shutdown
	serverCtx, serverCancel := context.WithCancel(ctx)

	// Start server
	serverDone := make(chan error, 1)
	go func() {
		defer close(serverDone)
		if err := serverInstance.Start(serverCtx); err != nil {
			serverDone <- err
		}
	}()

	// Create shutdown function to be called by the test
	testEnv.shutdown = func() {
		t.Log("Stopping server...")

		// Cancel the server context to trigger graceful shutdown
		serverCancel()

		// Wait for server to shut down gracefully with timeout
		select {
		case err := <-serverDone:
			if err != nil {
				t.Logf("❌ Server shutdown with error: %v", err)
			} else {
				t.Log("✅ Server shut down gracefully")
			}
		case <-time.After(5 * time.Second):
			t.Log("⚠️ Server shutdown timeout")
		}

		// Ensure database connections are closed
		serverInstance.DatabaseShutdown()
	}

	testEnv.baseURL = fmt.Sprintf("http://localhost:%d", cfg.Port)
	t.Logf("Starting in-process server at %s", testEnv.baseURL)

	// Wait for server to be ready
	if !waitForServer(t, testEnv.baseURL+"/health/live", 30*time.Second) {
		t.Fatal("Server failed to start within timeout")
	}

	// Test the server is working
	resp, err := http.Get(testEnv.baseURL + "/health/live")
	if err != nil {
		t.Fatalf("Failed to call health endpoint: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}

	t.Log("✅ Server started")
	return testEnv
}

// startServerWithEnv starts the signalsd server with an existing testEnv
// Use this after setupTestDatabaseAndEnv when you need to create test data before starting the server
func startServerWithEnv(t *testing.T, testEnv *testEnv) {
	t.Helper()

	ctx := context.Background()
	port := findFreePort(t)
	logLevel := logger.ParseLogLevel("none")

	if os.Getenv("ENABLE_SERVER_LOGS") == "true" {
		logLevel = logger.ParseLogLevel("debug")
	}

	testDBURL := testEnv.pool.Config().ConnString()

	// Set environment variables before calling NewServerConfig
	testEnvVars := map[string]string{
		"SECRET_KEY":   testServerConfig.secretKey,
		"DATABASE_URL": testDBURL,
		"ENVIRONMENT":  testServerConfig.environment,
		"LOG_LEVEL":    testServerConfig.logLevel,
		"PORT":         fmt.Sprintf("%d", port),
	}

	// Save original env vars and set test values
	originalEnvVars := make(map[string]string)
	for key, value := range testEnvVars {
		originalEnvVars[key] = os.Getenv(key)
		os.Setenv(key, value)
	}

	// Restore original environment variables when test completes
	t.Cleanup(func() {
		for key, original := range originalEnvVars {
			if original != "" {
				os.Setenv(key, original)
			} else {
				os.Unsetenv(key)
			}
		}
	})

	cfg, corsConfigs, err := signalsd.NewServerConfig()
	if err != nil {
		t.Fatalf("Failed to load configuration: %v", err)
	}

	cfg.ServiceMode = "all"

	schemaCache := schemas.NewSchemaCache()
	if err := schemaCache.Load(ctx, testEnv.queries); err != nil {
		t.Logf("Warning: Failed to load schema cache: %v", err)
	}

	publicIsnCache := isns.NewPublicIsnCache()
	if err := publicIsnCache.Load(ctx, testEnv.queries); err != nil {
		t.Logf("Warning: Failed to load public ISN cache: %v", err)
	}

	appLogger := logger.InitLogger(logLevel, testServerConfig.environment)

	serverInstance := server.NewServer(
		testEnv.pool,
		testEnv.queries,
		testEnv.authService,
		cfg,
		corsConfigs,
		appLogger,
		schemaCache,
		publicIsnCache,
	)

	// Create a cancellable context for server shutdown
	serverCtx, serverCancel := context.WithCancel(ctx)

	// Start server
	serverDone := make(chan error, 1)
	go func() {
		defer close(serverDone)
		if err := serverInstance.Start(serverCtx); err != nil {
			serverDone <- err
		}
	}()

	// Create shutdown function to be called by the test
	// Wrap any existing shutdown function (from setupTestDatabaseAndEnv)
	oldShutdown := testEnv.shutdown
	testEnv.shutdown = func() {
		t.Log("Stopping server...")

		// Cancel the server context to trigger graceful shutdown
		serverCancel()

		// Wait for server to shut down gracefully with timeout
		select {
		case err := <-serverDone:
			if err != nil {
				t.Logf("❌ Server shutdown with error: %v", err)
			} else {
				t.Log("✅ Server shut down gracefully")
			}
		case <-time.After(5 * time.Second):
			t.Log("⚠️ Server shutdown timeout")
		}

		// Ensure database connections are closed
		serverInstance.DatabaseShutdown()

		// Call the old shutdown function if it exists (for cleanup from setupTestDatabaseAndEnv)
		if oldShutdown != nil {
			oldShutdown()
		}
	}

	testEnv.baseURL = fmt.Sprintf("http://localhost:%d", cfg.Port)
	t.Logf("Starting in-process server at %s", testEnv.baseURL)

	// Wait for server to be ready
	if !waitForServer(t, testEnv.baseURL+"/health/live", 30*time.Second) {
		t.Fatal("Server failed to start within timeout")
	}

	// Test the server is working
	resp, err := http.Get(testEnv.baseURL + "/health/live")
	if err != nil {
		t.Fatalf("Failed to call health endpoint: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}

	t.Log("✅ Server started")
}

func findFreePort(t *testing.T) int {
	t.Helper()
	listener, err := net.Listen("tcp", ":0")
	if err != nil {
		t.Fatalf("Failed to find free port: %v", err)
	}
	defer listener.Close()

	addr := listener.Addr().(*net.TCPAddr)
	return addr.Port
}

func waitForServer(t *testing.T, url string, timeout time.Duration) bool {
	t.Helper()

	client := &http.Client{Timeout: 1 * time.Second}
	deadline := time.Now().Add(timeout)

	for time.Now().Before(deadline) {
		resp, err := client.Get(url)
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return true
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	return false
}

// createExpiredAccessToken creates an expired JWT access token for testing purposes
func createExpiredAccessToken(t *testing.T, accountID uuid.UUID) string {
	t.Helper()

	// Create JWT claims with expired timestamp
	issuedAt := time.Now().Add(-2 * time.Hour)  // 2 hours ago
	expiresAt := time.Now().Add(-1 * time.Hour) // 1 hour ago (expired)

	claims := auth.Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   accountID.String(),
			IssuedAt:  jwt.NewNumericDate(issuedAt),
			ExpiresAt: jwt.NewNumericDate(expiresAt),
			Issuer:    signalsd.TokenIssuerName,
		},
		AccountID:   accountID,
		AccountType: "user",
		Role:        "member",
		IsnPerms:    make(map[string]auth.IsnPerms),
	}

	// Create and sign the token using the same secret key as the auth service
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signedToken, err := token.SignedString([]byte(testServerConfig.secretKey))
	if err != nil {
		t.Fatalf("Failed to create expired access token: %v", err)
	}

	return signedToken
}

// setupTestDatabaseAndEnv creates a test database and environment without starting the server
// This is useful for tests that need to create test data before starting the server
// (e.g., to populate public ISN cache which is only populated at startup)
// After creating test data, call startServerWithEnv to start the server
func setupTestDatabaseAndEnv(t *testing.T) *testEnv {
	t.Helper()

	testDB := setupTestDatabase(t)

	testEnv := &testEnv{
		pool:        testDB,
		queries:     database.New(testDB),
		authService: auth.NewAuthService(testServerConfig.secretKey, testServerConfig.environment, database.New(testDB)),
		shutdown: func() {
			// Default shutdown just closes the database pool
			// This will be replaced by startServerWithEnv if the server is started
			testDB.Close()
		},
	}

	return testEnv
}
