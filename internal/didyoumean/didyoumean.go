// Package didyoumean suggests the closest candidate for a mistyped name.
package didyoumean

// Closest returns the candidate nearest to input by edit distance, or the
// empty string when nothing is close enough to be a plausible typo.
func Closest(input string, candidates []string) string {
	best, bestDist := "", -1
	for _, candidate := range candidates {
		d := distance(input, candidate)
		if bestDist < 0 || d < bestDist {
			best, bestDist = candidate, d
		}
	}
	// A third of the input's length, but at least 2, separates a typo from a
	// different word: "claud"→"claude" suggests, "gui"→"git" does not drown
	// out a genuinely unknown name among many near misses of distance 2.
	limit := len(input) / 3
	if limit < 2 {
		limit = 2
	}
	if bestDist < 0 || bestDist > limit {
		return ""
	}
	return best
}

func distance(a, b string) int {
	ra, rb := []rune(a), []rune(b)
	prev := make([]int, len(rb)+1)
	curr := make([]int, len(rb)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(ra); i++ {
		curr[0] = i
		for j := 1; j <= len(rb); j++ {
			cost := 1
			if ra[i-1] == rb[j-1] {
				cost = 0
			}
			curr[j] = min3(prev[j]+1, curr[j-1]+1, prev[j-1]+cost)
		}
		prev, curr = curr, prev
	}
	return prev[len(rb)]
}

func min3(a, b, c int) int {
	if b < a {
		a = b
	}
	if c < a {
		a = c
	}
	return a
}
