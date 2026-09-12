package execution

import (
	"encoding/binary"
	"hash/fnv"
)

// NFR-03's determinism contract.
//
// The promise is that a session can be recomputed: the same feed and the same seed produce the same
// fills, so a disputed result is reproducible rather than arguable. That matters more here than in
// an ordinary simulator, because the product's claim is proof-of-skill — a ledger nobody can
// recompute attests to nothing.
//
// The draw is a pure function of **(seed, bar index, order sequence)** and of nothing else. Not of
// wall-clock time, not of a shared PRNG's position, not of the order rows' database ids. A stream
// of draws from one generator would make a fill depend on how many orders happened to be processed
// before it — so replaying a session in a different order, or re-running one order, would produce a
// different price. Hashing the coordinates instead means any fill can be recomputed on its own.

// SlippageDraw returns how many ticks of slippage an order suffers: a value in [0, max].
//
// Integer arithmetic throughout. Partly the architecture rule — no floats under domain — and partly
// because slippage in whole ticks is what a venue actually produces: a fill lands on the tick grid
// or it is not a fill.
func SlippageDraw(seed int64, barIndex, sequence, maxTicks int) int {
	if maxTicks <= 0 {
		return 0
	}
	hash := fnv.New64a()
	var buffer [8]byte
	binary.BigEndian.PutUint64(buffer[:], uint64(seed))
	_, _ = hash.Write(buffer[:])
	binary.BigEndian.PutUint64(buffer[:], uint64(int64(barIndex)))
	_, _ = hash.Write(buffer[:])
	binary.BigEndian.PutUint64(buffer[:], uint64(int64(sequence)))
	_, _ = hash.Write(buffer[:])
	// Modulo over maxTicks+1 is very slightly biased towards the low end for most values of max.
	// Accepted knowingly: max is a single-digit number of ticks, the bias is in the fourth decimal
	// place of a distribution that is itself a simplification, and rejection sampling would buy
	// nothing a trader could perceive.
	return int(hash.Sum64() % uint64(maxTicks+1))
}
