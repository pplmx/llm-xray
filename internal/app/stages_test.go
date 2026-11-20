package app

import (
	"math"
	"strconv"
	"strings"
	"testing"
)

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

// TestMaxAbsFloatAnchorsTheVectorStrip pins the scale the strip's colours are drawn against.
//
// vectorStrip passes maxAbsFloat(v) to valueColor for every cell, so this one number decides
// whether the strip shows relative magnitude at all. Replacing the body with `return 0` left the
// whole suite green: no test looks at the strip's pixels, and valueColor's degenerate branch
// (maxAbs <= 0 paints every cell colFaint at 0.25) is silent in Texts().
//
// Two things make it worth a unit test rather than a screenshot. The function is pure, so the
// assertions are exact. And zero is a real input: an all-zero vector has maxAbs 0, and painting
// it faint is the intended answer rather than a defect — which is why the degenerate branch
// exists and why the guard has to distinguish "no magnitude" from "forgot to measure".
func TestMaxAbsFloatAnchorsTheVectorStrip(t *testing.T) {
	cases := []struct {
		name string
		in   []float32
		want float32
	}{
		{"empty", nil, 0},
		{"all zero", []float32{0, 0, 0}, 0},
		{"positive", []float32{1, 2, 3}, 3},
		{"negative dominates", []float32{-5, 1, 2}, 5},
		{"symmetric", []float32{-4, 4}, 4},
		{"single negative", []float32{-0.25}, 0.25},
	}
	for _, c := range cases {
		if got := maxAbsFloat(c.in); got != c.want {
			t.Errorf("%s: maxAbsFloat(%v) = %v, want %v", c.name, c.in, got, c.want)
		}
	}

	// And the property the strip depends on: whatever the input, no cell's colour can be
	// saturated by a value this scale did not account for.
	v := []float32{-7.5, 0.4, 3.1, -0.2}
	maxAbs := maxAbsFloat(v)
	for _, x := range v {
		if math.Abs(float64(x)) > float64(maxAbs) {
			t.Fatalf("maxAbsFloat(%v) = %v, smaller than |%v|; valueColor would clamp "+
				"that cell and flatten the rest of the strip", v, maxAbs, x)
		}
	}
}

// TestValueColorIsSymmetricAndDegenerateSafely pins the strip's colour mapping.
//
// valueColor turns a signed value and a scale into an alpha on a red/blue ramp. It had no test:
// the only mentions of its name were in comments. Three properties matter, and each fails
// silently on screen rather than in Texts().
//
//   - Sign picks the hue: positive uses colPos (warm), negative colNeg.
//   - Magnitude maps monotonically onto alpha, saturating at the scale.
//   - A non-positive scale paints every cell faint rather than dividing by zero.
//
// The last one is the interesting case. It is also what an all-zero vector legitimately looks
// like, so the branch has to be reached by a real input and not only by a bug — the test covers
// both: the degenerate scale directly, and a zero vector through maxAbsFloat.
func TestValueColorIsSymmetricAndDegenerateSafely(t *testing.T) {
	const scale = 4

	// A non-positive scale must not reach the division.
	for _, maxAbs := range []float32{0, -1} {
		got := valueColor(3, maxAbs)
		want := colFaint.Alpha(0.25)
		if got != want {
			t.Errorf("valueColor(3, %v) = %+v, want the faint fallback %+v", maxAbs, got, want)
		}
	}

	// Sign picks the ramp.
	if pos, neg := valueColor(2, scale), valueColor(-2, scale); pos == neg {
		t.Fatalf("+2 and -2 share a colour %+v; the strip cannot show sign", pos)
	}

	// Magnitude is monotonic, and saturates at the scale instead of running past it.
	prev := valueColor(0, scale)
	for _, v := range []float32{0.5, 1, 2, 3, 4} {
		cur := valueColor(v, scale)
		if cur.A < prev.A {
			t.Fatalf("alpha fell from %v to %v going from a smaller value to %v; the ramp "+
				"must not invert", prev.A, cur.A, v)
		}
		prev = cur
	}
	// A value at the scale reaches the top of the ramp.
	//
	// Saturation past the scale is NOT asserted, deliberately. A colour's alpha is a byte, so
	// 0.10 + 0.90*t is clamped to 255 by the conversion on its own — measured: deleting
	// valueColor's explicit `if t > 1 { t = 1 }` leaves valueColor(40, 4) byte-identical to
	// valueColor(4, 4), so any assertion written here would pass either way. The explicit clamp
	// keeps the arithmetic in range for a future consumer that wants the ratio rather than the
	// colour; that is a readability choice, and a test that cannot fail does not protect it.
	at := valueColor(scale, scale)
	if at.A != 255 {
		t.Fatalf("a value at the scale gives alpha %v, not the top of the ramp", at.A)
	}

	// And an all-zero vector takes the degenerate branch through the real caller chain.
	if got := valueColor(0, maxAbsFloat([]float32{0, 0, 0})); got != colFaint.Alpha(0.25) {
		t.Fatalf("a zero vector paints %+v; the strip should read as empty, not as an extreme",
			got)
	}
}

