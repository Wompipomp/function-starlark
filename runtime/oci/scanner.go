package oci

import (
	"fmt"
	"sort"
	"strings"

	"go.starlark.net/syntax"
)

// ScanForOCILoads parses Starlark source and inline modules for oci:// load()
// targets. Returns deduplicated targets (by RefStr). Each unique OCI artifact
// reference appears at most once, even if multiple files are loaded from it.
func ScanForOCILoads(source string, inlineModules map[string]string) ([]*OCILoadTarget, error) {
	var targets []*OCILoadTarget

	// Scan main script.
	found, err := scanSource(source, "composition.star")
	if err != nil {
		return nil, err
	}
	targets = append(targets, found...)

	// Scan inline modules in deterministic order.
	names := make([]string, 0, len(inlineModules))
	for name := range inlineModules {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		found, err := scanSource(inlineModules[name], name)
		if err != nil {
			return nil, err
		}
		targets = append(targets, found...)
	}

	return dedup(targets), nil
}

// scanSource parses a single Starlark source string and extracts oci:// load targets.
func scanSource(source, filename string) ([]*OCILoadTarget, error) {
	opts := &syntax.FileOptions{
		TopLevelControl: true,
		Set:             true,
		While:           true,
	}
	f, err := opts.Parse(filename, source, 0)
	if err != nil {
		return nil, fmt.Errorf("parsing %s for OCI loads: %w", filename, err)
	}

	var targets []*OCILoadTarget
	for _, stmt := range f.Stmts {
		load, ok := stmt.(*syntax.LoadStmt)
		if !ok {
			continue
		}
		mod := load.ModuleName()
		if !strings.HasPrefix(mod, "oci://") {
			continue
		}
		target, err := ParseOCILoadTarget(mod)
		if err != nil {
			return nil, err
		}
		targets = append(targets, target)
	}
	return targets, nil
}

// dedup removes duplicate targets by RefStr. The first target for each unique
// RefStr is kept.
func dedup(targets []*OCILoadTarget) []*OCILoadTarget {
	seen := make(map[string]bool, len(targets))
	result := make([]*OCILoadTarget, 0, len(targets))
	for _, t := range targets {
		if seen[t.RefStr] {
			continue
		}
		seen[t.RefStr] = true
		result = append(result, t)
	}
	return result
}
