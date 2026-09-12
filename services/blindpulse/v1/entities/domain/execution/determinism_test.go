package execution

import "testing"

// NFR-03. The product's claim is proof-of-skill, and a ledger nobody can recompute attests to
// nothing — so a disputed fill has to be reproducible rather than arguable.

func TestTheSameCoordinatesAlwaysDrawTheSameSlippage(t *testing.T) {
	first := SlippageDraw(42, 214, 3, 2)
	for i := 0; i < 100; i++ {
		if got := SlippageDraw(42, 214, 3, 2); got != first {
			t.Fatalf("draw %d returned %d, want %d — the draw is not a function of its coordinates", i, got, first)
		}
	}
}

// The reason the draw hashes its coordinates rather than pulling from a stream: a generator's
// position would make a fill depend on how many orders happened to be processed before it, so
// replaying a session in a different order — or re-running one order on its own — would produce a
// different price, and the recomputation would disagree with the ledger it was checking.
func TestADrawDoesNotDependOnHowManyCameBeforeIt(t *testing.T) {
	direct := SlippageDraw(42, 214, 7, 3)
	for sequence := 0; sequence < 7; sequence++ {
		_ = SlippageDraw(42, 214, sequence, 3)
	}
	if after := SlippageDraw(42, 214, 7, 3); after != direct {
		t.Errorf("drawing in sequence gave %d, drawing alone gave %d", after, direct)
	}
}

func TestEachCoordinateChangesTheDraw(t *testing.T) {
	// Not a claim about any one pair — a hash can collide — but about the set: if a coordinate were
	// being ignored entirely, every draw along that axis would be identical.
	distinct := func(values []int) bool {
		seen := map[int]struct{}{}
		for _, value := range values {
			seen[value] = struct{}{}
		}
		return len(seen) > 1
	}
	bySeed := make([]int, 0, 32)
	byBar := make([]int, 0, 32)
	bySequence := make([]int, 0, 32)
	for i := 0; i < 32; i++ {
		bySeed = append(bySeed, SlippageDraw(int64(i), 214, 3, 5))
		byBar = append(byBar, SlippageDraw(42, i, 3, 5))
		bySequence = append(bySequence, SlippageDraw(42, 214, i, 5))
	}
	if !distinct(bySeed) {
		t.Error("the seed does not affect the draw")
	}
	if !distinct(byBar) {
		t.Error("the bar index does not affect the draw")
	}
	if !distinct(bySequence) {
		t.Error("the order sequence does not affect the draw")
	}
}

func TestTheDrawStaysInsideItsBound(t *testing.T) {
	for maxTicks := 0; maxTicks <= 5; maxTicks++ {
		for i := 0; i < 500; i++ {
			got := SlippageDraw(int64(i), i*7, i%11, maxTicks)
			if got < 0 || got > maxTicks {
				t.Fatalf("draw %d is outside [0,%d]", got, maxTicks)
			}
		}
	}
	// Zero max means no slippage — a teaching feed's setting, and it must be exactly zero rather
	// than "usually zero".
	for i := 0; i < 100; i++ {
		if got := SlippageDraw(int64(i), i, i, 0); got != 0 {
			t.Fatalf("draw with no slippage allowed returned %d", got)
		}
	}
}

// A draw that only ever returned its floor would make slippage a constant, which is not the same
// model and would be invisible in every other test here.
func TestTheDrawActuallyVaries(t *testing.T) {
	counts := map[int]int{}
	for i := 0; i < 600; i++ {
		counts[SlippageDraw(99, i, 0, 2)]++
	}
	for ticks := 0; ticks <= 2; ticks++ {
		if counts[ticks] == 0 {
			t.Errorf("%d ticks never came up in 600 draws", ticks)
		}
	}
}

// A negative seed is legitimate — the session seed is drawn from a CSPRNG into an int64 — and the
// modulo of a negative number is where an implementation quietly starts returning negative
// slippage, which would pay the trader.
func TestANegativeSeedStillDrawsInRange(t *testing.T) {
	for _, seed := range []int64{-1, -42, -9223372036854775808} {
		for bar := 0; bar < 50; bar++ {
			if got := SlippageDraw(seed, bar, 0, 3); got < 0 || got > 3 {
				t.Fatalf("seed %d bar %d drew %d, outside [0,3]", seed, bar, got)
			}
		}
	}
}
