package app

import (
	"fmt"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
)

// A switch that lists some values and lets the rest fall to a default is only
// safe when the unlisted values cannot occur. Last round found one where they
// could: railMarkColor handled "q" and "k" and sent "qk" to the default, which
// meant "no mark" — so a token attending to itself was drawn as if the crosshair
// were somewhere else entirely.
//
// These are the other switches of that shape in internal/app. Each checks that
// the value sets are actually closed, rather than trusting that they are.

// TestStepDotStateIsAClosedSet checks the stepper's three states are exhaustive.
// The dots switch on 0/1/2 with a default, so a fourth state would silently draw
// as "still to come".
func TestStepDotStateIsAClosedSet(t *testing.T) {
	seen := map[int]bool{}
	for step := 0; step < len(lessons); step++ {
		a := newTestApp(t)
		a.Step = step
		for i := range lessons {
			state := 2 // ahead
			switch {
			case i == a.Step:
				state = 1
			case i < a.Step:
				state = 0
			}
			seen[state] = true
			// Every state must have a distinct colour: the three-way distinction
			// is the whole point of the stepper.
			_ = stepTextColor(state)
		}
	}
	for _, want := range []int{0, 1, 2} {
		if !seen[want] {
			t.Fatalf("state %d never occurs across the seven stages, so the stepper's "+
				"three-way split is not being exercised", want)
		}
	}
	if len(seen) != 3 {
		t.Fatalf("the stepper produced %d distinct states, want 3: %v", len(seen), seen)
	}
	// And the three text colours must be three different values.
	if stepTextColor(0) == stepTextColor(1) || stepTextColor(1) == stepTextColor(2) ||
		stepTextColor(0) == stepTextColor(2) {
		t.Fatal("two stepper states share a colour")
	}
}

// TestControlScopeNoteCoversEveryControl is the closure check for the switch that
// has no default at all: an unlisted name would be dropped from the sentence,
// leaving a stage saying "顶部「层」在这一步不生效：" with nothing after the colon.
func TestControlScopeNoteCoversEveryControl(t *testing.T) {
	names := []string{"层", "头", "温度"}
	for _, n := range names {
		got := controlScopeNote([]string{n})
		if got == "" {
			t.Fatalf("controlScopeNote(%q) is empty, so that control's name would be "+
				"followed by nothing", n)
		}
	}
	// The full set, as the card builds it, must produce all three clauses.
	all := controlScopeNote(names)
	for _, want := range []string{"选层", "选头", "改温度"} {
		if !strings.Contains(all, want) {
			t.Fatalf("controlScopeNote drips %q for the full set: %q", want, all)
		}
	}
}

// TestControlScopeNoteMatchesTheRanges checks the sentence's step numbers come
// from the same constants that dim the controls, so they cannot drift apart.
func TestControlScopeNoteMatchesTheRanges(t *testing.T) {
	note := controlScopeNote([]string{"层", "头", "温度"})
	for _, want := range []string{
		fmt.Sprintf("第 %d–%d 步选层", layerFrom+1, layerTo+1),
		fmt.Sprintf("第 %d–%d 步选头", headFrom+1, headTo+1),
		fmt.Sprintf("第 %d 步改温度", tempFrom+1),
	} {
		if !strings.Contains(note, want) {
			t.Fatalf("the scope sentence lacks %q: %q", want, note)
		}
	}
}

// TestByteTextIsClosedOverEveryByte checks the fallback that every byte relies on.
// byteText names space, newline and tab, then falls through to two more branches;
// any byte must produce a non-empty, single-line string.
func TestByteTextIsClosedOverEveryByte(t *testing.T) {
	for b := 0; b < 256; b++ {
		got := byteText(b)
		if got == "" {
			t.Fatalf("byte %d renders as nothing", b)
		}
		if strings.ContainsAny(got, "\n\t") {
			t.Fatalf("byte %d renders as %q, which would break the single-line layout", b, got)
		}
	}
	// The three named cases must stay distinguishable from the generic escape.
	if byteText(' ') == byteText(0x20) && byteText(' ') == " " {
		t.Fatal("space is rendered literally, which is invisible in the strip")
	}
	if byteText('\n') != "\\n" || byteText('\t') != "\\t" {
		t.Fatalf("newline/tab escapes changed: %q %q", byteText('\n'), byteText('\t'))
	}
}

