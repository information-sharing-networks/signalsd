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
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"

	"github.com/information-sharing-networks/signalsd/app/internal/auth"
	"github.com/information-sharing-networks/signalsd/app/internal/database"
	"github.com/information-sharing-networks/signalsd/app/internal/logger"
	"github.com/information-sharing-networks/signalsd/app/internal/publicisns"
	"github.com/information-sharing-networks/signalsd/app/internal/router"
	"github.com/information-sharing-networks/signalsd/app/internal/schemas"
	"github.com/information-sharing-networks/signalsd/app/internal/server"
	signalsd "github.com/information-sharing-networks/signalsd/app/internal/server/config"
)

const testSecretKey = "test-secret-key-12345"

// testEnv provides access to test db and server for integration tests
type testEnv struct {
	baseURL        string
	cfg            *signalsd.ServerEnvironment
	pool           *pgxpool.Pool
	queries        *database.Queries
	authService    *auth.AuthService
	publicIsnCache *publicisns.Cache
	routerCache    *router.Cache
	schemaCache    *schemas.Cache
}

// startInProcessServer starts the signalsd server in-process for testing.
// A new database is created for each test and the server is configured to use it.
// Server shutdown is registered via t.Cleanup automatically.
func startInProcessServer(t *testing.T, publicBaseURL string) *testEnv {
	t.Helper()

	testEnv := &testEnv{}

	t.Log("Starting in-process server...")

	var (
		ctx         = context.Background()
		port        = findFreePort(t)
		logLevel    = logger.ParseLogLevel("none")
		environment = "test"
	)

	// configure db
	testEnv.pool = setupTestDatabase(t)

	// Build config directly instead of round-tripping through environment variables
	cfg := &signalsd.ServerEnvironment{
		Environment:          environment,
		Host:                 "0.0.0.0",
		Port:                 port,
		SecretKey:            testSecretKey,
		DatabaseURL:          testEnv.pool.Config().ConnString(),
		LogLevel:             logLevel.String(),
		ServiceMode:          "all",
		ReadTimeout:          15 * time.Second,
		WriteTimeout:         15 * time.Second,
		IdleTimeout:          60 * time.Second,
		AllowedOrigins:       []string{"*"},
		MaxSignalPayloadSize: 5242880,
		MaxAPIRequestSize:    65536,
		RateLimitRPS:         2500,
		RateLimitBurst:       5000,
		DBMaxConnections:     4,
		DBMinConnections:     0,
		DBMaxConnLifetime:    60 * time.Minute,
		DBMaxConnIdleTime:    30 * time.Minute,
		DBConnectTimeout:     5 * time.Second,
		PublicBaseURL:        fmt.Sprintf("http://localhost:%d", port),
	}

	// publicBaseURL is only used when generating end user facing links like password reset
	if publicBaseURL != "" {
		cfg.PublicBaseURL = publicBaseURL
	}

	// Allow tests to override allowed origins (e.g. CORS tests)
	if origins := os.Getenv("ALLOWED_ORIGINS"); origins != "" {
		cfg.AllowedOrigins = strings.Split(origins, "|")
	}

	corsConfigs, err := signalsd.CreateCORSConfigs(cfg)
	if err != nil {
		t.Fatalf("Failed to create CORS configs: %v", err)
	}

	testEnv.queries = database.New(testEnv.pool)

	if os.Getenv("ENABLE_SERVER_LOGS") == "true" {
		logLevel = logger.ParseLogLevel("Info")
	}

	testEnv.authService = auth.NewAuthService(testSecretKey, environment, testEnv.queries)

	testEnv.schemaCache = schemas.NewCache(testEnv.queries)
	if err := testEnv.schemaCache.Load(ctx); err != nil {
		t.Logf("Warning: Failed to load schema cache: %v", err)
	}

	testEnv.publicIsnCache = publicisns.NewCache(testEnv.queries)
	if err := testEnv.publicIsnCache.Load(ctx); err != nil {
		t.Logf("Warning: Failed to load public ISN cache: %v", err)
	}

	testEnv.routerCache = router.NewCache(testEnv.queries)
	if err := testEnv.routerCache.Load(ctx); err != nil {
		t.Logf("Warning: Failed to load router cache: %v", err)
	}

	appLogger := logger.InitLogger(logLevel, environment)

	serverInstance := server.NewServer(
		testEnv.pool,
		testEnv.queries,
		testEnv.authService,
		cfg,
		corsConfigs,
		appLogger,
		testEnv.schemaCache,
		testEnv.publicIsnCache,
		testEnv.routerCache,
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

	// Register shutdown via t.Cleanup so callers don't need to remember defer
	t.Cleanup(func() {
		t.Log("Stopping server...")
		serverCancel()

		select {
		case err := <-serverDone:
			if err != nil {
				t.Logf("Server shutdown with error: %v", err)
			} else {
				t.Log("Server shut down")
			}
		case <-time.After(5 * time.Second):
			t.Log("Server shutdown timeout")
		}

		serverInstance.DatabaseShutdown()
	})

	testEnv.baseURL = fmt.Sprintf("http://localhost:%d", cfg.Port)
	testEnv.cfg = cfg

	// Wait for server to be ready
	if !waitForServer(t, testEnv.baseURL+"/health/live", 30*time.Second) {
		t.Fatal("Server failed to start within timeout")
	}

	t.Logf("Server started at %s", testEnv.baseURL)
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

// setupTestDatabase creates an empty test db, applies migrations and returns a connection pool.
// The function auto-detects if it is running in CI (github actions) and uses the appropriate database config.
func setupTestDatabase(t *testing.T) *pgxpool.Pool {
	ctx := context.Background()

	config := &databaseConfig{host: "localhost", user: "signalsd-dev", port: 15432}
	if os.Getenv("GITHUB_ACTIONS") == "true" {
		config = &databaseConfig{host: "localhost", user: "postgres:postgres", port: 5432}
	}

	// Generate a unique test database name
	testDbName := fmt.Sprintf("test_%d", time.Now().UnixMilli())

	// Connect to the postgres database to create the test database
	postgresConnectionURL := (&databaseConfig{user: config.user, host: config.host, port: config.port, dbname: "postgres"}).connectionURL()

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
	testDatabaseURL := (&databaseConfig{user: config.user, host: config.host, port: config.port, dbname: testDbName}).connectionURL()
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
