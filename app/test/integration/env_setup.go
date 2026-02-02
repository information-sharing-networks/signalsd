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

// testEnvironment holds common test dependencies
type testEnvironment struct {
	dbConn      *pgxpool.Pool
	queries     *database.Queries
	authService *auth.AuthService
}

type databaseConfig struct {
	userAndPassword string
	password        string
	dbname          string
	host            string
	port            int
}

type serverConfig struct {
	secretKey   string
	environment string
	logLevel    string
}

var (
	ciDatabaseConfig = databaseConfig{
		userAndPassword: "postgres:postgres",
		dbname:          "tmp_signalsd_integration_test",
		host:            "localhost",
		port:            5432,
	}
	localDatabaseConfig = databaseConfig{
		userAndPassword: "signalsd-dev:",
		dbname:          "tmp_signalsd_integration_test",
		host:            "localhost",
		port:            15432,
	}

	testServerConfig = serverConfig{
		secretKey:   "test-secret-key-12345",
		environment: "test",
		logLevel:    "debug",
	}
)

const (

	// Test signal type
	testSignalTypeDetail = "Simple test signal type for integration tests"
	testSchemaURL        = "https://github.com/information-sharing-networks/signalsd_test_schemas/blob/main/2025.05.13/integration-test-schema.json"
	testReadmeURL        = "https://github.com/information-sharing-networks/signalsd_test_schemas/blob/main/2025.05.13/README.md"
	testSchemaContent    = `{"type": "object", "properties": {"test": {"type": "string"}}, "required": ["test"], "additionalProperties": false }`
)

func buildConnString(userAndPassword string, host string, port int, dbname string) string {
	return fmt.Sprintf("postgres://%s@%s:%d/%s?sslmode=disable",
		userAndPassword, host, port, dbname)
}

// setupTestEnvironment creates a new test environment with database connection and services
func setupTestEnvironment(dbConn *pgxpool.Pool) *testEnvironment {
	queries := database.New(dbConn)
	authService := auth.NewAuthService(testServerConfig.secretKey, testServerConfig.environment, queries)
	return &testEnvironment{
		dbConn:      dbConn,
		queries:     queries,
		authService: authService,
	}
}

// getDatabaseURL returns the appropriate test database URL for the local docker db when running locally
// or the CI test database when being run in github action
func getDatabaseURL() string {
	if os.Getenv("GITHUB_ACTIONS") == "true" {
		return buildConnString(ciDatabaseConfig.userAndPassword, ciDatabaseConfig.host, ciDatabaseConfig.port, ciDatabaseConfig.dbname)
	}
	return buildConnString(localDatabaseConfig.userAndPassword, localDatabaseConfig.host, localDatabaseConfig.port, localDatabaseConfig.dbname)
}

