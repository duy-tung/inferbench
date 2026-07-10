package workload

import (
	"math/rand/v2"
	"strings"
)

// promptStream is the PRNG stream selector for prompt text (see
// schedule.Build for the arrival/length stream selectors); prefixStream
// derives the shared-prefix text per prefix group.
const (
	promptStream = 0x50524f4d50543031 // "PROMPT01"
	prefixStream = 0x5052464958543031 // "PRFXT01" + pad
)

// Prompt builds the deterministic synthetic prompt for one workload item:
// a sequence of dictionary words sized to approximately targetTokens under
// a ~4-bytes-per-token estimate. Prompts are synthetic by construction —
// never real user data (docs/security.md). The honest input token count in
// raw events comes from the target's usage payload, not from this estimate.
//
// The per-item PRNG is derived from (seed, item index) only, so prompt
// content is independent of scheduling and iteration order.
func Prompt(seed int64, item int, targetTokens int) string {
	rng := rand.New(rand.NewPCG(uint64(seed)^promptStream, uint64(item)))
	// ~4 bytes/token; subtract a small fixed message overhead so short
	// prompts do not overshoot.
	return fillWords(rng, targetTokens*4-28)
}

// SharedPrompt builds the deterministic prompt for a prefix-sharing item
// (IB-T004, workload.schema.json prefix_sharing): a shared prefix derived
// from (seed, group) only — byte-identical for every item in the group, so
// prefix caching is exercised by construction — followed by a unique suffix
// derived from (seed, item), sized so the whole prompt approximates
// targetTokens. The suffix is never empty: every sharing request is a
// distinct request (the sharing ratio is a controlled variable, and a fully
// duplicated prompt would conflate prefix caching with response caching).
func SharedPrompt(seed int64, group, prefixTokens, item, targetTokens int) string {
	prefixRNG := rand.New(rand.NewPCG(uint64(seed)^prefixStream, uint64(group)))
	prefix := fillWords(prefixRNG, prefixTokens*4-28)

	suffixTokens := targetTokens - prefixTokens
	if suffixTokens < 1 {
		suffixTokens = 1
	}
	suffixRNG := rand.New(rand.NewPCG(uint64(seed)^promptStream, uint64(item)))
	return prefix + " " + fillWords(suffixRNG, suffixTokens*4)
}

// fillWords draws dictionary words until the text reaches targetBytes.
func fillWords(rng *rand.Rand, targetBytes int) string {
	if targetBytes < 4 {
		targetBytes = 4
	}
	var b strings.Builder
	b.Grow(targetBytes + 8)
	for b.Len() < targetBytes {
		if b.Len() > 0 {
			b.WriteByte(' ')
		}
		b.WriteString(promptWords[rng.IntN(len(promptWords))])
	}
	return b.String()
}

// promptWords is the deterministic synthetic vocabulary (average ~6 bytes).
var promptWords = []string{
	"amber", "basin", "cedar", "delta", "ember", "fable", "grove", "haven",
	"inlet", "jade", "kiln", "larch", "maple", "north", "ochre", "pine",
	"quill", "reef", "slate", "thorn", "umbra", "vale", "wharf", "xylem",
	"yarn", "zinc", "arch", "bluff", "crest", "dune", "eddy", "ford",
	"glen", "heath", "isle", "jetty", "knot", "loch", "moor", "notch",
	"oxbow", "peak", "quay", "ridge", "shoal", "tarn", "vault", "weir",
}
