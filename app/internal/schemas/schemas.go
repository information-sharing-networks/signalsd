package schemas

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"

	"github.com/information-sharing-networks/signalsd/app/internal/database"
	"github.com/santhosh-tekuri/jsonschema/v5"
)

// this GitHub URL indicates validation should be skipped
const NoValidationURL = "https://github.com/skip/validation/main/schema.json"

func SkipValidation(url string) bool {
	return url == NoValidationURL
}

// stores compiled JSON schemas indexed by signal type path. "{signal_type_slug}/v{sem_ver}"
type schemaCache struct {
	schemas map[string]*jsonschema.Schema
}

var cache *schemaCache

// FetchSchema gets a JSON schema from a URL
// if you supply a 'blob' url (i.e. the url rendered by github web interface) it will be converted to a raw url
func FetchSchema(url string) (string, error) {
	originalURL := url

	// Example: https://github.com/org/repo/blob/2025.01.01/file.json
	//       -> https://raw.githubusercontent.com/org/repo/2025.01.01/file.json
	if strings.HasPrefix(url, "https://github.com/") {
		url = strings.Replace(url, "https://github.com/", "https://raw.githubusercontent.com/", 1)
		url = strings.Replace(url, "/blob/", "/", 1)
	}

	res, err := http.Get(url)
	if err != nil {
		return "", fmt.Errorf("failed to fetch schema from %s: %w", originalURL, err)
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		if res.StatusCode == http.StatusNotFound {
			return "", fmt.Errorf("schema not found at %s", originalURL)
		}
		return "", fmt.Errorf("schema fetch failed with status: %d", res.StatusCode)
	}

	bodyBytes, err := io.ReadAll(res.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response body: %w", err)
	}

	return string(bodyBytes), nil
}

// checks if URL is a github url ending in .json
func ValidateSchemaURL(url string) error {

	if SkipValidation(url) {
		return nil
	}

	githubPattern := `^https://github\.com/[a-zA-Z0-9_-]+/[a-zA-Z0-9_.-]+/.*\.json$`
	matched, err := regexp.MatchString(githubPattern, url)
	if err != nil {
		return fmt.Errorf("error validating URL pattern: %w", err)
	}

	if !matched {
		return fmt.Errorf("schema URL must be a GitHub URL ending in .json (e.g., https://github.com/org/repo/blob/2025.01.01/schema.json) or use %s to skip validation", NoValidationURL)
	}

	return nil
}

// ValidateAndCompileSchema validates schema content and returns the compiled schema
func ValidateAndCompileSchema(schemaURL, content string) (*jsonschema.Schema, error) {
	// First check if it's valid JSON
	var jsonData any
	if err := json.Unmarshal([]byte(content), &jsonData); err != nil {
		return nil, fmt.Errorf("schema content is not valid JSON: %v", err)
	}

	// Compile the schema (this also validates it)
	schema, err := jsonschema.CompileString(schemaURL, content)
	if err != nil {
		return nil, fmt.Errorf("invalid JSON Schema format: %w", err)
	}
	return schema, nil
}

// ValidateJSON validates unmarshaled data against a cached schema using signal type path
func ValidateJSON(signalTypePath string, data any) error {
	if cache == nil {
		return fmt.Errorf("schema cache not initialized")
	}

	schema, exists := cache.schemas[signalTypePath]
	if !exists {
		return fmt.Errorf("schema not found in cache for signal type: %s", signalTypePath)
	}

	if err := schema.Validate(data); err != nil {
		return fmt.Errorf("schema validation failed: %w", err)
	}

	return nil
}

// LoadSchemaCache loads schema content from database and compiles them into memory cache
func LoadSchemaCache(ctx context.Context, queries *database.Queries) error {
	// Initialize the cache
	cache = &schemaCache{
		schemas: make(map[string]*jsonschema.Schema),
	}

	signalTypes, err := queries.GetSignalTypes(ctx)
	if err != nil {
		return fmt.Errorf("failed to get signal types from database: %w", err)
	}

	var loadErrors []string

	for _, signalType := range signalTypes {
		// Create signal type path as cache key
		signalTypePath := fmt.Sprintf("%s/v%s", signalType.Slug, signalType.SemVer)

		// Skip if we already have this signal type path cached
		if _, exists := cache.schemas[signalTypePath]; exists {
			continue
		}

		// Compile the schema from the stored content
		schema, err := jsonschema.CompileString(signalType.SchemaURL, signalType.SchemaContent)
		if err != nil {
			loadErrors = append(loadErrors, fmt.Sprintf("signal type %s: %v", signalTypePath, err))
		} else {
			cache.schemas[signalTypePath] = schema
		}
	}

	if len(loadErrors) > 0 {
		return fmt.Errorf("failed to load some schemas: %s", strings.Join(loadErrors, "; "))
	}

	return nil
}

// AddToCache adds a compiled schema to the cache
func AddToCache(signalTypePath string, schema *jsonschema.Schema) {
	if cache == nil {
		// Initialize cache if this is the first signal type being added
		cache = &schemaCache{
			schemas: make(map[string]*jsonschema.Schema),
		}
	}
	cache.schemas[signalTypePath] = schema
}
