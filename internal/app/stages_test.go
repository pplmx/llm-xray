package app

import "testing"

// TestClampIndexContract pins all four answers and the trap in the n == 0 one.
//
// The function has no test. Three of its four branches are covered indirectly — deleting the
// i < 0 or the i >= n clamp makes other tests fail — but the n == 0 branch is unreachable from
// the app: every call site sits behind a `len(tokens) == 0` return, so nothing exercises it and
// deleting it changes no test.
//
// It matters anyway because of how one call site reads:
//
//	if i := clampIndex(a.SelToken, len(tr.Tokens)); i >= 0 {
//
// With no tokens that condition is *true* (the result is 0, not -1), so the body would index an
// empty slice. It is safe only because stage() returns the empty-prompt card before reaching the
// stage functions. The test states both halves: what the function answers, and that the idiom is
// not a guard against an empty trace.
func TestClampIndexContract(t *testing.T) {
	cases := []struct{ i, n, want int }{
		{5, 0, 0},   // n == 0: not a valid index, but not -1 either
		{-3, 0, 0},  // both sides empty, same answer
		{-1, 5, 0},  // negative clamps to the front
		{0, 5, 0},   // already in range
		{4, 5, 4},   // last valid index
		{5, 5, 4},   // one past the end clamps back
		{999, 5, 4}, // far past the end
	}
	for _, c := range cases {
		if got := clampIndex(c.i, c.n); got != c.want {
			t.Fatalf("clampIndex(%d, %d) = %d, want %d", c.i, c.n, got, c.want)
		}
	}

	// The trap, asserted rather than described: the common idiom does not skip an empty trace.
	if i := clampIndex(-1, 0); i < 0 {
		t.Fatalf("clampIndex(-1, 0) = %d; it answers 0, and stages that rely on `i >= 0` to mean "+
			"\"there are tokens\" are wrong — they are safe only because stage() returns first", i)
	}
}
