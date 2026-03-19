package schema

import "testing"

func TestLevenshteinEmptyStrings(t *testing.T) {
	if d := levenshtein("", ""); d != 0 {
		t.Errorf("levenshtein('', '') = %d, want 0", d)
	}
}

func TestLevenshteinFirstEmpty(t *testing.T) {
	if d := levenshtein("", "abc"); d != 3 {
		t.Errorf("levenshtein('', 'abc') = %d, want 3", d)
	}
}

func TestLevenshteinSecondEmpty(t *testing.T) {
	if d := levenshtein("abc", ""); d != 3 {
		t.Errorf("levenshtein('abc', '') = %d, want 3", d)
	}
}

func TestLevenshteinKittenSitting(t *testing.T) {
	if d := levenshtein("kitten", "sitting"); d != 3 {
		t.Errorf("levenshtein('kitten', 'sitting') = %d, want 3", d)
	}
}

func TestLevenshteinIdentical(t *testing.T) {
	if d := levenshtein("same", "same"); d != 0 {
		t.Errorf("levenshtein('same', 'same') = %d, want 0", d)
	}
}

func TestLevenshteinSingleChar(t *testing.T) {
	if d := levenshtein("a", "b"); d != 1 {
		t.Errorf("levenshtein('a', 'b') = %d, want 1", d)
	}
}

func TestLevenshteinTable(t *testing.T) {
	tests := []struct {
		a, b string
		want int
	}{
		{"", "", 0},
		{"a", "", 1},
		{"", "a", 1},
		{"abc", "abc", 0},
		{"abc", "abd", 1},
		{"abc", "abcd", 1},
		{"abcd", "abc", 1},
		{"sunday", "saturday", 3},
	}
	for _, tt := range tests {
		got := levenshtein(tt.a, tt.b)
		if got != tt.want {
			t.Errorf("levenshtein(%q, %q) = %d, want %d", tt.a, tt.b, got, tt.want)
		}
	}
}

func TestSuggestCloseMatch(t *testing.T) {
	got := Suggest("resourceGroupNme", []string{"location", "resourceGroupName", "sku", "tags"})
	if got != "resourceGroupName" {
		t.Errorf("Suggest('resourceGroupNme', ...) = %q, want 'resourceGroupName'", got)
	}
}

func TestSuggestLocationTypo(t *testing.T) {
	got := Suggest("locaiton", []string{"location", "resourceGroupName", "sku"})
	if got != "location" {
		t.Errorf("Suggest('locaiton', ...) = %q, want 'location'", got)
	}
}

func TestSuggestTooFar(t *testing.T) {
	got := Suggest("xyzzy", []string{"location", "resourceGroupName", "sku"})
	if got != "" {
		t.Errorf("Suggest('xyzzy', ...) = %q, want empty", got)
	}
}

func TestSuggestShortInput(t *testing.T) {
	got := Suggest("a", []string{"ab", "cd"})
	if got != "ab" {
		t.Errorf("Suggest('a', ...) = %q, want 'ab'", got)
	}
}

func TestSuggestShortNoMatch(t *testing.T) {
	got := Suggest("sku", []string{"sku_name", "location"})
	if got != "" {
		t.Errorf("Suggest('sku', ...) = %q, want empty", got)
	}
}

func TestSuggestEmptyValid(t *testing.T) {
	got := Suggest("anything", nil)
	if got != "" {
		t.Errorf("Suggest with empty valid list = %q, want empty", got)
	}
}

func TestSuggestExactMatch(t *testing.T) {
	got := Suggest("location", []string{"location", "sku"})
	if got != "location" {
		t.Errorf("Suggest('location', ...) = %q, want 'location'", got)
	}
}
