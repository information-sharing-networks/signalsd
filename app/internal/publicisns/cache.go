package publicisns

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/information-sharing-networks/signalsd/app/internal/database"
)

// Cache stores public ISN data for quick lookup
type Cache struct {
	db          *database.Queries
	mu          sync.RWMutex
	isnSlugs    map[string]bool
	signalTypes map[string]map[string]bool // ISN slug -> signal type path -> exists
}

// NewCache creates a new public ISN cache instance
func NewCache(db *database.Queries) *Cache {
	return &Cache{
		db:          db,
		isnSlugs:    make(map[string]bool),
		signalTypes: make(map[string]map[string]bool),
	}
}

// Load loads public ISN slugs and their signal types from database and replaces the cache
func (c *Cache) Load(ctx context.Context) error {

	isnSlugs := make(map[string]bool)
	signalTypes := make(map[string]map[string]bool)

	publicSignalTypes, err := c.db.GetInUsePublicIsnSignalTypes(ctx)
	if err != nil {
		return fmt.Errorf("failed to get public ISN signal types from database: %w", err)
	}

	for _, row := range publicSignalTypes {
		if !isnSlugs[row.IsnSlug] {
			isnSlugs[row.IsnSlug] = true
			signalTypes[row.IsnSlug] = make(map[string]bool)
		}

		// Build signal type path: signal-type-slug/v1.0.0
		signalTypePath := fmt.Sprintf("%s/v%s", row.SignalTypeSlug, row.SemVer)
		signalTypes[row.IsnSlug][signalTypePath] = true
	}

	c.mu.Lock()
	c.isnSlugs = isnSlugs
	c.signalTypes = signalTypes
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
					slog.Error("public isn cache: poll refresh failed", slog.String("error", err.Error()))
				}
			case <-ctx.Done():
				return
			}
		}
	}()
}

func (c *Cache) Len() int {
	return len(c.isnSlugs)
}

func (c *Cache) Contains(slug string) bool {
	return c.isnSlugs[slug]
}

// HasSignalType checks if a signal type path is available on a public ISN
func (c *Cache) HasSignalType(isnSlug, signalTypePath string) bool {
	signalTypeMap, exists := c.signalTypes[isnSlug]
	if !exists {
		return false
	}

	return signalTypeMap[signalTypePath]
}
