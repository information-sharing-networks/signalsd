package schemas

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"github.com/information-sharing-networks/signalsd/app/internal/database"
	signalsd "github.com/information-sharing-networks/signalsd/app/internal/server/config"
	"github.com/santhosh-tekuri/jsonschema/v6"
)

func SkipValidation(url string) bool {
	return url == signalsd.SkipValidationURL
}

// SchemaCache stores compiled JSON schemas indexed by signal type path: {signal_type_slug}/v{sem_ver}
// Signal types can be added while the signals handler is running, so the ValidateSignal function
// will refresh the cache when encountering an uncached Signal Type.
// The mutex protects the cache from concurrent access when multiple http go routines are validating signals
type SchemaCache struct {
	schemas    map[string]*jsonschema.Schema
	schemaURLs map[string]string // tracks schema URLs for each signal type path
	mutex      sync.RWMutex
}

// NewSchemaCache creates a new schema cache instance
func NewSchemaCache() *SchemaCache {
	return &SchemaCache{
		schemas:    make(map[string]*jsonschema.Schema),
		schemaURLs: make(map[string]string),
	}
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

// Load loads schemas from database and compiles them into memory cache
func (s *SchemaCache) Load(ctx context.Context, queries *database.Queries) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	return s.refresh(ctx, queries)
}

// Len returns the number of schemas loaded to the schema cache
func (s *SchemaCache) Len() int {
	return len(s.schemas)
}

// ValidateSignal validates signal JSON content against its schema
// Automatically refreshes schema cache if the signal type is not found
func (s *SchemaCache) ValidateSignal(ctx context.Context, queries *database.Queries, signalTypePath string, rawJSON json.RawMessage) error {
	// Try cache first
	s.mutex.RLock()
	if schemaURL, exists := s.schemaURLs[signalTypePath]; exists {
		if SkipValidation(schemaURL) {
			s.mutex.RUnlock()
			return nil
		}
		if schema, exists := s.schemas[signalTypePath]; exists {
			s.mutex.RUnlock()
			// Validate with cached schema
			var data any
			if err := json.Unmarshal(rawJSON, &data); err != nil {
				return fmt.Errorf("invalid JSON format: %v", err)
			}
			if err := schema.Validate(data); err != nil {
				return fmt.Errorf("schema validation failed: %w", err)
			}
			return nil // valid json confirmed
		}
	}
	s.mutex.RUnlock()

	// Schema not in cache, refresh cache
	s.mutex.Lock()
	// Refresh entire cache from database
	if err := s.refresh(ctx, queries); err != nil {
		s.mutex.Unlock()
		return fmt.Errorf("failed to refresh schema cache: %w", err)
	}
	s.mutex.Unlock()

	// Try validation with refreshed cache using read lock
	s.mutex.RLock()
	defer s.mutex.RUnlock()
	if schemaURL, exists := s.schemaURLs[signalTypePath]; exists {
		if SkipValidation(schemaURL) {
			return nil
		}
		if schema, exists := s.schemas[signalTypePath]; exists {
			// Validate with refreshed schema
			var data any
			if err := json.Unmarshal(rawJSON, &data); err != nil {
				return fmt.Errorf("invalid JSON format: %v", err)
			}
			if err := schema.Validate(data); err != nil {
				return fmt.Errorf("schema validation failed: %w", err)
			}
			return nil
		}
	}

	// Schema still not found after refresh
	return fmt.Errorf("signal type not found: %s", signalTypePath)
}

// refresh refreshes the cache from database (caller must hold write lock)
func (s *SchemaCache) refresh(ctx context.Context, queries *database.Queries) error {
	signalTypes, err := queries.GetSignalTypes(ctx)
	if err != nil {
		return fmt.Errorf("failed to get signal types from database: %w", err)
	}

	// Clear existing cache and rebuild
	s.schemas = make(map[string]*jsonschema.Schema)
	s.schemaURLs = make(map[string]string)

	var loadErrors []string

	for _, signalType := range signalTypes {
		// Create signal type path as cache key
		signalTypePath := fmt.Sprintf("%s/v%s", signalType.Slug, signalType.SemVer)

		// Compile the schema from the stored content
		schema, err := ValidateAndCompileSchema(signalType.SchemaURL, signalType.SchemaContent)
		if err != nil {
			loadErrors = append(loadErrors, fmt.Sprintf("signal type %s: %v", signalTypePath, err))
		} else {
			s.schemas[signalTypePath] = schema
			// Store the schema URL for this signal type path
			s.schemaURLs[signalTypePath] = signalType.SchemaURL
		}
	}

	if len(loadErrors) > 0 {
		return fmt.Errorf("failed to load some schemas: %s", strings.Join(loadErrors, "; "))
	}

	return nil
}
