package normalize

// topicmatch.go implements the near-duplicate story gate for clustering (§159
// level 5). Exact fingerprints (TitleFingerprint, ContentHash) only catch
// byte-identical reports; independent publishers reword the same story, so the
// cluster gate also needs a fuzzy signal.
//
// Short news titles make bit-level hashes (SimHash) unreliable: with six or so
// tokens a single swapped word flips dozens of bits, so rewrites and unrelated
// stories become statistically indistinguishable. Token-set overlap is the right
// tool at this length. Two gates run together to keep the rule honest:
//
//   - containment = shared / min(|A|,|B|)  >= containmentMin
//     (one title covers nearly all of the other's content words), and
//   - shared tokens >= sharedMin
//     (a short title sharing only a preposition and a city name must not adopt
//     an unrelated long story).
//
// Both sides must already be NormalizedTitle output so ZWNJ, diacritics, digit
// variants and punctuation cannot defeat the comparison.

import "strings"

const (
	// containmentMin is the required fraction of the smaller title's content
	// tokens present in the other title.
	containmentMin = 0.6
	// sharedMin is the absolute floor of shared content tokens. It defeats the
	// short-title trap where two generic words (e.g. a preposition plus a
	// province name) satisfy containment on their own.
	sharedMin = 4
)

// nearDuplicateTitle reports whether two normalized titles describe the same
// story under the containment + shared-floor gates.
func NearDuplicateTitle(aNormalized, bNormalized string) bool {
	a := TopicTokens(aNormalized)
	b := TopicTokens(bNormalized)
	if len(a) == 0 || len(b) == 0 {
		return false
	}
	small, large := a, b
	if len(large) < len(small) {
		small, large = large, small
	}
	shared := 0
	for tok := range small {
		if _, ok := large[tok]; ok {
			shared++
		}
	}
	return shared >= sharedMin && float64(shared)/float64(len(small)) >= containmentMin
}

// TopicTokens renders a normalized title as its content-token set. Empty tokens
// are dropped so stray whitespace cannot inflate counts.
func TopicTokens(normalizedTitle string) map[string]struct{} {
	fields := strings.Fields(normalizedTitle)
	tokens := make(map[string]struct{}, len(fields))
	for _, f := range fields {
		if f != "" {
			tokens[f] = struct{}{}
		}
	}
	return tokens
}
