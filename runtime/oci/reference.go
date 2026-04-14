// Package oci provides OCI registry module resolution for Starlark scripts.
//
// It supports loading Starlark modules from OCI container registries using
// the oci://registry/repo:tag/file.star URL syntax.
package oci

import (
	"fmt"
	"strings"

	"github.com/google/go-containerregistry/pkg/name"
)

// OCILoadTarget represents a parsed oci:// load() URL.
type OCILoadTarget struct {
	RawURL   string // original load() string, e.g. "oci://ghcr.io/org/modules:v1/helpers.star"
	Registry string // e.g. "ghcr.io"
	Repo     string // e.g. "org/modules"
	Tag      string // e.g. "v1" (empty if digest-pinned)
	Digest   string // e.g. "sha256:abc123" (empty if tag)
	File     string // e.g. "helpers.star"
	RefStr   string // OCI reference portion for go-containerregistry, e.g. "ghcr.io/org/modules:v1"
}

// ParseOCILoadTarget parses an oci:// URL into its components.
//
// Accepted formats:
//
//	oci://registry/repo:tag/file.star
//	oci://registry/repo@sha256:hex/file.star
//
// Returns an error for:
//   - Missing oci:// prefix
//   - Missing tag or digest (implicit :latest is not supported)
//   - File path not ending in .star
//   - Missing file path component
func ParseOCILoadTarget(raw string) (*OCILoadTarget, error) {
	const prefix = "oci://"
	if !strings.HasPrefix(raw, prefix) {
		return nil, fmt.Errorf("OCI load target must start with oci:// prefix, got %q", raw)
	}

	remainder := strings.TrimPrefix(raw, prefix)
	if remainder == "" {
		return nil, fmt.Errorf("OCI load target %q missing file path after oci://", raw)
	}

	// Split the ref portion from the file portion.
	// The file is always the last path segment ending in .star.
	refStr, file, err := splitRefAndFile(remainder)
	if err != nil {
		return nil, fmt.Errorf("parsing OCI load target %q: %w", raw, err)
	}

	if !strings.HasSuffix(file, ".star") {
		return nil, fmt.Errorf("OCI load target file %q must end with .star", file)
	}

	// Detect whether this is a digest or tag reference.
	target := &OCILoadTarget{
		RawURL: raw,
		File:   file,
	}

	switch {
	case strings.Contains(refStr, "@sha256:"):
		// Digest reference.
		ref, err := name.NewDigest(refStr, name.StrictValidation)
		if err != nil {
			return nil, fmt.Errorf("parsing OCI digest reference %q: %w", refStr, err)
		}
		target.Registry = ref.Context().RegistryStr()
		target.Repo = ref.Context().RepositoryStr()
		target.Digest = ref.DigestStr()
		target.RefStr = ref.String()
	case strings.Contains(refStr, ":"):
		// Tag reference: the portion after the last ":" that doesn't contain "/" is the tag.
		ref, err := name.NewTag(refStr, name.StrictValidation)
		if err != nil {
			return nil, fmt.Errorf("parsing OCI tag reference %q: %w", refStr, err)
		}
		target.Registry = ref.Context().RegistryStr()
		target.Repo = ref.Context().RepositoryStr()
		target.Tag = ref.TagStr()
		target.RefStr = ref.String()
	default:
		return nil, fmt.Errorf(
			"OCI load target %q: tag or digest required; use explicit :tag or @sha256:digest (implicit :latest is not supported)",
			raw,
		)
	}

	return target, nil
}

// IsDefaultRegistryTarget returns true if the module string is a short-form
// load target that requires expansion via a default registry.
// Detection rules (in order):
//  1. starts with "oci://" -> false (explicit full OCI)
//  2. contains ":" or "@sha256:" -> true (default-registry package)
//  3. otherwise -> false (local module)
func IsDefaultRegistryTarget(module string) bool {
	if strings.HasPrefix(module, "oci://") {
		return false
	}
	return strings.Contains(module, ":") || strings.Contains(module, "@sha256:")
}

// ExpandDefaultRegistry expands a short-form load target to a full oci:// URL.
// target is like "function-starlark-stdlib:v1/naming.star"
// registry is like "ghcr.io/wompipomp"
// Returns "oci://ghcr.io/wompipomp/function-starlark-stdlib:v1/naming.star"
func ExpandDefaultRegistry(target, registry string) (string, error) {
	if registry == "" {
		return "", fmt.Errorf(
			"load target %q requires a default OCI registry; "+
				"set STARLARK_OCI_DEFAULT_REGISTRY env var on the function pod "+
				"or spec.ociDefaultRegistry in function input",
			target,
		)
	}
	return "oci://" + registry + "/" + target, nil
}

// IsPackageLocalTarget returns true if the module string is a package-local
// load target of the form "./file.star" — a flat sibling reference inside the
// caller's OCI artifact. Only flat paths are accepted (no subdirectories, no
// parent references).
//
// Detection rules:
//  1. must start with "./"
//  2. must end with ".star"
//  3. must NOT contain "/" after the "./" prefix (flat only; "./sub/x.star" is false)
//
// Note: "./" alone returns false because it lacks the .star suffix.
func IsPackageLocalTarget(module string) bool {
	if !strings.HasPrefix(module, "./") {
		return false
	}
	if !strings.HasSuffix(module, ".star") {
		return false
	}
	return !strings.Contains(module[2:], "/")
}

