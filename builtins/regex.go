package builtins

import (
	"container/list"
	"fmt"
	"regexp"
	"sync"

	"go.starlark.net/starlark"
	"go.starlark.net/starlarkstruct"
)

// RegexModule is the predeclared "regex" namespace module.
// It provides RE2 regular expression functions with a bounded LRU cache
// for compiled patterns.
var RegexModule = &starlarkstruct.Module{
	Name: "regex",
	Members: starlark.StringDict{
		"match":       starlark.NewBuiltin("regex.match", regexMatch),
		"find":        starlark.NewBuiltin("regex.find", regexFind),
		"find_all":    starlark.NewBuiltin("regex.find_all", regexFindAll),
		"find_groups": starlark.NewBuiltin("regex.find_groups", regexFindGroups),
		"replace":     starlark.NewBuiltin("regex.replace", regexReplace),
		"replace_all": starlark.NewBuiltin("regex.replace_all", regexReplaceAll),
		"split":       starlark.NewBuiltin("regex.split", regexSplit),
	},
}

// ---------------------------------------------------------------------------
// LRU cache for compiled *regexp.Regexp patterns
// ---------------------------------------------------------------------------

// cacheEntry stores a compiled regex alongside its pattern string for O(1) lookup.
type cacheEntry struct {
	pattern string
	re      *regexp.Regexp
}

// regexCache is a bounded LRU cache of compiled regular expressions.
type regexCache struct {
	mu       sync.Mutex
	capacity int
	ll       *list.List
	items    map[string]*list.Element
}

// newRegexCache creates a new LRU cache with the given maximum capacity.
func newRegexCache(capacity int) *regexCache {
	return &regexCache{
		capacity: capacity,
		ll:       list.New(),
		items:    make(map[string]*list.Element, capacity),
	}
}

// get returns a compiled regexp for the given pattern, using the cache when
// possible. On cache hit the entry is moved to the front (most recently used).
// On cache miss the pattern is compiled, pushed to the front, and the least
// recently used entry is evicted if the cache is at capacity.
func (c *regexCache) get(pattern string) (*regexp.Regexp, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if el, ok := c.items[pattern]; ok {
		c.ll.MoveToFront(el)
		return el.Value.(*cacheEntry).re, nil
	}

	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil, err
	}

	el := c.ll.PushFront(&cacheEntry{pattern: pattern, re: re})
	c.items[pattern] = el

	if c.ll.Len() > c.capacity {
		back := c.ll.Back()
		c.ll.Remove(back)
		delete(c.items, back.Value.(*cacheEntry).pattern)
	}

	return re, nil
}

// defaultCache is the package-level regex cache with capacity 64.
var defaultCache = newRegexCache(64)

// compilePattern compiles a regex pattern via the LRU cache and wraps any
// error with the calling function's name for user-friendly diagnostics.
func compilePattern(fnName, pattern string) (*regexp.Regexp, error) {
	re, err := defaultCache.get(pattern)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", fnName, err)
	}
	return re, nil
}

// ---------------------------------------------------------------------------
// Builtin implementations
// ---------------------------------------------------------------------------

// regexMatch implements regex.match(pattern, s) -> bool.
// Returns True if pattern matches anywhere in s.
func regexMatch(_ *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var pattern, s string
	if err := starlark.UnpackArgs(b.Name(), args, kwargs, "pattern", &pattern, "s", &s); err != nil {
		return nil, err
	}
	re, err := compilePattern(b.Name(), pattern)
	if err != nil {
		return nil, err
	}
	return starlark.Bool(re.MatchString(s)), nil
}

// regexFind implements regex.find(pattern, s) -> string or None.
// Returns the first match string, or None if no match.
func regexFind(_ *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var pattern, s string
	if err := starlark.UnpackArgs(b.Name(), args, kwargs, "pattern", &pattern, "s", &s); err != nil {
		return nil, err
	}
	re, err := compilePattern(b.Name(), pattern)
	if err != nil {
		return nil, err
	}
	loc := re.FindStringIndex(s)
	if loc == nil {
		return starlark.None, nil
	}
	return starlark.String(s[loc[0]:loc[1]]), nil
}

// regexFindAll implements regex.find_all(pattern, s) -> list[string].
// Returns a list of all non-overlapping match strings. Returns an empty
// list if no matches are found.
func regexFindAll(_ *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var pattern, s string
	if err := starlark.UnpackArgs(b.Name(), args, kwargs, "pattern", &pattern, "s", &s); err != nil {
		return nil, err
	}
	re, err := compilePattern(b.Name(), pattern)
	if err != nil {
		return nil, err
	}
	matches := re.FindAllString(s, -1)
	elems := make([]starlark.Value, len(matches))
	for i, m := range matches {
		elems[i] = starlark.String(m)
	}
	return starlark.NewList(elems), nil
}

// regexFindGroups implements regex.find_groups(pattern, s) -> list[string] or None.
// Returns the capture groups (excluding group 0 / full match) from the first
// match, or None if no match is found.
func regexFindGroups(_ *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var pattern, s string
	if err := starlark.UnpackArgs(b.Name(), args, kwargs, "pattern", &pattern, "s", &s); err != nil {
		return nil, err
	}
	re, err := compilePattern(b.Name(), pattern)
	if err != nil {
		return nil, err
	}
	match := re.FindStringSubmatch(s)
	if match == nil {
		return starlark.None, nil
	}
	groups := make([]starlark.Value, len(match)-1)
	for i, m := range match[1:] {
		groups[i] = starlark.String(m)
	}
	return starlark.NewList(groups), nil
}

// regexReplace implements regex.replace(pattern, s, replacement) -> string.
// Replaces only the first match with $1 backreference support via ExpandString.
// Returns the original string unchanged if no match is found.
func regexReplace(_ *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var pattern, s, replacement string
	if err := starlark.UnpackArgs(b.Name(), args, kwargs, "pattern", &pattern, "s", &s, "replacement", &replacement); err != nil {
		return nil, err
	}
	re, err := compilePattern(b.Name(), pattern)
	if err != nil {
		return nil, err
	}
	loc := re.FindStringSubmatchIndex(s)
	if loc == nil {
		return starlark.String(s), nil
	}
	var result []byte
	result = append(result, s[:loc[0]]...)
	result = re.ExpandString(result, replacement, s, loc)
	result = append(result, s[loc[1]:]...)
	return starlark.String(string(result)), nil
}

// regexReplaceAll implements regex.replace_all(pattern, s, replacement) -> string.
// Replaces all matches with $1 backreference support (Go stdlib Expand).
func regexReplaceAll(_ *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var pattern, s, replacement string
	if err := starlark.UnpackArgs(b.Name(), args, kwargs, "pattern", &pattern, "s", &s, "replacement", &replacement); err != nil {
		return nil, err
	}
	re, err := compilePattern(b.Name(), pattern)
	if err != nil {
		return nil, err
	}
	return starlark.String(re.ReplaceAllString(s, replacement)), nil
}

// regexSplit implements regex.split(pattern, s) -> list[string].
// Splits s on all matches of pattern and returns the resulting list of strings.
func regexSplit(_ *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var pattern, s string
	if err := starlark.UnpackArgs(b.Name(), args, kwargs, "pattern", &pattern, "s", &s); err != nil {
		return nil, err
	}
	re, err := compilePattern(b.Name(), pattern)
	if err != nil {
		return nil, err
	}
	parts := re.Split(s, -1)
	elems := make([]starlark.Value, len(parts))
	for i, p := range parts {
		elems[i] = starlark.String(p)
	}
	return starlark.NewList(elems), nil
}
