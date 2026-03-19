package schema

// levenshtein computes the edit distance between two strings using the
// standard dynamic programming algorithm with two-row optimization.
func levenshtein(a, b string) int {
	la, lb := len(a), len(b)
	if la == 0 {
		return lb
	}
	if lb == 0 {
		return la
	}

	prev := make([]int, lb+1)
	curr := make([]int, lb+1)
	for j := 0; j <= lb; j++ {
		prev[j] = j
	}

	for i := 1; i <= la; i++ {
		curr[0] = i
		for j := 1; j <= lb; j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			curr[j] = min(curr[j-1]+1, min(prev[j]+1, prev[j-1]+cost))
		}
		prev, curr = curr, prev
	}
	return prev[lb]
}

// Suggest returns the closest match from valid if within a distance threshold,
// or "" if no match is close enough. The threshold is max(1, len(unknown)/3).
func Suggest(unknown string, valid []string) string {
	if len(valid) == 0 {
		return ""
	}
	threshold := max(1, len(unknown)/3)
	best := ""
	bestDist := threshold + 1
	for _, v := range valid {
		d := levenshtein(unknown, v)
		if d < bestDist {
			best = v
			bestDist = d
		}
	}
	if bestDist <= threshold {
		return best
	}
	return ""
}
