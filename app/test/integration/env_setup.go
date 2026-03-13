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
	baseURL        string
	cfg            *signalsd.ServerEnvironment
	pool           *pgxpool.Pool
	queries        *database.Queries
	authService    *auth.AuthService
	publicIsnCache *isns.PublicIsnCache
	shutdown       func()
}

// startInProcessServer starts the signalsd server in-process for testing - returns the base URL for the API and a shutdown function
// a new database is created for each test and the server is configured to use it.
func startInProcessServer(t *testing.T, publicBaseURL string) *testEnv {
	t.Helper()

	testEnv := &testEnv{}

	t.Log("Starting in-process server...")

	// server config
	var (
		ctx         = context.Background()
		port        = findFreePort(t)
		logLevel    = logger.ParseLogLevel("none")
		environment = "test"
		secretKey   = "test-secret-key-12345"
	)

	// configure db
	testEnv.pool = setupTestDatabase(t)
	testDatabaseURL := testEnv.pool.Config().ConnString()

	// Set environment variables before calling NewServerConfig
	testEnvVars := map[string]string{
		"SECRET_KEY":   secretKey,
		"DATABASE_URL": testDatabaseURL,
		"ENVIRONMENT":  environment,
		"LOG_LEVEL":    logLevel.String(),
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

	testEnv.queries = database.New(testEnv.pool)

	if os.Getenv("ENABLE_SERVER_LOGS") == "true" {
		logLevel = logger.ParseLogLevel("Info")
	}

	testEnv.authService = auth.NewAuthService(secretKey, environment, testEnv.queries)

	schemaCache := schemas.NewSchemaCache()
	if err := schemaCache.Load(ctx, testEnv.queries); err != nil {
		t.Logf("Warning: Failed to load schema cache: %v", err)
	}

	testEnv.publicIsnCache = isns.NewPublicIsnCache()
	if err := testEnv.publicIsnCache.Load(ctx, testEnv.queries); err != nil {
		t.Logf("Warning: Failed to load public ISN cache: %v", err)
	}

	appLogger := logger.InitLogger(logLevel, environment)

	serverInstance := server.NewServer(
		testEnv.pool,
		testEnv.queries,
		testEnv.authService,
		cfg,
		corsConfigs,
		appLogger,
		schemaCache,
		testEnv.publicIsnCache,
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
				t.Log("✅ Server shut down")
			}
		case <-time.After(5 * time.Second):
			t.Log("⚠️ Server shutdown timeout")
		}

		// Ensure database connections are closed
		serverInstance.DatabaseShutdown()
	}

	testEnv.baseURL = fmt.Sprintf("http://localhost:%d", cfg.Port)

	testEnv.cfg = cfg

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

	t.Logf("✅ Server started at %s", testEnv.baseURL)
	return testEnv
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

type databaseConfig struct {
	user   string
	dbname string
	host   string
	port   int
}

func (d *databaseConfig) connectionURL() string {
	return fmt.Sprintf("postgres://%s@%s:%d/%s?sslmode=disable",
		d.user, d.host, d.port, d.dbname)
}

func (d *databaseConfig) WithDbname(dbname string) *databaseConfig {
	return &databaseConfig{
		user:   d.user,
		host:   d.host,
		port:   d.port,
		dbname: dbname,
	}
}

func (d *databaseConfig) WithPort(port int) *databaseConfig {
	return &databaseConfig{
		user:   d.user,
		host:   d.host,
		port:   port,
		dbname: d.dbname,
	}
}

func (d *databaseConfig) WithUser(user string) *databaseConfig {
	return &databaseConfig{
		user:   user,
		host:   d.host,
		port:   d.port,
		dbname: d.dbname,
	}
}

// setupTestDatabase creates an empty test db, applies migrations and returns a connection pool
// the function auto-detetcs if it is running in CI (github actions) and uses the appropriate database config
func setupTestDatabase(t *testing.T) *pgxpool.Pool {
	ctx := context.Background()
	config := &databaseConfig{
		host: "localhost",
	}
	if os.Getenv("GITHUB_ACTIONS") == "true" {
		config = config.WithUser("postgres:postgres").
			WithPort(5432)
	} else {
		config = config.WithUser("signalsd-dev").
			WithPort(15432)
	}

	// Generate a unique test database name
	testDbName := fmt.Sprintf("test_%d", time.Now().UnixMilli())

	// Connect to the postgres database to create the test database
	postgresConfig := config.WithDbname("postgres")
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
		t.Fatalf("Can't ping PostgreSQL server %s - did you start the db container?", postgresConnectionURL)
	}

	_, err = postgresPool.Exec(ctx, "CREATE DATABASE "+testDbName)
	if err != nil {
		t.Fatalf("CREATE DATABASE failed: %v", err)
	}
	t.Cleanup(func() {
		postgresPool.Close()
	})

	t.Cleanup(func() {
		_, err := postgresPool.Exec(ctx, "DROP DATABASE "+testDbName+" WITH (FORCE)")
		if err != nil {
			t.Fatalf("Failed to drop test database: %v", err)
		}
		t.Logf("Dropped database: %s", testDbName)
	})

	// Connect to the new test database
	testDbConfig := config.WithDbname(testDbName)
	testDatabaseURL := testDbConfig.connectionURL()
	testDatabasePool := setupDatabaseConn(t, testDatabaseURL)

	// Apply database migrations
	if err := runDatabaseMigrations(t, testDatabasePool); err != nil {
		t.Fatalf("Failed to apply database migrations: %v", err)
	}
	t.Logf("Database ready: %s", testDbName)

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