// ExpandPackageLocal expands a package-local target like "./file.star" to a
// full oci:// URL rooted at the caller's OCI artifact reference.
//
// parentRefStr is the OCI reference portion (e.g. "ghcr.io/org/mod:v1" or
// "ghcr.io/org/mod@sha256:..."). It must be non-empty and parseable as either
// a tag or digest reference (strict validation).
//
// Returns e.g. "oci://ghcr.io/org/mod:v1/file.star".
func ExpandPackageLocal(target, parentRefStr string) (string, error) {
	if parentRefStr == "" {
		return "", fmt.Errorf(
			"package-local load %q requires an OCI parent; "+
				"package-local loads are only valid from OCI modules",
			target,
		)
	}
	if !strings.HasPrefix(target, "./") {
		return "", fmt.Errorf("package-local load %q must start with \"./\"", target)
	}
	inner := strings.TrimPrefix(target, "./")
	if strings.Contains(inner, "/") {
		return "", fmt.Errorf("package-local load %q must be flat (no subdirectories)", target)
	}
	if !strings.HasSuffix(inner, ".star") {
		return "", fmt.Errorf("package-local load %q must end with .star", target)
	}

	// Validate the parent reference as either a tag or digest ref
	// (defense-in-depth; scanner/loader should have canonical refs already).
	if strings.Contains(parentRefStr, "@sha256:") {
		if _, err := name.NewDigest(parentRefStr, name.StrictValidation); err != nil {
			return "", fmt.Errorf("parsing OCI parent %q for package-local load %q: %w", parentRefStr, target, err)
		}
	} else if strings.Contains(parentRefStr, ":") {
		if _, err := name.NewTag(parentRefStr, name.StrictValidation); err != nil {
			return "", fmt.Errorf("parsing OCI parent %q for package-local load %q: %w", parentRefStr, target, err)
		}
	} else {
		return "", fmt.Errorf(
			"OCI parent %q for package-local load %q: tag or digest required; "+
				"use explicit :tag or @sha256:digest (implicit :latest is not supported)",
			parentRefStr, target,
		)
	}

	return "oci://" + parentRefStr + "/" + inner, nil
}

// NormalizeRegistry cleans a registry value by stripping oci:// prefix and trailing slashes.
func NormalizeRegistry(registry string) string {
	r := strings.TrimPrefix(registry, "oci://")
	return strings.TrimRight(r, "/")
}

// ValidateRegistry checks that a registry string is a valid OCI registry+namespace.
// Uses go-containerregistry's name.NewRepository with strict validation.
func ValidateRegistry(registry string) error {
	_, err := name.NewRepository(registry+"/validate", name.StrictValidation)
	if err != nil {
		return fmt.Errorf("invalid default OCI registry %q: %w", registry, err)
	}
	return nil
}

// splitRefAndFile separates the OCI reference portion from the file path.
//
// For tag refs like "ghcr.io/org/modules:v1/helpers.star":
//   - The tag (:v1) divides the ref portion from the subsequent path.
//   - After the tag, the first "/" separates tag from file.
//
// For digest refs like "ghcr.io/org/modules@sha256:hex/helpers.star":
//   - The @sha256: marks the digest. The hex runs until the next "/".
//
// For tagless refs like "ghcr.io/org/modules/helpers.star":
//   - No tag or digest marker found; error.
func splitRefAndFile(s string) (ref, file string, err error) {
	// Handle digest references: find @sha256: and then the / after the hex.
	if idx := strings.Index(s, "@sha256:"); idx != -1 {
		afterDigest := s[idx+len("@sha256:"):]
		slashIdx := strings.Index(afterDigest, "/")
		if slashIdx == -1 {
			return "", "", fmt.Errorf("missing file path after digest")
		}
		ref = s[:idx+len("@sha256:")+slashIdx]
		file = afterDigest[slashIdx+1:]
		if file == "" {
			return "", "", fmt.Errorf("missing file path after digest")
		}
		return ref, file, nil
	}

	// Handle tag references: find the last ":" that could be a tag separator.
	// A tag separator ":" is NOT inside a host:port pattern.
	// Strategy: find ":" positions, try from the rightmost. The tag is between
	// ":" and the next "/".
	lastColon := strings.LastIndex(s, ":")
	if lastColon == -1 {
		// No tag and no digest -- tagless.
		// Check if there's at least a .star file at the end.
		lastSlash := strings.LastIndex(s, "/")
		if lastSlash == -1 || !strings.HasSuffix(s, ".star") {
			return "", "", fmt.Errorf("missing file path")
		}
		// Tagless reference with a file path: this is invalid.
		return "", "", fmt.Errorf("tag or digest required")
	}

	// After the colon, find the first "/" -- that separates tag from file.
	afterColon := s[lastColon+1:]
	slashIdx := strings.Index(afterColon, "/")
	if slashIdx == -1 {
		// No "/" after tag means no file path.
		return "", "", fmt.Errorf("missing file path after tag")
	}

	ref = s[:lastColon+1+slashIdx]
	file = afterColon[slashIdx+1:]
	if file == "" {
		return "", "", fmt.Errorf("missing file path after tag")
	}

	return ref, file, nil
}
