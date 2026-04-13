package schemas

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/information-sharing-networks/signalsd/app/internal/database"
	signalsd "github.com/information-sharing-networks/signalsd/app/internal/server/config"
	"github.com/santhosh-tekuri/jsonschema/v6"
)

// SkipValidation returns true if the registered schema is the pre-defined SkipValidationURL
func SkipValidation(url string) bool {
	return url == signalsd.SkipValidationURL
}

// Cache stores compiled JSON schemas indexed by signal type path: {signal_type_slug}/v{sem_ver}
// The mutex protects the cache from concurrent access when multiple http go routines are validating signals
// Initialised on startup and refreshed by polling (see server.go)
type Cache struct {
	db         *database.Queries
	mu         sync.RWMutex
	schemas    map[string]*jsonschema.Schema
	schemaURLs map[string]string // tracks schema URLs for each signal type path
}

// NewCache creates a new schema cache instance
func NewCache(db *database.Queries) *Cache {
	return &Cache{
		db:         db,
		schemas:    make(map[string]*jsonschema.Schema),
		schemaURLs: make(map[string]string),
	}
}

// Load loads schemas from database and compiles them into memory cache
func (c *Cache) Load(ctx context.Context) error {

	signalTypes, err := c.db.GetSignalTypes(ctx)
	if err != nil {
		return fmt.Errorf("failed to get signal types from database: %w", err)
	}

	schemas := make(map[string]*jsonschema.Schema)
	schemaURLs := make(map[string]string)

	var loadErrors []string

	for _, signalType := range signalTypes {
		// Create signal type path as cache key
		signalTypePath := fmt.Sprintf("%s/v%s", signalType.Slug, signalType.SemVer)

		// Compile the schema from the stored content
		schema, err := ValidateAndCompileSchema(signalType.SchemaURL, signalType.SchemaContent)
		if err != nil {
			loadErrors = append(loadErrors, fmt.Sprintf("signal type %s: %v", signalTypePath, err))
		} else {
			schemas[signalTypePath] = schema
			// Store the schema URL for this signal type path
			schemaURLs[signalTypePath] = signalType.SchemaURL
		}

	}

	if len(loadErrors) > 0 {
		return fmt.Errorf("failed to compile one or more schemas: %s", strings.Join(loadErrors, "; "))
	}

	c.mu.Lock()
	c.schemas = schemas
	c.schemaURLs = schemaURLs
	c.mu.Unlock()

	return nil
}

// StartPolling starts a background goroutine that reloads the routing rules from the
// database every interval. Errors are logged but do not stop the polling loop.
// The goroutine exits when ctx is cancelled.
func (c *Cache) StartPolling(ctx context.Context, interval time.Duration) {

	go func() {

		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				if err := c.Load(ctx); err != nil {
					slog.Error("routing cache: poll refresh failed", slog.String("error", err.Error()))
				}
			case <-ctx.Done():
				return
			}
		}
	}()
}

// Len returns the number of schemas loaded to the schema cache
func (c *Cache) Len() int {
	return len(c.schemas)
}

// ValidateSignal validates the JSON payload for a signal against its schema
func (c *Cache) ValidateSignal(ctx context.Context, queries *database.Queries, signalTypePath string, rawJSON json.RawMessage) error {

	c.mu.RLock()
	defer c.mu.RUnlock()

	schemaURL, exists := c.schemaURLs[signalTypePath]
	if !exists {
		return fmt.Errorf("no schema found in the cache for this signal type")
	}

	// Try cache first
	if SkipValidation(schemaURL) {
		return nil
	}

	schema, exists := c.schemas[signalTypePath]
	if !exists {
		return fmt.Errorf("internal error: schema missing in cache for signalTypePath %s", signalTypePath)
	}

	// Validate with cached schema
	var data any
	if err := json.Unmarshal(rawJSON, &data); err != nil {
		return fmt.Errorf("invalid JSON format: %v", err)
	}

	if err := schema.Validate(data); err != nil {
		return fmt.Errorf("schema validation failed: %w", err)
	}

	// json successfully validates against schema
	return nil
}

// ValidateAndCompileSchema validates schema content and returns the compiled schema
func ValidateAndCompileSchema(schemaURL, content string) (*jsonschema.Schema, error) {
	// Parse the schema content using UnmarshalJSON
	schemaData, err := jsonschema.UnmarshalJSON(strings.NewReader(content))
	if err != nil {
		return nil, fmt.Errorf("schema content is not valid JSON: %v", err)
	}

	// Compile the schema using the Compiler API
	compiler := jsonschema.NewCompiler()
	if err := compiler.AddResource(schemaURL, schemaData); err != nil {
		return nil, fmt.Errorf("failed to add schema resource: %w", err)
	}

	schema, err := compiler.Compile(schemaURL)
	if err != nil {
		return nil, fmt.Errorf("failed to compile schema: %w", err)
	}

	return schema, nil
}
