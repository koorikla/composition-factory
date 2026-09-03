package blueprint

import (
	"fmt"
	"sort"
	"strings"
)

// ClosestPath returns the candidate string closest in edit distance to target,
// provided the edit distance is within reasonable bounds (dist <= 3 and dist*2 < len(target)).
func ClosestPath(target string, candidates []string) string {
	best, bestDist := "", 0
	for _, c := range candidates {
		d := editDistance(target, c)
		if best == "" || d < bestDist {
			best, bestDist = c, d
		}
	}
	if best != "" && bestDist <= 3 && bestDist*2 < len(target) {
		return best
	}

	// For nested paths (e.g. "spec.selector.machLabels"), check candidates sharing the parent prefix
	if idx := strings.LastIndex(target, "."); idx != -1 {
		prefix := target[:idx]
		leaf := target[idx+1:]
		var leafCandidates []string
		prefixDot := prefix + "."
		for _, c := range candidates {
			if strings.HasPrefix(c, prefixDot) {
				leafCandidates = append(leafCandidates, strings.TrimPrefix(c, prefixDot))
			}
		}
		if len(leafCandidates) > 0 {
			bestLeaf := ClosestPath(leaf, leafCandidates)
			if bestLeaf != "" {
				return prefix + "." + bestLeaf
			}
		}
	}

	return ""
}

// editDistance is Levenshtein distance over runes, two rows at a time.
func editDistance(a, b string) int {
	ar, br := []rune(a), []rune(b)
	prev := make([]int, len(br)+1)
	curr := make([]int, len(br)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(ar); i++ {
		curr[0] = i
		for j := 1; j <= len(br); j++ {
			cost := 1
			if ar[i-1] == br[j-1] {
				cost = 0
			}
			curr[j] = min(prev[j]+1, curr[j-1]+1, prev[j-1]+cost)
		}
		prev, curr = curr, prev
	}
	return prev[len(br)]
}

// UnknownEnvKeyError constructs an error for an unknown environment key reference,
// including a nearest-match suggestion if a close match exists among the declared keys.
func UnknownEnvKeyError(context, key string, env map[string]EnvironmentKey) error {
	var candidates []string
	for k := range env {
		candidates = append(candidates, k)
	}
	sort.Strings(candidates)
	if s := ClosestPath(key, candidates); s != "" {
		return fmt.Errorf("%s: references unknown environment key %q; did you mean %q?", context, key, s)
	}
	return fmt.Errorf("%s: references unknown environment key %q", context, key)
}