// setupCleanDatabase creates an empty test db, applies migrations and returns a connection pool
func setupCleanDatabase(t *testing.T, ctx context.Context) *pgxpool.Pool {

	config := localDatabaseConfig

	ci := false
	if os.Getenv("GITHUB_ACTIONS") == "true" {
		config = ciDatabaseConfig
		ci = true
	}

	// postgres://postgres:postgres@localhost:5432/postgres?sslmode=disable
	// postgres://signalsd-dev@localhost:14532/postgres?sslmode=disable

	// connect to the postgres database to create the test database
	postgresConnectionURL := buildConnString(config.userAndPassword, config.host, config.port, "postgres")

	// Check PostgreSQL server connectivity
	postgresPool := setupCleanDatabaseConn(t, postgresConnectionURL)
	if err := postgresPool.Ping(ctx); err != nil {
		if !ci {
			t.Fatalf("⚠️ Can't ping PostgreSQL server %s - is the docker db running?", postgresConnectionURL)
		} else {
			t.Fatalf("❌ Can't ping PostgreSQL server %s", postgresConnectionURL)
		}
	}

	_, err := postgresPool.Exec(ctx, "DROP DATABASE IF EXISTS "+config.dbname)
	if err != nil {
		t.Fatalf("DROP DATABASE IF EXISTS Failed : %v", err)
	}

	_, err = postgresPool.Exec(ctx, "CREATE DATABASE "+config.dbname)
	if err != nil {
		t.Fatalf("CREATE DATABASE Failed : %v", err)
	}

	// drop the test database when the test is complete
	t.Cleanup(func() {
		_, err := postgresPool.Exec(ctx, "DROP DATABASE "+config.dbname)
		if err != nil {
			t.Fatalf("Failed to drop test database: %v", err)
		}
	})
	t.Log("test database created")

	testDatabaseURLTODO := buildConnString(config.userAndPassword, config.host, config.port, config.dbname)
	testDatabasePool := setupCleanDatabaseConn(t, testDatabaseURLTODO)

	// Apply database migrations
	if err := runDatabaseMigrations(t, testDatabasePool); err != nil {
		t.Fatalf("❌ Failed to apply database migrations: %v", err)
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

	t.Log("✅ Database created")

	return testDatabasePool
}

func setupCleanDatabaseConn(t *testing.T, databaseURL string) *pgxpool.Pool {
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

// startInProcessServer starts the signalsd server in-process for testing - returns the base URL for the API and a shutdown function
// when using public isns be sure to load the test data before starting the server (the public isn cache is not dynamic and is only populated at startup)
// if you are not testing a function that generates end user facing urls then send an empty string for publicBaseURL
func startInProcessServer(t *testing.T, ctx context.Context, testDB *pgxpool.Pool, testDatabaseURL string, publicBaseURL string) (string, func()) {

	enableServerLogs := false

	if os.Getenv("ENABLE_SERVER_LOGS") == "true" {
		enableServerLogs = true
	}

	// Set environment variables before calling NewServerConfig

	originalEnvVars := make(map[string]string)
	testEnvVars := map[string]string{
		"SECRET_KEY":   testServerConfig.secretKey,
		"DATABASE_URL": testDatabaseURL,
		"ENVIRONMENT":  testServerConfig.environment,
		"LOG_LEVEL":    testServerConfig.logLevel,
		"PORT":         fmt.Sprintf("%d", findFreePort(t)),
	}

	// set test env vars
	for key, value := range testEnvVars {
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

	cfg, corsConfigs, err := signalsd.NewServerConfig()
	if err != nil {
		t.Fatalf("Failed to load configuration: %v", err)
	}

	cfg.ServiceMode = "all"

	// publicBaseURL is only used when generating end user facing links like password reset - default to the test env url
	if publicBaseURL != "" {
		cfg.PublicBaseURL = publicBaseURL
	}

	queries := database.New(testDB)
	authService := auth.NewAuthService(cfg.SecretKey, testServerConfig.environment, queries)

	schemaCache := schemas.NewSchemaCache()
	if err := schemaCache.Load(ctx, queries); err != nil {
		t.Logf("Warning: Failed to load schema cache: %v", err)
	}

	publicIsnCache := isns.NewPublicIsnCache()
	if err := publicIsnCache.Load(ctx, queries); err != nil {
		t.Logf("Warning: Failed to load public ISN cache: %v", err)
	}

	logLevel := logger.ParseLogLevel("none")

	if enableServerLogs {
		logLevel = logger.ParseLogLevel("debug")
	}
	appLogger := logger.InitLogger(logLevel, testServerConfig.environment)

	serverInstance := server.NewServer(
		testDB,
		queries,
		authService,
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
	shutdownFunc := func() {
		t.Log("Stopping server...")

		// Cancel the server context to trigger graceful shutdown
		serverCancel()

		// Wait for server to shut down gracefully with timeout
		select {
		case err := <-serverDone:
			if err != nil {
				t.Logf("Server shutdown with error: %v", err)
			} else {
				t.Log("✅ Server shut down gracefully")
			}
		case <-time.After(5 * time.Second):
			// if the server won't shutdown it will most likely be due to bugs, e.g uncommitted transactions or unclosed http requests bodies
			t.Log("⚠️ Server shutdown timeout")
		}

		// Ensure database connections are closed
		serverInstance.DatabaseShutdown()
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
