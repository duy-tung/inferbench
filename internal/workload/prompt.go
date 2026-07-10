package workload

import (
	"math/rand/v2"
	"strings"
)

// promptStream is the PRNG stream selector for prompt text (see
// schedule.Build for the arrival/length stream selectors).
const promptStream = 0x50524f4d50543031 // "PROMPT01"

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
	targetBytes := targetTokens*4 - 28
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
