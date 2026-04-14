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
// When defaultRegistry is non-empty, short-form targets (containing ":" but not
// starting with "oci://") are expanded to full oci:// URLs.
//
// parentRef, when non-empty, is the OCI reference (e.g. "ghcr.io/org/mod:v1")
// of the caller artifact; package-local load targets ("./file.star") are
// expanded against this reference. When parentRef is empty (top-level scan of
// the main script / user-supplied inline modules) a package-local target is an
// error because such callers have no OCI parent.
func ScanForOCILoads(source string, inlineModules map[string]string, defaultRegistry, parentRef string) ([]*OCILoadTarget, error) {
	var targets []*OCILoadTarget

	// Scan main script.
	found, err := scanSource(source, "composition.star", defaultRegistry, parentRef)
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
		found, err := scanSource(inlineModules[name], name, defaultRegistry, parentRef)
		if err != nil {
			return nil, err
		}
		targets = append(targets, found...)
	}

	return dedup(targets), nil
}

// scanSource parses a single Starlark source string and extracts oci:// load targets.
// parentRef is used to expand package-local ("./file.star") targets; empty means
// the caller has no OCI parent and package-local targets are rejected.
func scanSource(source, filename, defaultRegistry, parentRef string) ([]*OCILoadTarget, error) {
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
		// Package-local ./file.star is same-artifact; expand against parentRef
		// BEFORE short-form detection so it never gets mistaken for a
		// short-form default-registry target.
		if IsPackageLocalTarget(mod) {
			expanded, err := ExpandPackageLocal(mod, parentRef)
			if err != nil {
				return nil, fmt.Errorf("scanning %s: %w", filename, err)
			}
			mod = expanded
		} else if IsDefaultRegistryTarget(mod) {
			expanded, err := ExpandDefaultRegistry(mod, defaultRegistry)
			if err != nil {
				return nil, err
			}
			mod = expanded
		}
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

// dedup removes duplicate targets by RefStr+File combination. Multiple files
// from the same artifact are preserved so the resolver maps all of them.
func dedup(targets []*OCILoadTarget) []*OCILoadTarget {
	type key struct{ ref, file string }
	seen := make(map[key]bool, len(targets))
	result := make([]*OCILoadTarget, 0, len(targets))
	for _, t := range targets {
		k := key{t.RefStr, t.File}
		if seen[k] {
			continue
		}
		seen[k] = true
		result = append(result, t)
	}
	return result
}