// TestCountAgreeExcludesTheFinalRung pins the one thing countAgree has to get right.
//
// The card says "只看这个位置：N / M 个中间层说对了". The final rung is not an intermediate one —
// it is the answer being agreed with — so counting it would inflate N by one every time. That is
// exactly what happened when the `!r.IsFinal` guard was dropped: measured on the bundled weights
// and the default prompt, the count went from 3 to 4 while the card would still say "中间层".
//
// Nothing noticed. The logit-lens tests check the ladder's figures and the README guards its
// numbers, but none of them touches this count, so the whole suite stayed green through a full
// 361s run with the guard removed.
func TestCountAgreeExcludesTheFinalRung(t *testing.T) {
	a := newTestApp(t)
	lens := computeLogitLens(a.W, a.Trace, a.SelToken, a.Temp)

	// Build the count three ways and require the guarded one to be the strictest.
	all, agreeing := 0, 0
	for _, r := range lens.Rungs {
		all++
		if r.AgreesWithFinal && !r.IsFinal {
			agreeing++
		}
	}
	if agreeing == 0 {
		t.Fatalf("no intermediate rung agrees with the final answer, so this test cannot tell "+
			"the guarded count from an unguarded one (%d rungs total)", all)
	}

	if got := countAgree(lens); got != agreeing {
		t.Fatalf("countAgree = %d, but %d intermediate rungs agree with the final answer; the "+
			"card would print a different number", got, agreeing)
	}

	// And the final rung is one of the rungs the lens builds, so the exclusion is doing work
	// rather than guarding against a case that cannot occur.
	var final int
	for _, r := range lens.Rungs {
		if r.IsFinal {
			final++
		}
	}
	if final != 1 {
		t.Fatalf("the lens built %d final rungs; countAgree's exclusion assumes exactly one", final)
	}
}

// TestAgreeNoteNamesAllThreeCases pins the bar tooltip's three answers.
//
// agreeNote returns one of three strings: the final rung, a rung that agrees with it, and one
// that does not. Deleting the first branch — so a final rung gets the plain "与最终答案一致" —
// left the whole suite green across a 361s run. The tooltip is the only place the reader learns
// that a bar is the model's actual output rather than an intermediate guess, so losing that
// distinction changes what the chart says.
func TestAgreeNoteNamesAllThreeCases(t *testing.T) {
	cases := []struct {
		name string
		rung lensRung
		want string
	}{
		{"final rung", lensRung{IsFinal: true, AgreesWithFinal: true}, " · 这是模型真正的输出"},
		{"intermediate agreeing", lensRung{AgreesWithFinal: true}, " · 与最终答案一致"},
		{"intermediate disagreeing", lensRung{}, " · 与最终答案不同"},
	}
	for _, c := range cases {
		if got := agreeNote(c.rung); got != c.want {
			t.Errorf("%s: agreeNote(%+v) = %q, want %q", c.name, c.rung, got, c.want)
		}
	}

	// The three answers have to be three different strings, or the chart cannot distinguish
	// the cases they describe.
	seen := map[string]string{}
	for _, c := range cases {
		got := agreeNote(c.rung)
		if other, dup := seen[got]; dup {
			t.Fatalf("%s and %s both read %q", other, c.name, got)
		}
		seen[got] = c.name
	}
}

