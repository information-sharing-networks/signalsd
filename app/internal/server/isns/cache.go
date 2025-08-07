package isns

import (
	"context"
	"fmt"

	"github.com/information-sharing-networks/signalsd/app/internal/database"
)

// PublicIsnCache stores public ISN data for quick lookup
type PublicIsnCache struct {
	isnSlugs    map[string]bool
	signalTypes map[string]map[string]bool // ISN slug -> signal type path -> exists
}

// NewPublicIsnCache creates a new public ISN cache instance
func NewPublicIsnCache() *PublicIsnCache {
	return &PublicIsnCache{
		isnSlugs:    make(map[string]bool),
		signalTypes: make(map[string]map[string]bool),
	}
}

// Load loads public ISN slugs and their signal types from database into memory cache
func (p *PublicIsnCache) Load(ctx context.Context, queries *database.Queries) error {
	// Clear existing cache
	p.isnSlugs = make(map[string]bool)
	p.signalTypes = make(map[string]map[string]bool)

	publicSignalTypes, err := queries.GetInUsePublicIsnSignalTypes(ctx)
	if err != nil {
		return fmt.Errorf("failed to get public ISN signal types from database: %w", err)
	}

	for _, row := range publicSignalTypes {
		if !p.isnSlugs[row.IsnSlug] {
			p.isnSlugs[row.IsnSlug] = true
			p.signalTypes[row.IsnSlug] = make(map[string]bool)
		}

		// Build signal type path: signal-type-slug/v1.0.0
		signalTypePath := fmt.Sprintf("%s/v%s", row.SignalTypeSlug, row.SemVer)
		p.signalTypes[row.IsnSlug][signalTypePath] = true
	}

	return nil
}

func (p *PublicIsnCache) Len() int {
	return len(p.isnSlugs)
}

func (p *PublicIsnCache) Contains(slug string) bool {
	return p.isnSlugs[slug]
}

// HasSignalType checks if a signal type path is available on a public ISN
func (p *PublicIsnCache) HasSignalType(isnSlug, signalTypePath string) bool {
	signalTypeMap, exists := p.signalTypes[isnSlug]
	if !exists {
		return false
	}

	return signalTypeMap[signalTypePath]
}
