package app

import (
	"github.com/egoist/mygo/ui"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"

	"llm-xray/internal/model"
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

// TestTruncateRunesCountsCharactersNotBytes pins the unit truncateRunes works in.
//
// The function shortens a prompt chip's label. Rewriting it to slice s[:n] — bytes instead of
// runes — left the whole suite green across a 358s run, and that version is wrong twice over:
// "注意力就是加权求和" truncated to 3 becomes "注…" rather than "注意力…", and a cut that lands
// inside a multi-byte sequence produces an invalid UTF-8 byte on screen.
//
// The three-property version below is what the helper is for: it counts characters, it only adds
// the ellipsis when something was actually dropped, and it never emits an invalid byte. The last
// one is the property, the others are examples, which is the right split when the input space is
// every string a reader can type.
func TestTruncateRunesCountsCharactersNotBytes(t *testing.T) {
	cases := []struct {
		in   string
		n    int
		want string
	}{
		{"attention is all you need", 10, "attention …"},
		{"注意力就是加权求和", 3, "注意力…"},
		{"𝄞𝄞𝄞", 2, "𝄞𝄞…"},  // 4-byte runes
		{"abc", 3, "abc"},  // exactly n: nothing dropped, no ellipsis
		{"abc", 10, "abc"}, // shorter than n
		{"", 5, ""},
	}
	for _, c := range cases {
		if got := truncateRunes(c.in, c.n); got != c.want {
			t.Errorf("truncateRunes(%q, %d) = %q, want %q", c.in, c.n, got, c.want)
		}
	}

	// The property: truncating never *introduces* an invalid byte. The input may already have
	// one — the app accepts arbitrary bytes as a prompt — and when nothing is dropped the
	// function returns it unchanged, which is correct: a display helper must not silently
	// rewrite the reader's input. What it must not do is cut a multi-byte sequence in half,
	// which is exactly what a byte-slice version does.
	inputs := []string{
		"注意力就是加权求和", "𝄞𝄞𝄞", "a注🙂\xffb", "\xff\xfe", "mixed 内容 with spaces",
	}
	for _, s := range inputs {
		for n := 0; n <= len([]rune(s))+1; n++ {
			got := truncateRunes(s, n)
			if got == s {
				continue // nothing dropped, returned verbatim by design
			}
			// Something was cut, so the prefix has to be whole characters.
			if !utf8.ValidString(strings.TrimSuffix(got, "…")) {
				t.Fatalf("truncateRunes(%q, %d) = %q; the cut prefix is not valid UTF-8, so the "+
					"last character was sliced in half", s, n, got)
			}
		}
	}
}

// TestRailReadoutNeverShowsThePlaceholder keeps the Q/K readout from falling back to "?".
//
// railTokenText answers "?" for an out-of-range index, and its only caller checks the bounds
// before calling, so that answer never reaches the screen. Relaxing the caller's condition to
// `cq >= 0 && ck >= 0` leaves the whole suite green — measured across a 364s run — and the app
// then prints "? × ?" where a query and key should be. That is worse than a panic would be:
// nothing reports it, and the reader sees a placeholder they cannot explain.
//
// So the assertion is on the rendered readout, not on the helper: at every stage and with a
// pinned cell, no "?" may appear in the rail's Q/K row.
func TestRailReadoutNeverShowsThePlaceholder(t *testing.T) {
	// The state that reaches the placeholder is a stale pin: a cell pinned against a longer
	// prompt, then the prompt shortened. clampPinnedCell normally handles this, but the pin can
	// also be set from outside, and cellCursor hands the fields back unchecked. Measured: with
	// the caller's condition relaxed to `cq >= 0 && ck >= 0`, this exact state renders "?" in
	// the readout while the suite stays green.
	for _, step := range []int{2, 3} {
		a := newTestApp(t)
		a.Step = step
		a.applyPrompt("ab")
		a.KeyQ, a.KeyK = 50, 40
		if a.KeyQ < len(a.Trace.Tokens) {
			t.Fatalf("the test's pin is not actually outside the trace (%d tokens, pin %d); "+
				"it proves nothing", len(a.Trace.Tokens), a.KeyQ)
		}
		tr := ui.NewTester(a.View, 1500, 940)
		tr.Frame()
		for _, s := range tr.Texts() {
			if s == "?" {
				t.Fatalf("stage %d renders the rail's placeholder \"?\" for a pin outside the "+
					"trace; the caller's bounds check is missing or too weak", step+1)
			}
		}
	}

	// And the helper itself still answers "?" rather than panicking, so a future caller that
	// has not checked gets a placeholder instead of a crash.
	var toks []modelToken
	if got := railTokenText(toks, 0); got != "?" {
		t.Fatalf("railTokenText on an empty slice returned %q, want the placeholder", got)
	}
	if got := railTokenText(toks, -1); got != "?" {
		t.Fatalf("railTokenText(-1) returned %q, want the placeholder", got)
	}
}

// TestSampleDrawRefusesADistributionWithNoMass pins what happens when nothing can be drawn.
//
// Two inputs reach the final failure return: a nil distribution, which the length check catches
// first, and an all-zero one, which walks the whole accumulator without picking anything and then
// finds no index with mass to rescue. Both have to report failure rather than returning index 0 of
// a distribution with no mass — measured: changing that final return to a success makes this test
// fail with "an all-zero distribution produced a draw at index 0".
//
// The other rescue inside the function, which picks the last index with mass when the
// accumulator stops short, is not reached here or anywhere else; its comment records why.
func TestSampleDrawRefusesADistributionWithNoMass(t *testing.T) {
	a := newTestApp(t)

	if _, ok := a.sampleDraw(nil); ok {
		t.Fatal("a nil distribution produced a draw")
	}
	if d, ok := a.sampleDraw(make([]float32, 8)); ok {
		t.Fatalf("an all-zero distribution produced a draw at index %d", d.pick)
	}

	// A distribution that sums to 1 is drawn from normally, and the reported interval brackets
	// the pick's own mass — this is what the card prints, so it has to agree with the draw.
	probs := make([]float32, 256)
	for i := range probs {
		probs[i] = 1.0 / 256
	}
	d, ok := a.sampleDraw(probs)
	if !ok {
		t.Fatal("a uniform distribution produced no draw")
	}
	if d.lo > d.x || d.x > d.hi {
		t.Fatalf("the draw reports x=%.6f outside its own interval [%.6f, %.6f]", d.x, d.lo, d.hi)
	}
	if math.Abs(d.prob-1.0/256) > 1e-6 {
		t.Fatalf("the draw reports p=%v for a uniform distribution", d.prob)
	}
}

// TestSampleDrawHandlesADistributionThatSummedLowInFloat32 covers a shape the rescue was written
// for, and records that the rescue does not actually fire on it.
//
// 5000 masses of 1/5000 sum to 0.99994122 in float32, and the prompt "q180928" hashes to
// x = 0.99999700 — a point above that total. The intuition is that the walk therefore misses and
// the rescue has to choose. It does not: sampleDraw accumulates in float64 while the masses are
// float32, so its own running total reaches 1 and the last index is picked normally.
//
// That is worth asserting as it stands. It pins the draw's answer for a near-1 x, and it records
// the measured reason the rescue stays untested — which is more useful than a test named for a
// branch it never enters. The rescue's own comment carries the same finding.
func TestSampleDrawHandlesADistributionThatSummedLowInFloat32(t *testing.T) {
	a := newTestApp(t)
	a.Prompt = "q180928"

	const n = 5000
	probs := make([]float32, n)
	for i := range probs {
		probs[i] = 1.0 / float32(n)
	}

	// The premise, measured: a float32 walk stops short and x sits above it.
	var f32 float32
	for _, p := range probs {
		f32 += p
	}
	var seed uint64 = 1469598103934665603
	for _, b := range []byte(a.Prompt) {
		seed ^= uint64(b)
		seed *= 1099511628211
	}
	x := float64(seed%1000003) / 1000003.0
	if float64(f32) >= 1 {
		t.Fatalf("a float32 walk of this distribution reached %v, not short of 1; the case "+
			"this test describes no longer exists", f32)
	}
	if x <= float64(f32) {
		t.Fatalf("x = %.10f is not above the float32 total %.10f", x, f32)
	}

	// The behaviour: the draw lands on the last index via the normal path, because its own
	// accumulator is float64.
	d, ok := a.sampleDraw(probs)
	if !ok {
		t.Fatal("the draw failed on a distribution with mass at every index")
	}
	if d.pick != n-1 {
		t.Fatalf("the draw picked %d; with x above every boundary it should land on %d", d.pick, n-1)
	}
	if d.lo > d.x || d.x > d.hi {
		t.Fatalf("x = %v is outside the reported interval [%v, %v]", d.x, d.lo, d.hi)
	}
}

// TestChipGlyphAnswersForIndexesItShouldNeverSee pins chipGlyph's bounds guard.
//
// The guard is unreachable from the app — every call site passes an index into a.Trace.Tokens —
// but unlike the readout's placeholder this one is easy to assert directly, because chipGlyph
// takes the trace as an argument. Measured: narrowing the guard to `tr == nil` alone left the
// whole suite green across a 366s run.
//
// The three out-of-range answers matter because the strip is drawn every frame: an index panic
// here blanks the window instead of failing one frame, and "?" is what a reader sees for a
// position that is not a token.
func TestChipGlyphAnswersForIndexesItShouldNeverSee(t *testing.T) {
	a := newTestApp(t)
	tr := a.Trace

	if got := chipGlyph(nil, 0); got != "?" {
		t.Fatalf("chipGlyph(nil, 0) = %q, want the placeholder", got)
	}
	if got := chipGlyph(tr, -1); got != "?" {
		t.Fatalf("chipGlyph(tr, -1) = %q, want the placeholder", got)
	}
	if got := chipGlyph(tr, len(tr.Tokens)); got != "?" {
		t.Fatalf("chipGlyph(tr, %d) = %q, want the placeholder", len(tr.Tokens), got)
	}

	// And an index that does exist answers with the token rather than the placeholder, so the
	// guard is not simply swallowing every call.
	if got := chipGlyph(tr, 0); got == "?" {
		t.Fatal("chipGlyph(tr, 0) answered with the placeholder for a real token")
	}
}

// TestLabelStrideAnswersForADegenerateCellWidth pins the guard that keeps the loop bounded.
//
// labelStride counts up until stride×cw reaches 26 DIP. With cw at or below zero that inequality
// is true for every stride ever tried, so without the guard the loop runs until float32 overflow
// turns stride×cw into +Inf or NaN. It does terminate — measured — but with a stride in the
// billions, which then indexes nothing useful.
//
// The existing test sweeps cell widths from 2 to 40 DIP, every one of them positive. Removing the
// `cw <= 0` guard leaves the whole suite green.
func TestLabelStrideAnswersForADegenerateCellWidth(t *testing.T) {
	for _, cw := range []float32{0, -1, -26} {
		if got := labelStride(cw); got != 1 {
			t.Fatalf("labelStride(%v) = %d; a cell with no width has no stride to compute, and "+
				"the answer has to be a value the caller can draw with", cw, got)
		}
	}

	// The positive side keeps its contract, so the guard is not swallowing the real cases.
	for _, cw := range []float32{1, 13, 26, 100} {
		if got := labelStride(cw); float32(got)*cw < 26 && cw < 26 {
			t.Fatalf("labelStride(%v) = %d leaves %.1f DIP between labels", cw, got, float32(got)*cw)
		}
	}
}

// TestStepTokenLeavesTheSelectionUnsetOnAnEmptyTrace pins the invariant stepToken's guard keeps.
//
// With an empty prompt SelToken is -1, meaning "nothing selected". Removing the `n == 0` return
// lets Shift+Right set it to 0 — an index into an empty slice. Nothing breaks today: every reader
// of SelToken bounds-checks first, and the next keystroke recomputes the value. Measured, the
// whole suite stayed green across a 354s run with the guard gone.
//
// The invariant is still worth holding, and the test states it directly rather than through a
// render: "selected nothing" must stay "selected nothing", so a future reader may trust the field
// instead of defending against a state this function created.
func TestStepTokenLeavesTheSelectionUnsetOnAnEmptyTrace(t *testing.T) {
	a := newTestApp(t)
	a.applyPrompt("")
	if len(a.Trace.Tokens) != 0 {
		t.Fatalf("the empty prompt produced %d tokens; this test needs an empty trace",
			len(a.Trace.Tokens))
	}
	if a.SelToken != -1 {
		t.Fatalf("an empty prompt leaves SelToken at %d, not -1", a.SelToken)
	}

	// Either direction has to leave it alone.
	a.stepToken(1)
	if a.SelToken != -1 {
		t.Fatalf("stepToken(1) on an empty trace set SelToken to %d; there is no token to select",
			a.SelToken)
	}
	a.stepToken(-1)
	if a.SelToken != -1 {
		t.Fatalf("stepToken(-1) on an empty trace set SelToken to %d", a.SelToken)
	}

	// And with tokens it still moves, so the guard is not disabling the function.
	a.applyPrompt("abc")
	a.SelToken = 0
	a.stepToken(1)
	if a.SelToken != 1 {
		t.Fatalf("stepToken(1) on a three-token trace moved the selection to %d, want 1", a.SelToken)
	}
}

// TestScrollStageByStaysInRange pins both clamps.
//
// scrollStageBy is what PageUp/PageDown call. It adds delta to Scroll.Y and then clamps to
// [0, MaxY]. Removing both clamps left the whole suite green across a 354s run, because every
// other scroll test checks where the view ended up rather than whether the field ever left its
// range. Ten presses of PageUp from the top would leave Y at -4500; the drawn frame clamps
// nothing, so the stage column would scroll past its own beginning.
func TestScrollStageByStaysInRange(t *testing.T) {
	a := newTestApp(t)
	a.Step = 4 // a tall stage, so MaxY is well above a single page
	tr := ui.NewTester(a.View, 1180, 720)
	tr.Frame()
	maxY := a.Scroll.MaxY
	if maxY <= 0 {
		t.Fatalf("the stage reports MaxY = %.0f; there is nothing to scroll", maxY)
	}

	// Far past the top.
	for i := 0; i < 20; i++ {
		a.scrollStageBy(-maxY)
		if a.Scroll.Y < 0 {
			t.Fatalf("scrolling up %d page(s) left Y at %.0f", i+1, a.Scroll.Y)
		}
	}
	if a.Scroll.Y != 0 {
		t.Fatalf("after scrolling far past the top Y is %.0f, want 0", a.Scroll.Y)
	}

	// Far past the bottom.
	for i := 0; i < 20; i++ {
		a.scrollStageBy(maxY)
		if a.Scroll.Y > maxY {
			t.Fatalf("scrolling down %d page(s) left Y at %.0f, past MaxY %.0f", i+1, a.Scroll.Y, maxY)
		}
	}
	if a.Scroll.Y != maxY {
		t.Fatalf("after scrolling far past the bottom Y is %.0f, want MaxY %.0f", a.Scroll.Y, maxY)
	}

	// A partial page in the middle still moves, so the clamps are not pinning the value.
	a.Scroll.Y = 0
	a.scrollStageBy(maxY / 2)
	if a.Scroll.Y <= 0 || a.Scroll.Y >= maxY {
		t.Fatalf("a half-page scroll from 0 landed at %.0f, inside neither end", a.Scroll.Y)
	}
}

// TestProbColorIsDegenerateSafe pins probColor's zero-scale branch.
//
// The probability strip passes max = the largest probability in the row. If that is zero — an
// empty row, or a distribution that underflowed to nothing — then p/max divides by zero and
// sqrt(+Inf) saturates every cell's alpha at 255. The strip would paint every position as the
// most likely one. Measured: deleting the `max <= 0` guard leaves the whole suite green across a
// 355s run, because no test asks what colour a strip with no mass produces.
//
// The guard's answer is colFaint, which reads as "nothing to show here" rather than as a maximum.
func TestProbColorIsDegenerateSafe(t *testing.T) {
	for _, max := range []float32{0, -1} {
		if got := probColor(0.5, max); got != colFaint {
			t.Errorf("probColor(0.5, %v) = %+v, want the faint fallback %+v", max, got, colFaint)
		}
	}

	// A real scale still ramps: zero mass is the palest, the maximum is the strongest, and
	// nothing in between runs past either end.
	if lo, hi := probColor(0, 1), probColor(1, 1); lo.A >= hi.A {
		t.Fatalf("a zero probability (%v) is not paler than the maximum (%v)", lo.A, hi.A)
	}
	mid := probColor(0.25, 1)
	if mid.A <= probColor(0, 1).A || mid.A >= probColor(1, 1).A {
		t.Fatalf("a quarter probability (%v) does not sit between the ends", mid.A)
	}
	// Past the scale it saturates rather than wrapping.
	if probColor(2, 1) != probColor(1, 1) {
		t.Fatalf("a probability past the scale gets a different colour from the scale itself; "+
			"probColor(%v) vs probColor(%v)", probColor(2, 1).A, probColor(1, 1).A)
	}
}

// TestResidualChainRejectsAnIndexAnySliceCannotHold covers every bound residualChain checks.
//
// The function reads five parallel slices of the layer trace at the same (token, dim) position:
// In, AttnProj, AfterAttn, FFNOut and AfterFFN. Its guard checks all five. Trimming it to the
// first one — `i` and `dim` against In alone — left the whole suite green across a 366s run,
// because the existing test asks whether the printed arithmetic closes, on traces where every
// slice has the same shape.
//
// They do not have to: a truncated or partially filled trace, or a dimension beyond a narrower
// slice, would index past the end. The test walks each slice in turn with only that one made
// short, so trimming any single bound shows up here.
func TestResidualChainRejectsAnIndexAnySliceCannotHold(t *testing.T) {
	a := newTestApp(t)
	base := a.Trace.Layers[0]

	// A sane position works, so the test is not rejecting everything.
	steps, ok := residualChain(base, 0, 0)
	if !ok || len(steps) != 3 {
		t.Fatalf("residualChain(base, 0, 0) = (%d steps, %v), want three steps", len(steps), ok)
	}

	// Each slice in turn gets a row shorter than its siblings, and the same position is asked
	// for. The guard has to answer false rather than index past the end.
	shorten := map[string]func(*model.LayerTrace){
		"In":        func(l *model.LayerTrace) { l.In[0] = l.In[0][:1] },
		"AttnProj":  func(l *model.LayerTrace) { l.AttnProj[0] = l.AttnProj[0][:1] },
		"AfterAttn": func(l *model.LayerTrace) { l.AfterAttn[0] = l.AfterAttn[0][:1] },
		"FFNOut":    func(l *model.LayerTrace) { l.FFNOut[0] = l.FFNOut[0][:1] },
		"AfterFFN":  func(l *model.LayerTrace) { l.AfterFFN[0] = l.AfterFFN[0][:1] },
	}
	for name, mutate := range shorten {
		l := base
		l.In, l.AttnProj, l.AfterAttn, l.FFNOut, l.AfterFFN =
			append([][]float32(nil), base.In...), append([][]float32(nil), base.AttnProj...),
			append([][]float32(nil), base.AfterAttn...), append([][]float32(nil), base.FFNOut...),
			append([][]float32(nil), base.AfterFFN...)
		mutate(&l)
		if _, ok := residualChain(l, 0, 1); ok {
			t.Fatalf("%s has one element but the guard accepted dim 1; it would index past the end",
				name)
		}
	}

	// Out-of-range tokens and dims are rejected too.
	if _, ok := residualChain(base, -1, 0); ok {
		t.Fatal("token -1 was accepted")
	}
	if _, ok := residualChain(base, len(base.In), 0); ok {
		t.Fatal("a token past the end was accepted")
	}
	if _, ok := residualChain(base, 0, -1); ok {
		t.Fatal("dim -1 was accepted")
	}
}

// TestScoreColorAndHeatColorKeepTheirDegenerateBranches pins the two colour helpers that sit
// beside probColor.
//
// scoreColor answers colPanel2 when the scale is zero, for the same reason probColor answers
// colFaint: v/scale divides by zero and the saturated result would paint every cell as an extreme.
// heatColor answers colPanel2 for weights at or below 0.0005, which is how a masked cell reads as
// empty — the causal mask sets the upper triangle to zero and those cells must be indistinguishable
// from the panel behind them.
//
// Measured: deleting scoreColor's guard left the suite green across 361s, and moving heatColor's
// threshold from 0.0005 to 0 left it green across 360s. Both are invisible to Texts(), which is
// why three colour helpers in a row needed the same kind of test.
func TestScoreColorAndHeatColorKeepTheirDegenerateBranches(t *testing.T) {
	// scoreColor with no scale.
	for _, scale := range []float32{0, -2} {
		if got := scoreColor(1.5, scale); got != colPanel2 {
			t.Errorf("scoreColor(1.5, %v) = %+v, want the panel colour %+v", scale, got, colPanel2)
		}
	}
	// And with a real scale it still ramps, saturating at the ends rather than wrapping.
	if scoreColor(1, 1) == scoreColor(0.2, 1) {
		t.Fatal("scoreColor gives the same colour for 0.2 and 1 on a unit scale")
	}
	// Saturation past the scale is NOT asserted. ui.Color's alpha is a byte, so 0.06 + 0.9*t is
	// clamped by the conversion itself: measured, scoreColor(50, 1) and scoreColor(1, 1) are
	// byte-identical even with the explicit `if t > 1 { t = 1 }` deleted, so any assertion here
	// would pass either way. Same finding as valueColor's clamp two rounds ago; the explicit
	// clamp is for readers and for a future consumer that wants the ratio, not for the colour.
	if scoreColor(-1, 1) == scoreColor(1, 1) {
		t.Fatal("scoreColor gives a negative and a positive score the same colour; sign carries " +
			"meaning on this scale")
	}

	// heatColor: at or below the threshold a cell reads as the panel behind it.
	for _, w := range []float32{-1, 0, 0.0001, 0.0004, 0.0005} {
		if got := heatColor(w); got != colPanel2 {
			t.Errorf("heatColor(%v) = %+v, want the panel colour; a masked cell must read as empty",
				w, got)
		}
	}
	// Just above it, the cell is visible — otherwise the mask would swallow small real weights.
	if heatColor(0.0006) == colPanel2 {
		t.Fatal("heatColor(0.0006) still paints the panel colour; a real weight above the " +
			"threshold has to be visible")
	}
	// And the ramp is monotonic up to the ceiling.
	prev := heatColor(0.01).A
	for _, w := range []float32{0.1, 0.3, 0.6, 1} {
		if cur := heatColor(w).A; cur < prev {
			t.Fatalf("heatColor alpha fell from %d to %d at weight %v", prev, cur, w)
		}
		prev = heatColor(w).A
	}
}

// TestLessonNamesMatchesTheLessons covers the names main.go prints when -step is out of range.
//
// LessonNames is the one exported function in this package that the app never calls. main.go
// uses it to build the error message: `-step 99 is out of range: lessons are numbered 0-6
// (分词, 嵌入, …)`. There is no -list flag — an earlier version of this comment said there was,
// and the class of bug this test guards is exactly that kind of confidently wrong sentence about
// what the CLI does.
//
// Its coverage was 0%: the test that mentions it greps main.go's source for `app.LessonNames()`
// rather than calling anything, which proves the CLI wires it up but not that it returns the
// right names.
//
// It has to agree with `lessons` in both directions: same count, same order, same spellings. A
// name that drifts here is a name the out-of-range message gets wrong, and a count that drifts
// is a stage that message omits.
func TestLessonNamesMatchesTheLessons(t *testing.T) {
	names := LessonNames()
	if len(names) != len(lessons) {
		t.Fatalf("LessonNames returned %d names for %d lessons", len(names), len(lessons))
	}
	for i, l := range lessons {
		if names[i] != l.name {
			t.Fatalf("LessonNames()[%d] = %q, but the lesson is %q", i, names[i], l.name)
		}
	}

	// A fresh slice each call: the CLI loops over the result, and a caller that sorted or
	// truncated it in place would otherwise corrupt the next caller's view.
	first := LessonNames()
	first[0] = "clobbered"
	if second := LessonNames(); second[0] == "clobbered" {
		t.Fatal("LessonNames hands out a slice that aliases the package's own state")
	}
	if len(names) == 0 {
		t.Fatal("LessonNames returned nothing")
	}
}

// TestCausalInvarianceLineNamesTheDisagreeingPosition covers the branch that only appears when
// the mask is broken.
//
// The line has two forms: "N 个位置的预测没变" when every position agrees, and "有 K 个的预测变了；
// 第一个在位置 P" when one does not. Only the first is ever rendered by the app — the bundled
// weights satisfy the invariant — so the second was at 66.7% coverage and no test mentions its
// text. It is the branch a reader would see on a day the mask regressed, which is exactly when it
// has to be right.
//
// The comparison is built by hand rather than by breaking the weights, because the branch's input
// is a struct: what matters is that the formatter names the position instead of only counting.
func TestCausalInvarianceLineNamesTheDisagreeingPosition(t *testing.T) {
	a := newTestApp(t)
	// The line lives in the per-position card on the last stage, under the accuracy figures.
	a.Step = 6

	clean := causalComparison{OK: true, Compared: 35, Identical: 35}
	broken := causalComparison{OK: true, Compared: 35, Identical: 34, MismatchAt: 7}

	render := func(cmp causalComparison) string {
		// causalInvariance re-measures unless the cache claims the current trace, so point the
		// cache at this trace and let the cached value stand.
		a.causal, a.causalFor = cmp, a.Trace
		tr := ui.NewTester(a.View, 1500, 2000)
		tr.Frame()
		for _, s := range tr.Texts() {
			if strings.HasPrefix(s, "因果掩码核对：") {
				return s
			}
		}
		return ""
	}

	got := render(clean)
	if got == "" {
		t.Fatal("the mask line is not rendered on the Softmax stage")
	}
	if !strings.Contains(got, "35 个位置的预测没变") {
		t.Fatalf("the agreeing line does not state the count: %q", got)
	}
	if strings.Contains(got, "变了") {
		t.Fatalf("the agreeing line claims a change: %q", got)
	}

	got = render(broken)
	if got == "" {
		t.Fatal("the mask line is not rendered when a position disagrees")
	}
	if !strings.Contains(got, "有 1 个的预测变了") {
		t.Fatalf("the disagreeing line does not count the change: %q", got)
	}
	if !strings.Contains(got, "第一个在位置 7") {
		t.Fatalf("the disagreeing line does not name the position; the reader's next question "+
			"is which one: %q", got)
	}
	if strings.Contains(got, "预测没变") {
		t.Fatalf("the disagreeing line still claims nothing changed: %q", got)
	}
}

// TestLayerClampsAnOutOfRangeIndex pins layer()'s three answers.
//
// The accessor is called on every stage that reads layer data, and Layer can be set from outside
// — a checkpoint with fewer layers than the one the reader last selected leaves it past the end.
// Clamping to the nearest real layer keeps the stage readable; returning nil would blank it.
//
// Coverage was 62.5% before this: the app never leaves Layer out of range, so only the middle
// path ever ran.
func TestLayerClampsAnOutOfRangeIndex(t *testing.T) {
	a := newTestApp(t)
	if len(a.Trace.Layers) < 2 {
		t.Fatalf("the bundled weights have %d layers; this test needs two to tell them apart",
			len(a.Trace.Layers))
	}
	last := len(a.Trace.Layers) - 1

	a.Layer = 0
	if got := a.layer(); got != &a.Trace.Layers[0] {
		t.Fatalf("Layer=0 gave a different layer than Layers[0]")
	}

	a.Layer = last
	if got := a.layer(); got != &a.Trace.Layers[last] {
		t.Fatalf("Layer=%d gave a different layer than Layers[%d]", last, last)
	}

	a.Layer = -5
	if got := a.layer(); got != &a.Trace.Layers[0] {
		t.Fatalf("a negative Layer did not clamp to the first layer")
	}

	a.Layer = last + 10
	if got := a.layer(); got != &a.Trace.Layers[last] {
		t.Fatalf("a Layer past the end did not clamp to the last layer")
	}
}

// TestComputeAllFFNConcentrationSkipsLayersWithNoHiddenRows covers the skip inside the aggregator.
//
// computeAllFFNConcentration walks every layer and keeps the ones computeFFNConcentration could
// measure. A layer whose Hidden rows are missing or short of the token count answers OK=false, and
// the aggregator drops it rather than reporting a concentration computed from nothing. Deleting
// the skip left the whole suite green across a 363s run, because the bundled trace always has
// every row.
//
// The state is reachable from a partially filled checkpoint, so the test builds it by emptying one
// layer's Hidden and requires the count to drop by exactly one — not merely to be smaller, since
// dropping the wrong layer would be the same count.
func TestComputeAllFFNConcentrationSkipsLayersWithNoHiddenRows(t *testing.T) {
	a := newTestApp(t)
	tr := a.Trace
	if len(tr.Layers) < 2 {
		t.Fatalf("the bundled weights have %d layers; this test needs two", len(tr.Layers))
	}

	full := computeAllFFNConcentration(tr)
	if len(full) != len(tr.Layers) {
		t.Fatalf("a complete trace gave %d entries for %d layers", len(full), len(tr.Layers))
	}

	saved := tr.Layers[0].Hidden
	tr.Layers[0].Hidden = nil
	defer func() { tr.Layers[0].Hidden = saved }()

	got := computeAllFFNConcentration(tr)
	if len(got) != len(full)-1 {
		t.Fatalf("emptying one layer's Hidden gave %d entries, want %d", len(got), len(full)-1)
	}
	// The entry that survived has to be the other layer, and it has to keep its own index.
	for _, c := range got {
		if c.Layer == 0 {
			t.Fatalf("the aggregate still contains layer 0, whose Hidden rows were removed")
		}
	}
}

// TestMeanFutureShareAnswersWithoutEnoughTokens pins the redundant guard's answer.
//
// The `tokens < 2` return duplicates what the loop bound and the rows check already produce —
// measured, deleting it changes no result. It is kept for readability, so the test states the
// behaviour rather than the guard: with nothing to compare, the answer is (0, 0) and not a
// division by zero.
func TestMeanFutureShareAnswersWithoutEnoughTokens(t *testing.T) {
	a := newTestApp(t)
	h := a.Trace.Layers[0].Heads[0]
	dHead := a.W.Config.DModel / a.W.Config.NHead

	for _, n := range []int{0, 1} {
		share, rows := meanFutureShare(h, n, dHead)
		if share != 0 || rows != 0 {
			t.Fatalf("meanFutureShare with %d token(s) = (%v, %d), want (0, 0)", n, share, rows)
		}
	}

	// With the two tokens a comparison needs, it produces a real average.
	share, rows := meanFutureShare(h, 2, dHead)
	if rows != 1 {
		t.Fatalf("two tokens gave %d comparable rows, want 1", rows)
	}
	if share < 0 || share > 1 {
		t.Fatalf("the future share is %v, outside [0, 1]", share)
	}
}

// TestShortPathKeepsTheTailAndSurvivesADegenerateWidth pins the weights line's path shortening.
//
// shortPath is drawn beside the model's source path in the top bar. It keeps the end of the path
// — the filename is the informative part — and marks the cut with an ellipsis. It had no test at
// all: removing the `n <= 1` branch left the whole suite green across a 361s run while
// shortPath("abc", 0) panics with "slice bounds out of range [4:3]", because the tail slice starts
// at len(r)-(n-1), which is past the end for n < 1.
//
// The width this app actually passes is 34, so neither branch runs today. Both are cheap to state,
// and the CJK case matters because a byte-slice version would cut a character in half.
func TestShortPathKeepsTheTailAndSurvivesADegenerateWidth(t *testing.T) {
	cases := []struct {
		name string
		p    string
		n    int
		want string
	}{
		{"fits", "/home/user/weights.json", 34, "/home/user/weights.json"},
		{"exactly n", "abcdef", 6, "abcdef"},
		{"keeps the tail", "/a/very/long/path/to/weights.json", 10, "…ghts.json"},
		{"n of two keeps one char", "abcdef", 2, "…f"},
		{"n of one is only the marker", "abcdef", 1, "…"},
		{"n of zero is only the marker", "abcdef", 0, "…"},
		{"negative n is only the marker", "abcdef", -3, "…"},
		{"empty stays empty", "", 5, ""},
	}
	for _, c := range cases {
		if got := shortPath(c.p, c.n); got != c.want {
			t.Errorf("%s: shortPath(%q, %d) = %q, want %q", c.name, c.p, c.n, got, c.want)
		}
	}

	// The property: the result never has more runes than n, except when the whole path was short
	// enough to return unchanged. A byte-slice version would violate this on any multi-byte path.
	for _, p := range []string{"日本語のとても長いパス名.json", "a注🙂b/路径/文件.json"} {
		for n := 2; n <= len([]rune(p)); n++ {
			got := shortPath(p, n)
			if len([]rune(got)) > n {
				t.Fatalf("shortPath(%q, %d) = %q, longer than the width asked for", p, n, got)
			}
			if !utf8.ValidString(got) {
				t.Fatalf("shortPath(%q, %d) = %q, which is not valid UTF-8", p, n, got)
			}
		}
	}
}

// TestComputeLayerDivergenceRejectsEveryBadIndex covers the five guards in one place.
//
// The function compares layer N's attention row against layer N-1's, so it needs a real previous
// layer, matching head counts, and a row inside both. Each rejection returns the zero value with
// Ok=false, and the card skips rather than drawing a comparison of nothing.
//
// Coverage was 84.2% and three guards were untested. Removing the row-past-the-end check and the
// half of the head guard that compares against the previous layer both left the suite green: the
// bundled weights give every layer four heads and rows come from token indexes, so neither fires
// here. A partially filled trace makes them reachable, which is the case they exist for.
//
// The `layer < 0` guard is redundant — the next one tests `layer < 1` — so deleting it alone
// changes nothing by design. Its comment records that; this test cannot distinguish the two.
func TestComputeLayerDivergenceRejectsEveryBadIndex(t *testing.T) {
	a := newTestApp(t)
	layers := a.Trace.Layers
	if len(layers) < 2 {
		t.Fatalf("the bundled weights have %d layers; this test needs two", len(layers))
	}
	rows := len(layers[1].Heads[0].Attn)
	if rows < 3 {
		t.Fatalf("layer 1 has %d attention rows; too few to test a middle one", rows)
	}

	// A real comparison works, so the rejections below are not rejecting everything.
	if d := computeLayerDivergence(layers, 1, 0, 2); !d.Ok || d.TV < 0 {
		t.Fatalf("a valid call returned Ok=%v TV=%v", d.Ok, d.TV)
	}

	bad := []struct {
		name             string
		layer, head, row int
	}{
		{"negative layer", -1, 0, 2},
		{"layer zero has no previous", 0, 0, 2},
		{"layer past the end", len(layers), 0, 2},
		{"row zero is one visible key", 1, 0, 0},
		{"negative head", 1, -1, 2},
		{"head past the end", 1, len(layers[1].Heads), 2},
		{"row past the end", 1, 0, rows},
	}
	for _, c := range bad {
		if d := computeLayerDivergence(layers, c.layer, c.head, c.row); d.Ok {
			t.Errorf("%s: layer=%d head=%d row=%d returned Ok with TV=%v",
				c.name, c.layer, c.head, c.row, d.TV)
		}
	}

	// A trace whose previous layer has fewer heads exercises the half of the head guard that
	// compares against layers[layer-1].Heads, which equal head counts never reach.
	shrunk := append([]model.LayerTrace(nil), layers...)
	shrunk[0] = layers[0]
	shrunk[0].Heads = layers[0].Heads[:1]
	if d := computeLayerDivergence(shrunk, 1, 3, 2); d.Ok {
		t.Fatalf("head 3 was accepted although the previous layer has %d head(s)",
			len(shrunk[0].Heads))
	}
}

// TestMaskCounterfactualHeadRangeIsMeasured keeps the mask card's per-head range from becoming a
// literal again.
//
// The line read "...每个头都在 51%–84% 之间" — a range measured once, on one prompt, then written
// into the sentence. The card states it as a property of the model, and it was false on two of the
// four bundled presets: measured, 注意力就是加权求和 gives 50%–55% and hi gives 26%–97%. The
// neighbouring figures in the same line were computed, so the sentence contradicted itself.
//
// The test renders the line on two prompts and requires the range to track the data, which is
// what makes it a statement about the model rather than about one sentence.
func TestMaskCounterfactualHeadRangeIsMeasured(t *testing.T) {
	line := func(prompt string) string {
		a := newTestApp(t)
		a.Prompt = prompt
		a.recompute()
		a.Step = 2
		a.Head = 0
		tr := ui.NewTester(a.View, 1500, 2000)
		tr.Frame()
		for _, s := range tr.Texts() {
			if strings.HasPrefix(s, "头 0 整句平均是") {
				return s
			}
		}
		return ""
	}

	en := line("The transformer reads the tokens and")
	zh := line("注意力就是加权求和")
	if en == "" || zh == "" {
		t.Fatalf("the mask card does not draw its per-head line (en=%q, zh=%q)", en, zh)
	}
	if en == zh {
		t.Fatalf("both prompts produced the same line, so at least one is not measured:\n  %s", en)
	}
	if strings.Contains(en, "51%–84%") || strings.Contains(zh, "51%–84%") {
		t.Fatalf("the line still carries the old literal range:\n  en: %s\n  zh: %s", en, zh)
	}

	// And the range has to be internally consistent: the head being shown sits inside it.
	re := regexp.MustCompile(`头 0 整句平均是 (\d+)%（\d+ 行）——每个头都在 (\d+)%–(\d+)% 之间`)
	for _, s := range []string{en, zh} {
		m := re.FindStringSubmatch(s)
		if m == nil {
			t.Fatalf("the line no longer has the shape this test reads: %q", s)
		}
		mean, _ := strconv.Atoi(m[1])
		lo, _ := strconv.Atoi(m[2])
		hi, _ := strconv.Atoi(m[3])
		if mean < lo || mean > hi {
			t.Fatalf("the shown head's mean (%d%%) sits outside the range it states (%d%%–%d%%): %q",
				mean, lo, hi, s)
		}
	}
}

// TestMatrixChromeTotalMatchesTheComment pins the figure the matrix sizing quotes.
//
// The heatmap sizes its cells from the width the window leaves after the sidebar, the inspector
// and the card's own chrome, and names the total as 302 in the comment above. Those four numbers
// (2 + 32 + 28 + 240) were summed inline, so changing any one of them was invisible: measured,
// turning the 32 into 40 left the whole suite green across a full run while the matrix would have
// been given 8 DIP less and the comment's 302 would no longer be the total.
//
// I nearly did exactly that myself — folding the leading 2 into a "16 padding per side" constant,
// which reads tidier and sums to 284. The figures are now named individually and this test holds
// their total to the number the comment states.
func TestMatrixChromeTotalMatchesTheComment(t *testing.T) {
	src, err := os.ReadFile("widgets.go")
	if err != nil {
		t.Fatalf("read widgets.go: %v", err)
	}
	text := string(src)

	// The comment's own figure.
	reComment := regexp.MustCompile(`393 − 224 − 330 − 302`)
	if !reComment.MatchString(text) {
		t.Fatal("the comment no longer quotes 393 − 224 − 330 − 302; update this test with the " +
			"figure it states now")
	}

	// The four terms the code sums, by name.
	terms := regexp.MustCompile(`(?s)borderX\s*=\s*(\d+).*?cardPadX\s*=\s*(\d+).*?columnGap\s*=\s*(\d+).*?secondCol\s*=\s*(\d+)`)
	m := terms.FindStringSubmatch(text)
	if m == nil {
		t.Fatal("the card chrome constants are no longer declared in the form this test reads")
	}
	total := 0
	for i := 1; i <= 4; i++ {
		v, err := strconv.Atoi(m[i])
		if err != nil {
			t.Fatalf("cannot read %q as a number: %v", m[i], err)
		}
		total += v
	}
	if total != 302 {
		t.Fatalf("the card chrome now sums to %d, but the comment above the matrix sizing quotes "+
			"302 as the same total; the cells would be sized from a different width", total)
	}
}

// TestMatrixSizingConstantsMatchTheLayout pins the three figures the heatmap sizes itself from.
//
// stageChrome (the window top to the first scrollable row), leaving (the card's padding, chip row
// and axis labels) and maxCell (the largest a cell should get) all carry comments explaining what
// they are, and none of the three had a test: changing stageChrome from 228 to 240, leaving from
// 160 to 180, or maxCell from 20 to 25 each left the whole suite green.
//
// stageChrome is the one that can be checked against the layout directly — its comment says it is
// the offset of the first scrollable row, and the stage header sits at exactly that y. The other
// two are budgets rather than positions, so they are held to their stated values: a change to
// either is a deliberate layout decision that should also update this test and the comment.
func TestMatrixSizingConstantsMatchTheLayout(t *testing.T) {
	for _, size := range [][2]int{{1180, 720}, {1500, 940}, {1920, 1080}} {
		a := newTestApp(t)
		a.Step = 2 // the score stage draws the matrix
		tr := ui.NewTester(a.View, size[0], size[1])
		tr.Frame()

		header, ok := tr.Find("第 3 / 7 步")
		if !ok {
			t.Fatalf("%dx%d: the stage header is not rendered", size[0], size[1])
		}
		// The row lands on a half-pixel boundary (measured 227.5), so the constant is that
		// offset rounded up. A whole DIP of slack is the right tolerance: the point is that the
		// figure tracks the layout, not that it matches to the subpixel.
		const stageChrome = 228
		if d := header.Y - float32(stageChrome); d < -1 || d > 1 {
			t.Fatalf("%dx%d: the first scrollable row starts at y=%.1f, but the matrix sizing "+
				"subtracts %d for it; the matrix would be sized from the wrong height",
				size[0], size[1], header.Y, stageChrome)
		}
	}

	// The two budget constants, held to the values their comments explain.
	src, err := os.ReadFile("widgets.go")
	if err != nil {
		t.Fatalf("read widgets.go: %v", err)
	}
	for _, want := range []string{
		"const stageChrome = 228",
		"const leaving = 160",
		"const maxCell = 20",
	} {
		if !strings.Contains(string(src), want) {
			t.Fatalf("widgets.go no longer declares %q; if a budget changed, update its comment "+
				"and this test together", want)
		}
	}
}

// TestDimmedControlsStayVisiblyOff pins the opacity that says "this control is off".
//
// MyGo's Disabled removes the pointer and the keyboard but paints the widget as usual, so the
// dimming is the only visible cue — the comment above it says 0.55 takes the labels to about
// 2.5:1, "which reads as off without hiding that the control is there". That number is the whole
// point: too faint and the control looks missing, too bright and it looks live while ignoring
// clicks. Measured, raising it to 0.75 left the suite green across a full run.
//
// The range asserted is deliberately wide on both sides — this pins a band, not a value. A
// disabled label must be dimmer than the muted text around it and still brighter than the panel
// behind it, which is what "off but present" means in numbers.
func TestDimmedControlsStayVisiblyOff(t *testing.T) {
	dim := func(c ui.Color) ui.Color { return c.Alpha(dimmed) }

	for _, bg := range []struct {
		name string
		col  ui.Color
	}{{"panel", colPanel}, {"column background", colBg}} {
		label := over(dim(colFaint), bg.col) // the control's own label, drawn in colFaint
		got := contrastRatio(label, bg.col)
		if got < 1.8 {
			t.Errorf("a dimmed label on the %s measures %.2f:1; it would read as missing rather "+
				"than as off", bg.name, got)
		}
		if got > 3.0 {
			t.Errorf("a dimmed label on the %s measures %.2f:1; that is close enough to live text "+
				"that a reader cannot tell the control is disabled", bg.name, got)
		}
	}

	// And it has to be dimmer than the same text undimmed, or the cue is not a cue.
	bg := colPanel
	plain := contrastRatio(colFaint, bg)
	dimmedRatio := contrastRatio(over(dim(colFaint), bg), bg)
	if dimmedRatio >= plain {
		t.Fatalf("dimming raised the contrast from %.2f:1 to %.2f:1", plain, dimmedRatio)
	}
}

// TestLessonNamesIsUsedForTheStepRange pins what main.go does with the names.
//
// This exists because the comment above this test said, for a while, that main.go uses
// LessonNames "for the -list flag". There is no -list flag. The names go into the error message
// for an out-of-range -step, and a reader who trusted the comment went looking for a CLI feature
// that does not exist.
//
// Checking the wiring is not busywork: the call site is the whole reason the function is exported,
// and if main.go ever stops calling it the export and this accessor have no purpose left.
func TestLessonNamesIsUsedForTheStepRange(t *testing.T) {
	src, err := os.ReadFile(filepath.Join("..", "..", "main.go"))
	if err != nil {
		t.Fatalf("read main.go: %v", err)
	}
	body := string(src)

	if !strings.Contains(body, "names := app.LessonNames()") {
		t.Fatal("main.go no longer assigns app.LessonNames() to names; if the CLI stopped using " +
			"it, the export and the accessor have no purpose")
	}
	if !strings.Contains(body, "*step >= len(names)") {
		t.Fatal("main.go no longer range-checks -step against the lesson names; the names were " +
			"being used to build that message")
	}
	if regexp.MustCompile(`flag\.\w+\("list"`).MatchString(body) {
		t.Fatal("main.go grew a -list flag; if that is real, the comment on TestLessonNames " +
			"should describe it and this test should check its output")
	}
}
