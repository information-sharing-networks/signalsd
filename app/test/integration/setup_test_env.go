//go:build integration

package integration

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"syscall"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"github.com/rs/zerolog"

	signalsd "github.com/information-sharing-networks/signalsd/app"
	"github.com/information-sharing-networks/signalsd/app/internal/auth"
	"github.com/information-sharing-networks/signalsd/app/internal/database"
	"github.com/information-sharing-networks/signalsd/app/internal/logger"
	"github.com/information-sharing-networks/signalsd/app/internal/server"
	"github.com/information-sharing-networks/signalsd/app/internal/server/isns"
	"github.com/information-sharing-networks/signalsd/app/internal/server/schemas"
)

// docker container db url
const postgresDatabaseURL = "postgres://signalsd-dev:@localhost:15432/postgres?sslmode=disable"

// test database name and url (this temporary db be created in the existing docker container)
const testDatabaseName = "tmp_signalsd_integration_test"
const testDatabaseURL = "postgres://signalsd-dev:@localhost:15432/tmp_signalsd_integration_test?sslmode=disable"
const secretKey = "test-secret"
const environment = "test"

// createHttpLogger creates a logger that only logs errors - used to suppress request logs during tests
// Set ENABLE_SERVER_LOGS=true environment variable to enable full HTTP request/response logging for debugging
func createHttpLogger() *zerolog.Logger {
	if os.Getenv("ENABLE_SERVER_LOGS") == "true" {
		logger := zerolog.New(zerolog.ConsoleWriter{Out: os.Stderr}).
			Level(zerolog.DebugLevel).
			With().
			Timestamp().
			Logger()
		return &logger
	}

	// Default: only log errors to reduce test noise
	logger := zerolog.New(io.Discard).
		Level(zerolog.ErrorLevel).
		With().
		Timestamp().
		Logger()
	return &logger
}

// setupTestDatabase sets up a local integration test environment:
// - creates a temporary database in the existing docker db container
// - applies database migrations
// - drops the database on exit
func setupTestDatabase(t *testing.T, ctx context.Context) *pgxpool.Pool {

	t.Log("Running local integration test")

	// Check PostgreSQL server connectivity
	postgresDB := setupDatabaseConn(t, postgresDatabaseURL)
	if err := postgresDB.Ping(ctx); err != nil {
		t.Fatalf("❌ Can't ping PostgreSQL server %s - is the docker db container running?", postgresDatabaseURL)
	}

	// create the test database
	t.Logf("setting up test database %v", testDatabaseURL)
	createTestDatabase(t, ctx, postgresDB, testDatabaseName)

	testDB := setupDatabaseConn(t, testDatabaseURL)

	t.Log("✅ test database created")

	// Apply database migrations
	if err := runDatabaseMigrations(t, testDB); err != nil {
		t.Fatalf("❌ Failed to apply database migrations: %v", err)
	}

	t.Log("✅ Database migrations applied successfully")

	return testDB

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

	t.Log("Database pool created")
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

func createTestDatabase(t *testing.T, ctx context.Context, pool *pgxpool.Pool, databaseName string) {
	t.Helper()

	// try to drop db in case previous run was killed and the test db still exists
	_, err := pool.Exec(ctx, "DROP DATABASE IF EXISTS "+databaseName)
	if err != nil {
		t.Fatalf("DROP DATABASE IF EXISTS Failed : %v", err)
	}

	_, err = pool.Exec(ctx, "CREATE DATABASE "+databaseName)
	if err != nil {
		t.Fatalf("CREATE DATABASE Failed : %v", err)
	}

	t.Cleanup(func() {
		_, err := pool.Exec(ctx, "DROP DATABASE "+databaseName)
		if err != nil {
			t.Fatalf("Failed to drop test database: %v",
				err)
		}
	})

	t.Log("Test database created")
}

// startInProcessServer starts the signalsd server in-process for testing - returns the base URL for the API and a shutdown function
// be sure to load the test data before starting the server so the caches (below) are populated
func startInProcessServer(t *testing.T, ctx context.Context, testDB *pgxpool.Pool, testDatabaseURL string) (string, func()) {

	// Set environment variables before calling NewServerConfig
	originalEnvVars := make(map[string]string)
	testEnvVars := map[string]string{
		"SECRET_KEY":   secretKey,
		"DATABASE_URL": testDatabaseURL,
		"ENVIRONMENT":  environment,
		"LOG_LEVEL":    "debug",
		"PORT":         fmt.Sprintf("%d", findFreePort(t)),
	}

	// Save original values and set test values
	for key, value := range testEnvVars {
		if original := os.Getenv(key); original != "" {
			originalEnvVars[key] = original
		}
		os.Setenv(key, value)
	}

	// Restore original environment variables when test completes
	defer func() {
		for key := range testEnvVars {
			if original, exists := originalEnvVars[key]; exists {
				os.Setenv(key, original)
			} else {
				os.Unsetenv(key)
			}
		}
	}()

	serverLogger := logger.InitServerLogger()

	cfg, corsConfigs, err := signalsd.NewServerConfig(serverLogger)
	if err != nil {
		t.Fatalf("Failed to load configuration: %v", err)
	}

	cfg.ServiceMode = "all"

	httpLogger := createHttpLogger()

	queries := database.New(testDB)
	authService := auth.NewAuthService(cfg.SecretKey, environment, queries)

	schemaCache := schemas.NewSchemaCache()
	if err := schemaCache.Load(ctx, queries); err != nil {
		t.Logf("Warning: Failed to load schema cache: %v", err)
	}

	publicIsnCache := isns.NewPublicIsnCache()
	if err := publicIsnCache.Load(ctx, queries); err != nil {
		t.Logf("Warning: Failed to load public ISN cache: %v", err)
	}

	serverInstance := server.NewServer(
		testDB,
		queries,
		authService,
		cfg,
		corsConfigs,
		serverLogger,
		httpLogger,
		schemaCache,
		publicIsnCache,
	)

	// Start server
	serverDone := make(chan struct{})
	go func() {
		defer close(serverDone)
		serverInstance.Run() // This will block until SIGINT/SIGTERM
	}()

	// Create shutdown function to be called by the test
	shutdownFunc := func() {
		t.Log("Stopping server...")

		if err := syscall.Kill(syscall.Getpid(), syscall.SIGINT); err != nil {
			t.Logf("Warning: Failed to send SIGINT: %v", err)
		}

		// Wait for server to shut down gracefully with timeout
		select {
		case <-serverDone:
			t.Log("✅ Server shut down")
		case <-time.After(10 * time.Second):
			t.Log("⚠️ Server shutdown timeout")
		}
	}

	baseURL := fmt.Sprintf("http://localhost:%d", cfg.Port)
	t.Logf("Starting in-process server at %s", baseURL)

	// Wait for server to be ready
	if !waitForServer(t, baseURL+"/health/live", 30*time.Second) {
		t.Fatal("Server failed to start within timeout")
	}

	// Test the server is working
	resp, err := http.Get(baseURL + "/health/live")
	if err != nil {
		t.Fatalf("Failed to call health endpoint: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}

	return baseURL, shutdownFunc
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
