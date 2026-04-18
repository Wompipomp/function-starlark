package oci

import (
	"sync"
	"testing"
	"time"
)

func TestNewCache(t *testing.T) {
	c := NewCache(PullAlways, 5*time.Minute)
	if c == nil {
		t.Fatal("NewCache returned nil")
	}
}

func TestCacheGetByTagEmpty(t *testing.T) {
	c := NewCache(PullAlways, 5*time.Minute)
	files, fresh := c.GetByTag("ghcr.io/org/lib:v1")
	if files != nil {
		t.Errorf("expected nil files for empty cache, got %v", files)
	}
	if fresh {
		t.Error("expected fresh=false for empty cache")
	}
}

func TestCachePutAndGetFresh(t *testing.T) {
	now := time.Now()
	c := NewCache(PullAlways, 5*time.Minute)
	c.nowFn = func() time.Time { return now }

	c.PutContent("sha256:abc", map[string]string{"h.star": "x = 1"})
	c.PutTag("ghcr.io/org/lib:v1", "sha256:abc")

	files, fresh := c.GetByTag("ghcr.io/org/lib:v1")
	if files == nil {
		t.Fatal("expected files, got nil")
	}
	if !fresh {
		t.Error("expected fresh=true within TTL")
	}
	if files["h.star"] != "x = 1" {
		t.Errorf("got %q, want %q", files["h.star"], "x = 1")
	}
}

func TestCacheGetByTagStale(t *testing.T) {
	now := time.Now()
	c := NewCache(PullAlways, 5*time.Minute)
	c.nowFn = func() time.Time { return now }

	c.PutContent("sha256:abc", map[string]string{"h.star": "x = 1"})
	c.PutTag("ghcr.io/org/lib:v1", "sha256:abc")

	// Advance time past TTL.
	c.nowFn = func() time.Time { return now.Add(6 * time.Minute) }

	files, fresh := c.GetByTag("ghcr.io/org/lib:v1")
	if files == nil {
		t.Fatal("expected stale files, got nil")
	}
	if fresh {
		t.Error("expected fresh=false for stale cache")
	}
	if files["h.star"] != "x = 1" {
		t.Errorf("got %q, want %q", files["h.star"], "x = 1")
	}
}

// Under IfNotPresent the TTL is ignored and cached entries stay fresh
// indefinitely — this is the zero-traffic-after-first-pull mode.
func TestCacheGetByTagIfNotPresent_NeverStales(t *testing.T) {
	now := time.Now()
	// TTL of 5 minutes would mark this stale under PullAlways; here it must not.
	c := NewCache(PullIfNotPresent, 5*time.Minute)
	c.nowFn = func() time.Time { return now }

	c.PutContent("sha256:abc", map[string]string{"h.star": "x = 1"})
	c.PutTag("ghcr.io/org/lib:v1", "sha256:abc")

	// Advance the clock by a year.
	c.nowFn = func() time.Time { return now.Add(365 * 24 * time.Hour) }

	files, fresh := c.GetByTag("ghcr.io/org/lib:v1")
	if files == nil {
		t.Fatal("expected cached files, got nil")
	}
	if !fresh {
		t.Error("IfNotPresent should treat any cached entry as fresh")
	}
	if files["h.star"] != "x = 1" {
		t.Errorf("got %q, want %q", files["h.star"], "x = 1")
	}
}

// Unknown policy strings fall back to IfNotPresent (the safe default).
func TestNewCache_UnknownPolicyDefaultsToIfNotPresent(t *testing.T) {
	c := NewCache(PullPolicy("garbage"), 0)
	if c.policy != PullIfNotPresent {
		t.Errorf("policy = %q, want %q", c.policy, PullIfNotPresent)
	}
}

func TestCacheGetByDigestHit(t *testing.T) {
	c := NewCache(PullAlways, 5*time.Minute)
	c.PutContent("sha256:abc", map[string]string{"h.star": "x = 1"})

	files, ok := c.GetByDigest("sha256:abc")
	if !ok {
		t.Fatal("expected ok=true for known digest")
	}
	if files["h.star"] != "x = 1" {
		t.Errorf("got %q, want %q", files["h.star"], "x = 1")
	}
}

func TestCacheGetByDigestMiss(t *testing.T) {
	c := NewCache(PullAlways, 5*time.Minute)
	files, ok := c.GetByDigest("sha256:unknown")
	if ok {
		t.Error("expected ok=false for unknown digest")
	}
	if files != nil {
		t.Errorf("expected nil files, got %v", files)
	}
}

func TestCacheConcurrentAccess(t *testing.T) {
	c := NewCache(PullAlways, 5*time.Minute)
	var wg sync.WaitGroup

	// Concurrent writers.
	for i := range 50 {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			digest := "sha256:d" + string(rune('0'+i%10))
			ref := "ghcr.io/org/lib:v" + string(rune('0'+i%10))
			c.PutContent(digest, map[string]string{"h.star": "x = 1"})
			c.PutTag(ref, digest)
		}(i)
	}

	// Concurrent readers.
	for range 50 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c.GetByTag("ghcr.io/org/lib:v1")
			c.GetByDigest("sha256:d1")
		}()
	}

	wg.Wait()
}
