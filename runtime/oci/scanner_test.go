package oci

import (
	"testing"
)

func TestScanForOCILoads(t *testing.T) {
	tests := []struct {
		name            string
		source          string
		inlineModules   map[string]string
		defaultRegistry string
		wantCount       int
		wantRefs        []string // expected RefStr values
		wantErr         string
	}{
		{
			name:      "single oci load",
			source:    `load("oci://ghcr.io/org/lib:v1/h.star", "fn")`,
			wantCount: 1,
			wantRefs:  []string{"ghcr.io/org/lib:v1"},
		},
		{
			name:      "local load ignored",
			source:    `load("local.star", "fn")`,
			wantCount: 0,
		},
		{
			name: "dedup same artifact same file",
			source: `load("oci://ghcr.io/org/lib:v1/h.star", "fn1")
load("oci://ghcr.io/org/lib:v1/h.star", "fn2")`,
			wantCount: 1,
			wantRefs:  []string{"ghcr.io/org/lib:v1"},
		},
		{
			name: "dedup same artifact different files",
			source: `load("oci://ghcr.io/org/lib:v1/a.star", "fn1")
load("oci://ghcr.io/org/lib:v1/b.star", "fn2")`,
			wantCount: 1,
			wantRefs:  []string{"ghcr.io/org/lib:v1"},
		},
		{
			name:   "inline modules scanned too",
			source: `x = 1`,
			inlineModules: map[string]string{
				"helper.star": `load("oci://ghcr.io/org/lib:v1/h.star", "fn")`,
			},
			wantCount: 1,
			wantRefs:  []string{"ghcr.io/org/lib:v1"},
		},
		{
			name:   "targets from both main and inline",
			source: `load("oci://ghcr.io/org/lib:v1/a.star", "fn1")`,
			inlineModules: map[string]string{
				"helper.star": `load("oci://ghcr.io/other/lib:v2/b.star", "fn2")`,
			},
			wantCount: 2,
			wantRefs:  []string{"ghcr.io/org/lib:v1", "ghcr.io/other/lib:v2"},
		},
		{
			name:    "invalid starlark syntax",
			source:  `this is not valid starlark @@@@`,
			wantErr: "parsing",
		},
		{
			name: "mixed local and oci loads",
			source: `load("local.star", "fn1")
load("oci://ghcr.io/org/lib:v1/h.star", "fn2")
load("other.star", "fn3")`,
			wantCount: 1,
			wantRefs:  []string{"ghcr.io/org/lib:v1"},
		},
		// --- New default registry test cases ---
		{
			name:            "short-form with default registry",
			source:          `load("function-starlark-stdlib:v1/naming.star", "x")`,
			defaultRegistry: "ghcr.io/wompipomp",
			wantCount:       1,
			wantRefs:        []string{"ghcr.io/wompipomp/function-starlark-stdlib:v1"},
		},
		{
			name:            "short-form without default registry errors",
			source:          `load("function-starlark-stdlib:v1/naming.star", "x")`,
			defaultRegistry: "",
			wantErr:         "requires a default OCI registry",
		},
		{
			name:            "short-form digest with default registry",
			source:          `load("pkg@sha256:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855/file.star", "x")`,
			defaultRegistry: "ghcr.io/org",
			wantCount:       1,
			wantRefs:        []string{"ghcr.io/org/pkg@sha256:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"},
		},
		{
			name:            "explicit oci:// unaffected by default registry",
			source:          `load("oci://ghcr.io/org/lib:v1/h.star", "fn")`,
			defaultRegistry: "other.registry.io/ns",
			wantCount:       1,
			wantRefs:        []string{"ghcr.io/org/lib:v1"},
		},
		{
			name:            "local module unaffected by default registry",
			source:          `load("local.star", "fn")`,
			defaultRegistry: "ghcr.io/wompipomp",
			wantCount:       0,
		},
		{
			name: "mixed short-form and oci://",
			source: `load("function-starlark-stdlib:v1/naming.star", "x")
load("oci://ghcr.io/org/lib:v1/h.star", "fn")`,
			defaultRegistry: "ghcr.io/wompipomp",
			wantCount:       2,
			wantRefs:        []string{"ghcr.io/wompipomp/function-starlark-stdlib:v1", "ghcr.io/org/lib:v1"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ScanForOCILoads(tt.source, tt.inlineModules, tt.defaultRegistry)
			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tt.wantErr)
				}
				if !containsSubstring(err.Error(), tt.wantErr) {
					t.Fatalf("error %q does not contain %q", err.Error(), tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(got) != tt.wantCount {
				t.Fatalf("got %d targets, want %d", len(got), tt.wantCount)
			}
			for _, wantRef := range tt.wantRefs {
				found := false
				for _, target := range got {
					if target.RefStr == wantRef {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("expected target with RefStr %q not found in results", wantRef)
				}
			}
		})
	}
}
