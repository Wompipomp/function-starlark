package oci

import (
	"sync"
	"time"
)

// PullPolicy controls whether the resolver revalidates a cached tag against
// the registry. Semantics mirror Kubernetes imagePullPolicy:
//
//   - PullIfNotPresent: treat tags as immutable; once cached, never HEAD-check
//     again. The pod must be restarted (or a different tag/digest pinned) to
//     pick up a retag. This is the recommended default for production.
//   - PullAlways: HEAD-check on cache miss or after TTL expires. Blob bytes
//     only move when the digest actually changed, but every revalidation is
//     one manifest HEAD round-trip.
type PullPolicy string

const (
	// PullIfNotPresent caches tag->digest forever for the process lifetime.
	PullIfNotPresent PullPolicy = "IfNotPresent"
	// PullAlways revalidates with HEAD once per TTL window (or every
	// reconciliation when TTL is 0).
	PullAlways PullPolicy = "Always"
)

// Cache provides a two-layer in-memory cache for OCI module resolution.
//
// Layer 1 (tag cache): maps tag-ref strings (e.g. "ghcr.io/org/lib:v1") to
// digests. Revalidation is governed by PullPolicy; TTL is only consulted
// under PullAlways.
//
// Layer 2 (content cache): maps content digests to extracted file maps.
// Content is immutable by definition (content-addressed), so there is no TTL.
type Cache struct {
	mu       sync.RWMutex
	tags     map[string]*tagEntry
	contents map[string]map[string]string
	policy   PullPolicy
	ttl      time.Duration
	nowFn    func() time.Time // injectable clock for testing
}

// tagEntry stores a digest and its TTL expiry time.
type tagEntry struct {
	digest  string
	expires time.Time
}

// NewCache creates a Cache with the given pull policy and TTL. TTL is ignored
// when policy is PullIfNotPresent. An unrecognised policy is treated as
// PullIfNotPresent.
func NewCache(policy PullPolicy, ttl time.Duration) *Cache {
	if policy != PullAlways {
		policy = PullIfNotPresent
	}
	return &Cache{
		tags:     make(map[string]*tagEntry),
		contents: make(map[string]map[string]string),
		policy:   policy,
		ttl:      ttl,
		nowFn:    time.Now,
	}
}

// GetByTag looks up files by tag reference string.
//
// Under PullIfNotPresent: any cached entry is returned fresh.
// Under PullAlways: entries within TTL are fresh; expired entries are
// returned with fresh=false so the caller can revalidate via HEAD.
// Cold misses return (nil, false).
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

	if c.policy == PullIfNotPresent {
		return files, true
	}
	if c.ttl > 0 && c.nowFn().After(te.expires) {
		return files, false // stale -- caller should re-resolve but can use as fallback
	}
	return files, true // within TTL
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
