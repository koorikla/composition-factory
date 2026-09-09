package blueprint

import (
	"fmt"
	"sort"
	"strings"
)

// ClosestPath returns the candidate string closest in edit distance to target,
// provided the edit distance is within reasonable bounds (dist <= 2 for 3-4 char words,
// dist <= 3 and dist*2 < len(target) for longer words).
func ClosestPath(target string, candidates []string) string {
	best, bestDist := "", 0
	for _, c := range candidates {
		d := editDistance(target, c)
		if best == "" || d < bestDist {
			best, bestDist = c, d
		}
	}
	if best != "" && isCloseMatch(target, bestDist) {
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

// editDistance is Levenshtein distance over runes. It avoids heap allocations
// for typical string lengths using a stack-allocated row buffer.
func editDistance(a, b string) int {
	if a == b {
		return 0
	}
	ar, br := []rune(a), []rune(b)
	if len(ar) == 0 {
		return len(br)
	}
	if len(br) == 0 {
		return len(ar)
	}
	if len(br) > len(ar) {
		ar, br = br, ar
	}

	var stackBuf [64]int
	var row []int
	if len(br)+1 <= len(stackBuf) {
		row = stackBuf[:len(br)+1]
	} else {
		row = make([]int, len(br)+1)
	}

	for j := range row {
		row[j] = j
	}
	for i := 1; i <= len(ar); i++ {
		prevCorner := row[0]
		row[0] = i
		for j := 1; j <= len(br); j++ {
			prevAbove := row[j]
			cost := 1
			if ar[i-1] == br[j-1] {
				cost = 0
			}
			row[j] = min(prevAbove+1, row[j-1]+1, prevCorner+cost)
			prevCorner = prevAbove
		}
	}
	return row[len(br)]
}

func isCloseMatch(target string, dist int) bool {
	if dist <= 0 {
		return true
	}
	if dist > 3 {
		return false
	}
	if len(target) >= 3 && dist <= 2 {
		return true
	}
	return dist*2 < len(target)
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
