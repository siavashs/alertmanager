package silence

import (
	"sync"

	"github.com/prometheus/common/model"
)

type cacheEntry struct {
	activeIDs  []string
	pendingIDs []string
	version    int
}

func newCacheEntry(activeIDs, pendingIDs []string, version int) *cacheEntry {
	return &cacheEntry{
		activeIDs:  activeIDs,
		pendingIDs: pendingIDs,
		version:    version,
	}
}

func (e *cacheEntry) count() int {
	return len(e.activeIDs) + len(e.pendingIDs)
}

type cache struct {
	entries map[model.Fingerprint]*cacheEntry
	mu      sync.RWMutex
}

func (c *cache) get(fp model.Fingerprint) cacheEntry {
	c.mu.RLock()
	defer c.mu.RUnlock()
	entry := cacheEntry{}
	if e, found := c.entries[fp]; found {
		entry = *e
	}
	return entry
}

func (c *cache) set(fp model.Fingerprint, entry *cacheEntry) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries[fp] = entry
}