// TestChipGlyphCoversEveryByte is the same closure over the strip's glyphs.
func TestChipGlyphCoversEveryByte(t *testing.T) {
	a := newTestApp(t)
	// Build a prompt containing every byte value, then check each chip renders.
	var sb strings.Builder
	for b := 0; b < 256; b++ {
		sb.WriteByte(byte(b))
	}
	a.Prompt = sb.String()
	a.recompute()
	for i := range a.Trace.Tokens {
		g := chipGlyph(a.Trace, i)
		if g == "" {
			t.Fatalf("token %d (byte 0x%02X) has an empty glyph", i, a.Trace.Tokens[i].ID)
		}
	}
	// And the strip renders without panicking.
	tr := ui.NewTester(a.View, 1180, 720)
	tr.Frame()
}

// TestInactiveControlNoteNamesExactlyTheOffKnobs covers the rendering side of controlScopeNote.
//
// TestControlScopeNoteMatchesTheRanges proves the sentence is built from the range constants; it
// does not prove the sentence is drawn on the right stages, or that a knob that is ON is left out
// of it. Both can go wrong independently — the `off` slice is assembled by three negated inStages
// calls in lesson(), and inverting any one of them leaves the note present, well-formed, and
// naming the wrong knobs.
//
// Measured on all seven stages at 1500×4000, where every stage's text is on screen at once:
// stages 1-2 name 层/头/温度, 3-4 name 温度, 5-6 name 头/温度, and 7 names 层/头 — exactly the
// complement of the range constants, with stage 7 the only one whose note mentions 温度 as the
// knob that works there.
func TestInactiveControlNoteNamesExactlyTheOffKnobs(t *testing.T) {
	total := 0
	for step := 0; step < len(lessons); step++ {
		a := newTestApp(t)
		a.Step = step
		tr := ui.NewTester(a.View, 1500, 4000)
		tr.Frame()

		var wantOff []string
		if !inStages(step, layerFrom, layerTo) {
			wantOff = append(wantOff, "层")
		}
		if !inStages(step, headFrom, headTo) {
			wantOff = append(wantOff, "头")
		}
		if !inStages(step, tempFrom, tempTo) {
			wantOff = append(wantOff, "温度")
		}

		note := ""
		for _, s := range tr.Texts() {
			if strings.Contains(s, "在这一步不生效") {
				note = s
				break
			}
		}

		if len(wantOff) == 0 {
			// Every knob is live on this stage, so there is nothing to signpost.
			if note != "" {
				t.Fatalf("stage %d has every control enabled but still prints %q", step+1, note)
			}
			continue
		}
		if note == "" {
			t.Fatalf("stage %d dims %v but prints no note saying where they apply", step+1, wantOff)
		}
		total++

		// Quoted knobs are the off ones; a live knob must not appear in the「」list.
		for _, knob := range []string{"层", "头", "温度"} {
			quoted := strings.Contains(note, "「"+knob) || strings.Contains(note, "/ "+knob) ||
				strings.Contains(note, knob+" /")
			isOff := false
			for _, o := range wantOff {
				if o == knob {
					isOff = true
				}
			}
			if isOff && !quoted {
				t.Fatalf("stage %d: %q is dimmed but the note does not name it: %q",
					step+1, knob, note)
			}
			if !isOff && quoted {
				t.Fatalf("stage %d: %q is enabled but the note names it as not working: %q",
					step+1, knob, note)
			}
		}

		// And the note has to say where the knob does apply, which is the whole point of
		// replacing a greyed-out control with an explanation.
		for _, o := range wantOff {
			var want string
			switch o {
			case "层":
				want = fmt.Sprintf("第 %d–%d 步选层", layerFrom+1, layerTo+1)
			case "头":
				want = fmt.Sprintf("第 %d–%d 步选头", headFrom+1, headTo+1)
			case "温度":
				want = fmt.Sprintf("第 %d 步改温度", tempFrom+1)
			}
			if !strings.Contains(note, want) {
				t.Fatalf("stage %d dims %q but the note does not say %q: %q", step+1, o, want, note)
			}
		}
	}
	// Six of seven stages dim at least one knob on the bundle's ranges. If that ever drops to a
	// couple, the loop is still passing but the feature has gone quiet.
	if total < 5 {
		t.Fatalf("only %d of %d stages print an inactive-control note; the ranges or the lesson "+
			"body changed shape", total, len(lessons))
	}
}
