package isns

import (
	"context"
	"fmt"

	"github.com/information-sharing-networks/signalsd/app/internal/database"
)

// publicISNCache stores public ISN data for quick lookup
type publicISNCache struct {
	isnSlugs    map[string]bool
	signalTypes map[string]map[string]bool // ISN slug -> signal type path -> exists
}

var cache *publicISNCache

// LoadPublicISNCache loads public ISN slugs and their signal types from database into memory cache
func LoadPublicISNCache(ctx context.Context, queries *database.Queries) error {
	// Initialize cache
	cache = &publicISNCache{
		isnSlugs:    make(map[string]bool),
		signalTypes: make(map[string]map[string]bool),
	}

	publicSignalTypes, err := queries.GetPublicIsnSignalTypes(ctx)
	if err != nil {
		return fmt.Errorf("failed to get public ISN signal types from database: %w", err)
	}

	for _, row := range publicSignalTypes {
		if !cache.isnSlugs[row.IsnSlug] {
			cache.isnSlugs[row.IsnSlug] = true
			cache.signalTypes[row.IsnSlug] = make(map[string]bool)
		}

		// Build signal type path: signal-type-slug/v1.0.0
		signalTypePath := fmt.Sprintf("%s/v%s", row.SignalTypeSlug, row.SemVer)
		cache.signalTypes[row.IsnSlug][signalTypePath] = true
	}

	return nil
}

func PublicISNCount() int {
	if cache == nil {
		return 0
	}
	return len(cache.isnSlugs)
}

func IsPublicISN(slug string) bool {
	if cache == nil {
		return false
	}
	return cache.isnSlugs[slug]
}

// IsPublicSignalType checks if a signal type path is available on a public ISN
func IsPublicSignalType(isnSlug, signalTypePath string) bool {
	if cache == nil {
		return false
	}

	signalTypeMap, exists := cache.signalTypes[isnSlug]
	if !exists {
		return false
	}

	return signalTypeMap[signalTypePath]
}
