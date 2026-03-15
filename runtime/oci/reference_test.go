package oci

import (
	"testing"
)

func TestParseOCILoadTarget(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		want    *OCILoadTarget
		wantErr string
	}{
		{
			name: "tag reference",
			raw:  "oci://ghcr.io/org/modules:v1/helpers.star",
			want: &OCILoadTarget{
				RawURL:   "oci://ghcr.io/org/modules:v1/helpers.star",
				Registry: "ghcr.io",
				Repo:     "org/modules",
				Tag:      "v1",
				File:     "helpers.star",
				RefStr:   "ghcr.io/org/modules:v1",
			},
		},
		{
			name: "digest reference",
			raw:  "oci://ghcr.io/org/modules@sha256:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855/helpers.star",
			want: &OCILoadTarget{
				RawURL:   "oci://ghcr.io/org/modules@sha256:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855/helpers.star",
				Registry: "ghcr.io",
				Repo:     "org/modules",
				Digest:   "sha256:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
				File:     "helpers.star",
				RefStr:   "ghcr.io/org/modules@sha256:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
			},
		},
		{
			name: "nested repo with tag",
			raw:  "oci://ghcr.io/org/deep/nested/repo:v2/lib.star",
			want: &OCILoadTarget{
				RawURL:   "oci://ghcr.io/org/deep/nested/repo:v2/lib.star",
				Registry: "ghcr.io",
				Repo:     "org/deep/nested/repo",
				Tag:      "v2",
				File:     "lib.star",
				RefStr:   "ghcr.io/org/deep/nested/repo:v2",
			},
		},
		{
			name:    "tagless reference rejected",
			raw:     "oci://ghcr.io/org/modules/helpers.star",
			wantErr: "tag or digest required",
		},
		{
			name:    "non-star file rejected",
			raw:     "oci://ghcr.io/org/modules:v1/readme.txt",
			wantErr: "must end with .star",
		},
		{
			name:    "wrong scheme rejected",
			raw:     "not-oci://foo",
			wantErr: "oci://",
		},
		{
			name:    "missing file path",
			raw:     "oci://ghcr.io/org/modules:v1",
			wantErr: "file path",
		},
		{
			name: "registry with port",
			raw:  "oci://localhost:5000/myrepo:latest/mod.star",
			want: &OCILoadTarget{
				RawURL:   "oci://localhost:5000/myrepo:latest/mod.star",
				Registry: "localhost:5000",
				Repo:     "myrepo",
				Tag:      "latest",
				File:     "mod.star",
				RefStr:   "localhost:5000/myrepo:latest",
			},
		},
		{
			name: "single-level repo",
			raw:  "oci://docker.io/library/starlark:v1/init.star",
			want: &OCILoadTarget{
				RawURL:   "oci://docker.io/library/starlark:v1/init.star",
				Registry: "index.docker.io", // go-containerregistry normalizes docker.io
				Repo:     "library/starlark",
				Tag:      "v1",
				File:     "init.star",
				RefStr:   "docker.io/library/starlark:v1", // String() keeps short form
			},
		},
		{
			name:    "empty string",
			raw:     "",
			wantErr: "oci://",
		},
		{
			name:    "oci prefix only",
			raw:     "oci://",
			wantErr: "file path",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseOCILoadTarget(tt.raw)
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
			if got.RawURL != tt.want.RawURL {
				t.Errorf("RawURL = %q, want %q", got.RawURL, tt.want.RawURL)
			}
			if got.Registry != tt.want.Registry {
				t.Errorf("Registry = %q, want %q", got.Registry, tt.want.Registry)
			}
			if got.Repo != tt.want.Repo {
				t.Errorf("Repo = %q, want %q", got.Repo, tt.want.Repo)
			}
			if got.Tag != tt.want.Tag {
				t.Errorf("Tag = %q, want %q", got.Tag, tt.want.Tag)
			}
			if got.Digest != tt.want.Digest {
				t.Errorf("Digest = %q, want %q", got.Digest, tt.want.Digest)
			}
			if got.File != tt.want.File {
				t.Errorf("File = %q, want %q", got.File, tt.want.File)
			}
			if got.RefStr != tt.want.RefStr {
				t.Errorf("RefStr = %q, want %q", got.RefStr, tt.want.RefStr)
			}
		})
	}
}

func containsSubstring(s, substr string) bool {
	return len(s) >= len(substr) && searchSubstring(s, substr)
}

func searchSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
