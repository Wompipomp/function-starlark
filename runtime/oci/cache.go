package oci

import (
	"sync"
	"time"
)

// Cache provides a two-layer in-memory cache for OCI module resolution.
//
// Layer 1 (tag cache): maps tag-ref strings (e.g. "ghcr.io/org/lib:v1") to
// digests with a configurable TTL. Stale entries are returned with fresh=false
// so the caller can decide whether to serve stale content.
//
// Layer 2 (content cache): maps content digests to extracted file maps.
// Content is immutable by definition (content-addressed), so there is no TTL.
type Cache struct {
	mu       sync.RWMutex
	tags     map[string]*tagEntry
	contents map[string]map[string]string
	ttl      time.Duration
	nowFn    func() time.Time // injectable clock for testing
}

// tagEntry stores a digest and its TTL expiry time.
type tagEntry struct {
	digest  string
	expires time.Time
}

// NewCache creates a Cache with the given TTL for tag-to-digest mappings.
func NewCache(ttl time.Duration) *Cache {
	return &Cache{
		tags:     make(map[string]*tagEntry),
		contents: make(map[string]map[string]string),
		ttl:      ttl,
		nowFn:    time.Now,
	}
}

// GetByTag looks up files by tag reference string.
//
// Returns (files, true) for a fresh cache hit (within TTL).
// Returns (files, false) for a stale cache hit (expired TTL but content exists).
// Returns (nil, false) for a cache miss.
func (c *Cache) GetByTag(ref string) (map[string]string, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	te, ok := c.tags[ref]
	if !ok {
		return nil, false // cold miss
	}

	files, contentOK := c.contents[te.digest]
	if !contentOK {
		return nil, false
	}

	if c.nowFn().After(te.expires) {
		return files, false // stale -- caller should re-resolve but can use as fallback
	}

	return files, true // fresh hit
}

// GetByDigest looks up files by content digest.
// Returns (files, true) for a hit, (nil, false) for a miss.
func (c *Cache) GetByDigest(digest string) (map[string]string, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	files, ok := c.contents[digest]
	return files, ok
}

// PutTag stores a tag-to-digest mapping with TTL expiry.
func (c *Cache) PutTag(ref, digest string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.tags[ref] = &tagEntry{
		digest:  digest,
		expires: c.nowFn().Add(c.ttl),
	}
}

// PutContent stores a digest-to-files mapping. Content is immutable and
// never expires (content-addressed = same digest always has same content).
func (c *Cache) PutContent(digest string, files map[string]string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Don't overwrite existing content -- it is immutable.
	if _, exists := c.contents[digest]; exists {
		return
	}
	c.contents[digest] = files
}
