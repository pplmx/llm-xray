package model

import "testing"

// TestTokenizeKeepsInvalidBytes verifies that a byte-level tokenizer does not
// expand an invalid byte into the three bytes of U+FFFD.
func TestTokenizeKeepsInvalidBytes(t *testing.T) {
	s := string([]byte{0xFF, 'a', 0x80})
	tokens := Tokenize(s)
	want := []int{0xFF, 'a', 0x80}
	if len(tokens) != len(want) {
		t.Fatalf("%d tokens, want %d", len(tokens), len(want))
	}
	for i, id := range want {
		if tokens[i].ID != id || int(tokens[i].Byte) != id {
			t.Fatalf("token %d = id %d byte %d, want %d", i, tokens[i].ID, tokens[i].Byte, id)
		}
	}
}

// TestTokenizeStartIsRuneOffset checks the field's documented contract: all
// byte tokens of one rune share that rune's index, not the byte offset.
func TestTokenizeStartIsRuneOffset(t *testing.T) {
	const s = "éa中"
	tokens := Tokenize(s)
	want := []int{0, 0, 1, 2, 2, 2}
	if len(tokens) != len(want) {
		t.Fatalf("%d tokens, want %d", len(tokens), len(want))
	}
	for i, start := range want {
		if tokens[i].Start != start {
			t.Fatalf("token %d start = %d, want %d", i, tokens[i].Start, start)
		}
	}
}

// TestTruncateNonPositiveContext documents the empty-result edge case instead
// of letting a negative context slice out of bounds.
func TestTruncateNonPositiveContext(t *testing.T) {
	for _, ctx := range []int{0, -1} {
		if got := Truncate([]int{1, 2, 3}, ctx); len(got) != 0 {
			t.Fatalf("Truncate(ctx=%d) = %v, want empty", ctx, got)
		}
	}
}

// TestTruncateCanReturnNothingForAWindowInsideOneCharacter pins the second way Truncate returns an
// empty slice.
//
// TestTruncateNonPositiveContext covers ctx <= 0. There is another route: the context window is
// smaller than the last rune, so advancing to a UTF-8 lead byte walks start to len(ids) and the
// result is empty even though the input has bytes. Measured: a 4-byte emoji with ctx=3 truncates
// to zero tokens, and so does any window entirely inside a run of continuation bytes.
//
// This is the behaviour internal/app depends on and describes — the status line has a branch for
// "提示词装不进 %d 个 token 的窗口" precisely because a prompt can produce no ids while the input
// box plainly holds text, and its comment records that ctx=1, 2 and 3 do exactly this. Nothing
// asserted it on the model side, so that branch rested on a claim in a comment.
//
// Not a defect: dropping a partial character is the point of advancing to a lead byte, and a
// window that cannot hold one character cannot hold anything. It is pinned because the app's
// empty-trace branch is only reachable through this, and because the doc comment on Truncate
// mentions the lead-byte rule without saying it can exhaust the input.
func TestTruncateCanReturnNothingForAWindowInsideOneCharacter(t *testing.T) {
	// One 4-byte rune, window of 3: the cut lands at byte 1, which is a continuation byte, so
	// start advances past the whole rune to 4 and the slice is empty.
	emoji := []int{0xF0, 0x9F, 0x98, 0x80}
	for _, ctx := range []int{1, 2, 3} {
		got := Truncate(append([]int{}, emoji...), ctx)
		if len(got) != 0 {
			t.Fatalf("Truncate(%v, ctx=%d) = %v, want empty: a window smaller than the 4-byte "+
				"rune cannot start on a lead byte inside it", emoji, ctx, got)
		}
	}
	// At 4 the whole rune fits and is kept.
	if got := Truncate(append([]int{}, emoji...), 4); len(got) != 4 {
		t.Fatalf("Truncate(%v, ctx=4) = %v, want all four bytes", emoji, got)
	}

	// The same rule with surrounding text: the head is dropped and what remains starts on a lead
	// byte. A 5-byte ASCII prefix plus the emoji is 9 bytes, and a window of 5 cuts at byte 4 —
	// which is 'e', already a lead byte, so the function stops there and keeps 'e' too. That is
	// the intended behaviour, not an over-cut: advancing exists to avoid starting mid-character,
	// and 'e' does not need advancing past.
	ids := append([]int{0x61, 0x62, 0x63, 0x64, 0x65}, emoji...)
	got := Truncate(append([]int{}, ids...), 5)
	if len(got) != 5 || got[0] != 0x65 {
		t.Fatalf("Truncate(%v, ctx=5) = %v, want the 5 bytes from index 4 (the 'e' plus the "+
			"whole rune)", ids, got)
	}

	// A window that starts exactly on a lead byte keeps that rune: advancing is for continuation
	// bytes only, and stopping one byte early would drop a character the window had room for.
	// Measured — with two ASCII bytes then the emoji, ctx=4 cuts at index 2, which is 0xF0, and
	// the four bytes of the rune are exactly what is left. A "skip the whole leading rune" rule
	// would return the emoji minus its first byte's whole character, i.e. nothing.
	twoThenEmoji := append([]int{0x61, 0x62}, emoji...)
	if got := Truncate(append([]int{}, twoThenEmoji...), 4); len(got) != 4 || got[0] != 0xF0 {
		t.Fatalf("Truncate(%v, ctx=4) = %v, want the 4-byte rune starting at index 2: a window "+
			"that begins on a lead byte must keep that character", twoThenEmoji, got)
	}

	// And the contract that always holds: when the result is non-empty it begins on a lead
	// byte, which is the property the doc comment states.
	for ctx := 1; ctx <= len(ids); ctx++ {
		got := Truncate(append([]int{}, ids...), ctx)
		if len(got) == 0 {
			continue
		}
		if byte(got[0])&0xC0 == 0x80 {
			t.Fatalf("Truncate(ctx=%d) starts on continuation byte %#x", ctx, got[0])
		}
	}
}