// TestUTF8PayloadChainStripsThePrefixBits pins the arithmetic the "一个汉字三个 token" card
// shows.
//
// The card prints "还原码点：(04<<12) + (38<<6) + 2D → U+4E2D". Those numbers are the bytes with
// their UTF-8 prefix bits removed: E4 → 04, B8 → 38, AD → 2D. Printing the raw bytes instead —
// (E4<<12) + (B8<<6) + AD — does not reconstruct U+4E2D at all, so the card would stop
// demonstrating the thing it exists to demonstrate.
//
// Measured: replacing utf8Payload(b) with the bare b in both branches left the whole suite green
// across a 365s run. Nothing checks this string.
func TestUTF8PayloadChainStripsThePrefixBits(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		// 2-, 3- and 4-byte sequences, plus the single-byte case the prefix table handles
		// with a zero shift (so the term prints without a << at all).
		{"a", "61"},
		{"é", "(03<<6) + 29"},                       // C3 A9
		{"中", "(04<<12) + (38<<6) + 2D"},            // E4 B8 AD
		{"𝄞", "(00<<18) + (1D<<12) + (04<<6) + 1E"}, // F0 9D 84 9E
	}
	for _, c := range cases {
		if got := utf8PayloadChain([]byte(c.in)); got != c.want {
			t.Errorf("utf8PayloadChain(%q) = %q, want %q", c.in, got, c.want)
		}
	}

	// The empty and unsupported lengths answer empty rather than a partial sum.
	for _, bs := range [][]byte{nil, {}, {0x41, 0x42, 0x43, 0x44, 0x45}} {
		if got := utf8PayloadChain(bs); got != "" {
			t.Errorf("utf8PayloadChain(% X) = %q, want empty", bs, got)
		}
	}

	// And the property the card actually relies on: evaluating the chain reproduces the
	// codepoint. Parsing the printed expression back is the only way to state that without
	// re-deriving the same payload arithmetic in the test, which would test nothing.
	for _, s := range []string{"a", "é", "中", "𝄞"} {
		var want rune
		for _, r := range s {
			want = r
		}
		chain := utf8PayloadChain([]byte(s))
		sum := evaluatePayloadChain(t, chain)
		if sum != want {
			t.Fatalf("the chain printed for %q evaluates to %U, want %U", s, sum, want)
		}
	}
}

// evaluatePayloadChain parses a chain like "(04<<12) + (38<<6) + 2D" back into a rune.
//
// It is deliberately a small, independent parser rather than a copy of the production
// arithmetic: if the two agreed because they shared code, the test would only prove that the
// shared code runs.
func evaluatePayloadChain(t *testing.T, chain string) rune {
	t.Helper()
	var sum int
	for _, term := range strings.Split(chain, " + ") {
		term = strings.TrimSpace(term)
		if term == "" {
			t.Fatalf("empty term in chain %q", chain)
		}
		shift := 0
		if i := strings.Index(term, "<<"); i >= 0 {
			hexPart := strings.Trim(term[:i], "(")
			s, err := strconv.Atoi(term[i+2 : len(term)-1])
			if err != nil {
				t.Fatalf("cannot read the shift in %q: %v", term, err)
			}
			shift = s
			v, err := strconv.ParseInt(hexPart, 16, 32)
			if err != nil {
				t.Fatalf("cannot read the hex in %q: %v", term, err)
			}
			sum += int(v) << shift
			continue
		}
		v, err := strconv.ParseInt(term, 16, 32)
		if err != nil {
			t.Fatalf("cannot read the hex in %q: %v", term, err)
		}
		sum += int(v) << shift
	}
	return rune(sum)
}
