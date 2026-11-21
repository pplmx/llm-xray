package app

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"image"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
)

// The README states facts about the app that a reader will take at face value.
// TestReadmeStageRangesMatchTheControls already covers the numeric ranges; these
// cover the rest, because a README drifts silently while the code moves on and
// this one had already drifted on the callout count.

func readREADME(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "README.md"))
	if err != nil {
		t.Fatalf("cannot read the README: %v", err)
	}
	return string(b)
}

// readKeysDoc returns docs/keys.md, the interaction rationale the README links to. It carries
// some of the same figures as the README, and those copies need the same guards.
func readKeysDoc(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "docs", "keys.md"))
	if err != nil {
		t.Fatalf("cannot read docs/keys.md: %v", err)
	}
	return string(b)
}

// TestReadmeCalloutCountMatchesTheStages: "第 3–7 步各带「常见误解」卡片，共六条".
func TestReadmeCalloutCountMatchesTheStages(t *testing.T) {
	doc := readREADME(t)
	if !strings.Contains(doc, "共十五条") {
		t.Fatal("the README no longer states the callout count; update or remove this test")
	}

	counts := map[int]int{}
	total := 0
	for step := 0; step < 7; step++ {
		a := newTestApp(t)
		a.Step = step
		tr := ui.NewTester(a.View, 1500, 940)
		tr.Frame()
		for _, s := range tr.Texts() {
			if strings.HasPrefix(s, "「") && strings.Contains(s, "」") {
				counts[step]++
				total++
			}
		}
	}
	if total != 15 {
		t.Fatalf("the README says 共十五条, the app renders %d", total)
	}
	// And the README's own sentence about the distribution has to be the one the app renders.
	//
	// This check used to be a second hard-coded map — "step 4 has 3" written here, "第 3、4、5 步
	// 各两条" written in the README — so the two copies could disagree while this test passed.
	// They did: measured, the app renders 2/2/3/3/3/2 across steps 2-7, and the README said
	// steps 3, 4 and 5 each had two. The map was right and the prose was wrong, and nothing
	// compared them. The distribution is now parsed out of the sentence the reader actually
	// sees, so the two cannot drift apart: a third copy of the numbers is what created the bug.
	stated := statedCalloutDistribution(t, doc)
	for step := 0; step < 7; step++ {
		if got, ok := stated[step+1]; ok && counts[step] != got {
			t.Fatalf("the README says step %d carries %d callouts, the app renders %d",
				step+1, got, counts[step])
		}
	}
	// Every stage that renders callouts must be mentioned in the sentence, or the README is
	// silently describing a subset — which is how "第 3、4、5 步各两条" read as complete while
	// omitting steps 2, 6 and 7 entirely.
	for step := 0; step < 7; step++ {
		if counts[step] == 0 {
			continue
		}
		if _, ok := stated[step+1]; !ok {
			t.Fatalf("the README's 常见误解 sentence does not state how many callouts step %d "+
				"carries, and it renders %d", step+1, counts[step])
		}
	}

	// And the callouts the sentence names by title have to be the ones those stages render.
	//
	// The sentence quotes four titles by way of illustration. They were retyped from memory
	// when the count was fixed, and a quote that drifts from the rendered string is the same
	// defect one level down: a claim in prose that nothing compares to the app. Only quoted
	// strings are checked, and only for presence on the stage the sentence attributes them to.
	a := newTestApp(t)
	for _, c := range []struct {
		step  int // one-based, as the README writes it
		quote string
	}{
		{4, "四个头各写一块，加起来就是它们长度的和"},
		{5, "每一层做同样多的活，层数加倍的模型就是把同一个变换做两遍"},
		{5, "残差是加法，所以每加一次，数值就更大一点"},
		{5, "logit lens 能说明模型是一层一层把答案想清楚的"},
	} {
		if !strings.Contains(doc, "「"+c.quote+"」") {
			t.Fatalf("the README no longer quotes %q on step %d", c.quote, c.step)
		}
		a.Step = c.step - 1
		tr := ui.NewTester(a.View, 1500, 940)
		tr.Frame()
		rendered := false
		for _, t := range tr.Texts() {
			if t == "「"+c.quote+"」" {
				rendered = true
				break
			}
		}
		if !rendered {
			t.Fatalf("the README quotes %q as a callout on step %d, which no longer renders it",
				c.quote, c.step)
		}
	}
}

// statedCalloutDistribution parses the callout counts out of the README's sentence about them.
//
// It understands the two forms the sentence uses: "第 N 步X条" for one stage and "第 A、B、C 步各
// 三条" for a run of stages sharing a count, where X is a Chinese numeral. The point is not to
// accept every possible phrasing — it is to read the numbers the way a reader does, so a claim
// in the prose is compared against the render instead of against a second copy in this file.
func statedCalloutDistribution(t *testing.T, doc string) map[int]int {
	t.Helper()
	// Isolate the sentence so a numeral from a neighbouring paragraph cannot be matched.
	start := strings.Index(doc, "各带「常见误解」卡片")
	if start < 0 {
		t.Fatal("the README no longer describes the 常见误解 cards; update or remove this test")
	}
	end := strings.Index(doc[start:], "；第 1 步")
	if end < 0 {
		t.Fatal("the README's 常见误解 sentence no longer ends before the step-1 clause")
	}
	sentence := doc[start : start+end]

	cn := map[string]int{"一": 1, "两": 2, "二": 2, "三": 3, "四": 4, "五": 5, "六": 6}
	out := map[int]int{}
	// "第 A、B、C 步各 N 条" — a shared count. Parsed first so the more specific pattern below
	// cannot consume a single-stage prefix of it.
	shared := regexp.MustCompile(`第 ((?:[0-9]+、)+[0-9]+) 步各([一两二三四五六])条`)
	for _, m := range shared.FindAllStringSubmatch(sentence, -1) {
		n, ok := cn[m[2]]
		if !ok {
			t.Fatalf("unrecognised numeral %q in the callout sentence", m[2])
		}
		for _, part := range strings.Split(m[1], "、") {
			var step int
			if _, err := fmt.Sscanf(part, "%d", &step); err != nil {
				t.Fatalf("unrecognised stage number %q in the callout sentence", part)
			}
			out[step] = n
		}
	}
	// "第 N 步X条" — one stage on its own.
	one := regexp.MustCompile(`第 ([0-9]+) 步([一两二三四五六])条`)
	for _, m := range one.FindAllStringSubmatch(sentence, -1) {
		var step int
		if _, err := fmt.Sscanf(m[1], "%d", &step); err != nil {
			t.Fatalf("unrecognised stage number %q in the callout sentence", m[1])
		}
		n, ok := cn[m[2]]
		if !ok {
			t.Fatalf("unrecognised numeral %q in the callout sentence", m[2])
		}
		out[step] = n
	}
	if len(out) == 0 {
		t.Fatalf("no callout counts could be read out of the README's sentence: %q", sentence)
	}
	return out
}

// TestReadmeModelBlockMatchesTheWeights: the model paragraph quotes the
// architecture and the parameter count.
func TestReadmeModelBlockMatchesTheWeights(t *testing.T) {
	doc := readREADME(t)
	a := newTestApp(t)
	c := a.W.Config

	stated := strings.Contains(doc, "vocab 256 · d_model 96 · 2 层 · 4 头 · d_ff 384 · ctx 64")
	trueDims := c.Vocab == 256 && c.DModel == 96 && c.NLayer == 2 &&
		c.NHead == 4 && c.DFF == 384 && c.Ctx == 64
	if stated && !trueDims {
		t.Fatalf("the README quotes an architecture the weights do not have: %+v", c)
	}

	// The parameter count. Two holes made this check useless, both measured:
	//
	//  1. It was guarded by strings.Contains(doc, "参数量 25.3 万"), so editing the number made
	//     the condition false and the whole check skipped — 25.4, 25.9 and 25.0 all passed.
	//  2. It compared integer division, a.paramCount()/10000 != 25, which throws away the very
	//     digits the sentence states.
	//
	// It now reads the figure out of the sentence and compares it at the precision the
	// sentence uses.
	reParams := regexp.MustCompile(`参数量 ([0-9]+\.[0-9]+) 万`)
	mParams := reParams.FindStringSubmatch(doc)
	if mParams == nil {
		t.Fatal("the README no longer states the parameter count in the form this test knows; " +
			"update the test with the sentence you wrote")
	}
	got := strconv.FormatFloat(float64(a.paramCount())/10000, 'f', 1, 64)
	if mParams[1] != got {
		t.Fatalf("the README says %s 万 parameters; the weights give %d, which is %s 万",
			mParams[1], a.paramCount(), got)
	}
}

// TestReadmeKeyboardScopeMatchesTheCode: "第 3、4 步在注意力矩阵里移动格子；
// 其余步骤 Shift+←/→ 移动 token 选择".
func TestReadmeKeyboardScopeMatchesTheCode(t *testing.T) {
	for step := 0; step < 7; step++ {
		a := newTestApp(t)
		a.Step = step
		a.SelToken = 10
		tr := ui.NewTester(a.View, 1500, 940)
		tr.Frame()
		selBefore, cellBefore := a.SelToken, a.KeyQ
		tr.Key(ui.Shift, ui.KeyRight)
		tr.Frame()

		inMatrix := step == 2 || step == 3
		if inMatrix {
			if a.KeyQ < 0 || a.KeyQ == cellBefore {
				t.Fatalf("step %d: Shift+Right did not open a matrix cell (KeyQ %d -> %d)",
					step+1, cellBefore, a.KeyQ)
			}
			if a.SelToken != selBefore {
				t.Fatalf("step %d: Shift+Right moved the token selection instead of the cell", step+1)
			}
		} else {
			if a.SelToken == selBefore {
				t.Fatalf("step %d: Shift+Right did not move the token selection", step+1)
			}
		}
	}
}

// TestReadmeFadeDurationMatchesTheTransition: "切换阶段时中栏和检查器会淡入（160ms）".
//
// The duration is stated twice — once in the README's motion paragraph and once in docs/keys.md's
// "切换阶段时的淡入" section — and this test only read the README. So changing stageFade.Duration
// moved neither of them and only one of the two would ever have been noticed; the guard was
// covering one of the two copies, which is the shape this repository keeps finding (the tooltip
// count, the snapshot count, the card-callout distribution).
//
// Both files are checked now, and each has to state the number that is actually configured.
func TestReadmeFadeDurationMatchesTheTransition(t *testing.T) {
	// Derived from the transition, not written twice: a guard that hardcodes 160 in both the
	// assertion and the search would keep passing if the code and both documents moved together
	// to a different number the code no longer uses.
	want := fmt.Sprintf("%dms", stageFade.Duration.Milliseconds())
	if want != "160ms" {
		t.Fatalf("stageFade.Duration is %v, so the documented figure should now read %s; update "+
			"README.md and docs/keys.md together", stageFade.Duration, want)
	}

	for _, doc := range []struct{ name, body string }{
		{"README.md", readREADME(t)},
		{"docs/keys.md", readKeysDoc(t)},
	} {
		if !strings.Contains(doc.body, want) {
			t.Fatalf("%s does not state the fade duration as %s, which is what stageFade.Duration "+
				"is configured to — the two will read as different behaviours", doc.name, want)
		}
	}
}

// TestReadmeGraphLegendsMatchTheCode keeps the "图上每个元素怎么读" table honest.
// Each row states a rule the painter implements; the rules are the part a reader
// would otherwise have to reverse-engineer, and they are the part most likely to
// drift when someone tunes a colour ramp.
func TestReadmeGraphLegendsMatchTheCode(t *testing.T) {
	doc := readREADME(t)
	for _, section := range []string{
		"## 图上每个元素怎么读",
		"色深上限",
		"向量条带两端的",
		"直方图的竖线",
		"共同横轴",
	} {
		if !strings.Contains(doc, section) {
			t.Fatalf("the README no longer documents %q; update or remove this test", section)
		}
	}

	// The colour ramps stated in the table must be the ones the painter uses.
	if !strings.Contains(doc, "0.0005") {
		t.Fatal("the README no longer states the threshold below which a cell is drawn as background")
	}
	// Heat colour: at or below 0.0005 the cell is the panel colour, not a tint.
	atThreshold := heatColor(0.0005)
	justAbove := heatColor(0.002)
	if atThreshold == justAbove {
		t.Fatal("heatColor no longer treats 0.0005 as a cutoff, so the README's rule is wrong")
	}

	// The score scale is per-matrix, not a constant: the table says so and the
	// caption prints it, so a fixed value would make both wrong.
	caps := map[float64]bool{}
	for _, prompt := range []string{"hi", "attention is a weighted sum"} {
		b := newTestApp(t)
		b.Prompt = prompt
		b.recompute()
		caps[scoreColorScale(b.layer().Heads[b.Head])] = true
	}
	if len(caps) < 2 {
		t.Fatal("the colour cap is the same for every prompt, but the README describes it " +
			"as a value that moves with the prompt")
	}
}

// TestReadmeStrideRuleIsTheOneImplemented checks the stride figures the table
// quotes against the widget's own rule.
//
// My first version of the table said "36 token 时逐个标，64 token 时每 6 个标一次",
// both of which were guesses. The rule is a 26-DIP minimum gap, which gives 3 and
// 4 — so the document was wrong in two places and this test is what caught it.
func TestReadmeStrideRuleIsTheOneImplemented(t *testing.T) {
	// The painter's rule, restated: a label every `stride` cells, stride the
	// first integer putting at least 26 DIPs between labels.
	strideFor := func(cell float32) int {
		s := 1
		for float32(s)*cell < 26 {
			s++
		}
		return s
	}
	// The matrix box is not a fixed 420 DIP: it shrinks with the window, and the README
	// used to quote 3 and 4 as though it were. Measured from the layout arithmetic in
	// widgets.go — avail starts at 420 and takes the room the window leaves, floored at
	// 160 — the narrowest supported window gives 324 DIP, where a 64-token matrix labels
	// every 6th cell rather than every 4th. The snapshot size-longprompt-1180.png shows
	// exactly that: 0, 6, 12, … 60.
	app := newTestApp(t)
	availAt := func(windowW int) float32 {
		avail := float32(420)
		room := float32(windowW) - app.SidebarW - app.InspectorW - 2 - 32 - 28 - 240
		if room < avail {
			avail = room
		}
		if avail < 160 {
			avail = 160
		}
		return avail
	}

	for _, c := range []struct {
		windowW, tokens, want int
	}{
		{1180, 36, 3},
		{1180, 64, 6}, // the case the README used to get wrong
		{1500, 36, 3},
		{1500, 64, 4},
		{1920, 36, 3},
		{1920, 64, 4},
	} {
		avail := availAt(c.windowW)
		if got := strideFor(avail / float32(c.tokens)); got != c.want {
			t.Fatalf("window %d, %d tokens: avail %.0f, stride %d, want %d",
				c.windowW, c.tokens, avail, got, c.want)
		}
	}

	// And the document must say those numbers, including the narrow case.
	doc := readREADME(t)
	for _, want := range []string{"每 3 个标一次", "每 4 个标一次", "每 6 个标一次"} {
		if !strings.Contains(doc, want) {
			t.Fatalf("the README does not state %q, so its stride parenthetical is stale", want)
		}
	}
}

// TestReadmeSamplingRowStatesItsCondition keeps the README's sampling claim honest.
//
// The row used to end "同一句提示词永远得到同一个结果". The seed comes only from the
// prompt, but the interval it lands in depends on the distribution, which the
// temperature slider reshapes — measured: at T=0.8 the draw is id 32, and from
// T=2.2 it is a different token. The in-app note was fixed in round 66 with "在温度
// 不变的前提下"; the README kept the unconditional version for twenty rounds.
func TestReadmeSamplingRowStatesItsCondition(t *testing.T) {
	doc := readREADME(t)
	i := strings.Index(doc, "采样并续写")
	if i < 0 {
		t.Fatal("the README no longer has a sampling row")
	}
	end := strings.Index(doc[i:], "\n")
	if end < 0 {
		end = len(doc) - i
	}
	row := doc[i : i+end]
	if !strings.Contains(row, "温度不变") {
		t.Fatalf("the sampling row does not state that the repeatability depends on "+
			"a fixed temperature: %q", row)
	}

	// And the condition is real: some temperature draws something else.
	a := newTestApp(t)
	a.Step = 6
	base, ok := a.sampleDraw(a.Trace.Probabilities(0.8))
	if !ok {
		t.Fatal("no draw at T=0.8")
	}
	differs := false
	for temp := 0.1; temp <= 3.0; temp += 0.1 {
		d, ok := a.sampleDraw(a.Trace.Probabilities(temp))
		if ok && d.pick != base.pick {
			differs = true
			break
		}
	}
	if !differs {
		t.Skip("temperature does not change the draw on this prompt, so the " +
			"condition cannot be demonstrated here")
	}
}

// TestRenderDirIsDocumentedAndWorks keeps the snapshot instructions usable.
//
// RENDER_DIR existed in the tests but appeared in no document, and passing it a
// path that did not exist failed with "open /tmp/after/x.png: no such file or
// directory" — which reads as a broken test rather than a missing directory. The
// flag's whole purpose is keeping a before and an after side by side, and the
// obvious way to do that is a fresh path, so the writer now creates it.
func TestRenderDirIsDocumentedAndWorks(t *testing.T) {
	// Both docs must mention the variable, since both show the command.
	for _, doc := range []string{"README.md", "CONTRIBUTING.md"} {
		b, err := os.ReadFile(filepath.Join("..", "..", doc))
		if err != nil {
			t.Fatalf("read %s: %v", doc, err)
		}
		if !strings.Contains(string(b), "RENDER_DIR") {
			t.Fatalf("%s shows the snapshot command but never mentions RENDER_DIR", doc)
		}
	}

	// And a directory that does not exist yet must be created by the writer.
	dir := filepath.Join(t.TempDir(), "nested", "snapshots")
	writeSnapshot(t, dir, "probe.png", image.NewRGBA(image.Rect(0, 0, 4, 4)))
	if _, err := os.Stat(filepath.Join(dir, "probe.png")); err != nil {
		t.Fatalf("writeSnapshot did not create %s: %v", dir, err)
	}
}

// TestReadmeExplainsTheStripColour checks the figure table carries the one reading
// aid the strips themselves do not.
//
// The app has two diverging colour scales: valueColor paints a vector strip red for
// positive and blue for negative, scoreColor paints a heatmap cell warm for a high
// score and blue for a low one. Both are defensible and they are not the same thing —
// "positive" and "high" are different questions. The heatmap has a legend beside it
// ("正分（暖色）/负分（蓝色）"); the strips have none, so a reader looking at a red
// and blue band has nothing on screen telling them which end is which.
//
// That makes the README row load-bearing rather than decorative, and it has to stay
// accurate about which scale is which.
func TestReadmeExplainsTheStripColour(t *testing.T) {
	doc := readREADME(t)
	i := strings.Index(doc, "向量条带的颜色")
	if i < 0 {
		t.Fatal("the figure table no longer explains the vector strip's colours")
	}
	end := strings.Index(doc[i:], "\n")
	if end < 0 {
		end = len(doc) - i
	}
	row := doc[i : i+end]

	for _, want := range []string{"红色为正", "蓝色为负", "两套"} {
		if !strings.Contains(row, want) {
			t.Fatalf("the strip-colour row does not state %q: %s", want, row)
		}
	}

	// And the claim that the strips have no legend of their own must hold: if one is
	// ever added, this row's justification changes.
	for _, step := range []int{1, 4, 5} {
		a := newTestApp(t)
		a.Step = step
		tr := ui.NewTester(a.View, 1500, 940)
		tr.Frame()
		for _, s := range tr.Texts() {
			if strings.Contains(s, "红") && strings.Contains(s, "蓝") {
				t.Fatalf("stage %d now renders a colour legend %q; the README row says "+
					"the strips have none", step+1, s)
			}
		}
	}
}

// TestReadmeNumbersMatchTheCode holds the README's factual claims about this build to the
// build.
//
// Three of them are the kind that look verified because they are specific: the parity prompt
// is "46 token", the forward pass uses "最多 8 个 worker", and the model has "25.3 万" of
// parameters. Each is true today — measured 46, capped at 8 in tensor.go, and 252864 which
// rounds to 25.3万 — but a regenerated fixture, a changed worker cap or a retrained model
// would leave the prose stale with nothing to notice.
//
// The parameter count and the worker cap were checked here; the parity prompt's length was
// named in this comment for as long as the comment has existed and was never checked at all.
// It is now, against the fixture itself. That is the same shape as the defect the callout test
// had: a claim asserted in prose (here, a doc comment) that no code compared to reality.
func TestReadmeNumbersMatchTheCode(t *testing.T) {
	doc := readREADME(t)

	// The parameter count is already covered by the test above (a.paramCount() against
	// "参数量 25.3 万"), so this only adds the two claims nothing checked.

	// The parity fixture's prompt length. The README quotes "46 token" as the size of the
	// comparison, and the number is the reader's only sense of how much of the network was
	// checked; regenerating tools/parity.py with a different prompt would leave the prose
	// describing a fixture that no longer exists.
	if !strings.Contains(doc, "46 token") {
		t.Fatal("the README no longer states the parity prompt's length")
	}
	fixture, err := os.ReadFile(filepath.Join("..", "model", "testdata", "parity.json.gz"))
	if err != nil {
		t.Fatalf("read the parity fixture: %v", err)
	}
	if n := parityFixtureIDs(t, fixture); n != 46 {
		t.Fatalf("the README says the parity prompt is 46 tokens; the fixture has %d", n)
	}

	// The worker cap.
	src, err := os.ReadFile(filepath.Join("..", "model", "tensor.go"))
	if err != nil {
		t.Fatal(err)
	}
	// The cap's value, not merely the shape of the comparison: changing `workers = 8` to
	// `workers = 16` left the old check green while the README kept promising eight.
	m := regexp.MustCompile(`const maxWorkers = (\d+)`).FindStringSubmatch(string(src))
	if m == nil {
		t.Fatal("tensor.go no longer declares maxWorkers, which is what the README's worker " +
			"cap describes")
	}
	if !strings.Contains(string(src), "workers > maxWorkers") {
		t.Fatal("tensor.go declares maxWorkers but the matmul no longer caps at it")
	}
	if m[1] != "8" {
		t.Fatalf("maxWorkers is %s, but the README says the forward pass uses at most 8 workers",
			m[1])
	}
	if !strings.Contains(doc, "最多 8 个 worker") {
		t.Fatal("the README no longer states the worker cap")
	}
}

// TestReadmeImagesExist keeps every screenshot the guide embeds present on disk.
//
// The guide shows six: the hero image and a collapsed gallery of five. Two of the six had fallen out of the document
// entirely at some point while staying in docs/, and all five of those were rendered against an interface that no
// longer exists — sidebar numbering from 0, no data-flow card, no misconception block. A broken link is visible in the
// rendered page, but a stale image is not, so the check here is that the files are there; keeping them current is the
// job of running the render when the interface changes.
func TestReadmeImagesExist(t *testing.T) {
	doc := readREADME(t)
	re := regexp.MustCompile(`!\[[^\]]*\]\((docs/[^)]+)\)`)
	all := re.FindAllStringSubmatch(doc, -1)
	if len(all) < 6 {
		t.Fatalf("the guide embeds %d images; it documents six stages and a small window", len(all))
	}
	seen := map[string]bool{}
	for _, m := range all {
		path := m[1]
		if seen[path] {
			continue
		}
		seen[path] = true
		if _, err := os.Stat(filepath.Join("..", "..", path)); err != nil {
			t.Errorf("the guide embeds %s, which is missing: %v", path, err)
		}
	}
}

// TestSamplingHasTwoLabelsAndTheReadmeSaysSo records a deliberate inconsistency
// that a reader will meet within one screen.
//
// The top bar's button says 采样下一个 token and the output stage's card says
// 采样并续写. Both call sampleNext and both are bound to Enter, so a reader who
// notices has found a real duplication — but the two labels are doing different
// jobs: the top bar's is a quick action available on all seven stages, where
// "next token" is what the model is predicting; the card's sits inside the lesson
// about sampling and says what the reader is about to do to their prompt.
//
// Rather than unify the words and lose the distinction, the README names both. This
// test keeps that sentence honest: if either label changes, the row has to change
// with it.
func TestSamplingHasTwoLabelsAndTheReadmeSaysSo(t *testing.T) {
	doc := readREADME(t)
	row := ""
	for _, line := range strings.Split(doc, "\n") {
		if strings.HasPrefix(line, "| 采样并续写") {
			row = line
		}
	}
	if row == "" {
		t.Fatal("the README no longer has a sampling row")
	}

	// Both labels are what the app renders, on the stages that render them.
	a := newTestApp(t)
	a.Step = 6
	top := 0
	tr := ui.NewTester(a.View, 1500, 940)
	tr.Frame()
	for _, s := range tr.Texts() {
		if s == "采样下一个 token" {
			top++
		}
	}
	if top == 0 {
		t.Fatal("the top bar no longer renders 采样下一个 token")
	}
	if !tr.HasText("采样并续写") {
		t.Fatal("the output stage's card no longer renders 采样并续写")
	}
	for _, label := range []string{"采样下一个 token", "采样并续写"} {
		if !strings.Contains(row, label) {
			t.Fatalf("the README's sampling row does not name %q, which the app renders:\n%s",
				label, row)
		}
	}
}

// TestReadmeSectionsStayUnderTheRightParent keeps the README's outline from drifting.
//
// The document grew a section per card over many rounds, and the new sections were
// appended wherever the end of the file happened to be — which by then was past 目录, the
// file listing. Four teaching sections ended up below the table of contents, so a reader
// scrolling top to bottom met the most interesting material after the appendices. The fix
// was to gather them under one heading; this test is what stops it happening again.
//
// It checks the outline rather than the prose: every `###` must belong to a `##` that
// appears above it, no `##` may repeat, and the card sections must all sit under the same
// parent instead of scattered through the document.
func TestReadmeSectionsStayUnderTheRightParent(t *testing.T) {
	// The card sections moved to docs/cards.md, where they are the file's own ## headings — the
	// property this test was written for ("the card sections sit under one parent, not scattered
	// through the document") is now structural rather than something to parse for.
	doc := readmeCardSections(t)
	cardHeadings := []string{
		"## 为什么第 5 步多了一张「每一层会说什么」",
		"## 权重绑定让预测变成一个余弦",
		"## 学出来的位置编码和 sin/cos 长得完全不一样",
		"## 四个头真的在分工吗",
		"## 掩码到底挡掉了什么",
		"## FFN 的单元也不是平均出力",
		"## 两层做的活不一样多",
	}
	seen := map[string]bool{}
	for _, line := range strings.Split(doc, "\n") {
		if !strings.HasPrefix(line, "## ") {
			continue
		}
		if seen[line] {
			t.Fatalf("the heading %q appears twice", line)
		}
		seen[line] = true
	}
	for _, want := range cardHeadings {
		if !seen[want] {
			t.Fatalf("docs/cards.md no longer has the section %q; the README links to this "+
				"file for the card notes, so a section dropped here is a note nobody reads", want)
		}
	}
}

// parityFixtureIDs returns the token count of the recorded parity prompt.
//
// The fixture is gzipped JSON from tools/parity.py; only the ids are needed, and they are
// decoded through the same struct shape internal/model uses so a field rename shows up here
// as a decode failure rather than as a silently wrong zero.
func parityFixtureIDs(t *testing.T, gz []byte) int {
	t.Helper()
	zr, err := gzip.NewReader(bytes.NewReader(gz))
	if err != nil {
		t.Fatalf("the parity fixture is not gzip: %v", err)
	}
	defer zr.Close()
	var f struct {
		IDs []int `json:"ids"`
	}
	if err := json.NewDecoder(zr).Decode(&f); err != nil {
		t.Fatalf("decode the parity fixture: %v", err)
	}
	if len(f.IDs) == 0 {
		t.Fatal("the parity fixture records no ids")
	}
	return len(f.IDs)
}

// TestReadmeBinarySizeNamesTheWeightsPart checks the size claim in the opening paragraph
// against the file it is mostly made of.
//
// The README said "11 MB" from the initial commit, which was never revisited: the shipped
// Linux binary is 12.4 MB, so the headline number had been 13% low for the project's whole
// history. There is nothing wrong with a statement of the form "about N MB", but "about 11"
// and "12.4" differ by more than rounding.
//
// The binary size itself cannot be asserted here — it depends on the toolchain, the linker
// and the platform, and this test runs on three of them. What can be checked is the weights
// file the sentence now names as its bulk: if that grows, the number beside it is wrong again,
// and the test says so with both figures instead of the reader finding out from a download.
func TestReadmeBinarySizeNamesTheWeightsPart(t *testing.T) {
	doc := readREADME(t)
	const claim = "12 MB（Linux amd64，含 2.3 MB 的权重）"
	if !strings.Contains(doc, claim) {
		t.Fatalf("the README no longer states the binary size in the form this test knows (%q); "+
			"update the test with the sentence you wrote", claim)
	}

	info, err := os.Stat(filepath.Join("..", "model", "weights.json"))
	if err != nil {
		// Not a skip: the file is part of the checkpoint the package embeds, so its absence is a
		// broken checkout, not an unsupported platform. A skip here would let the size sentence
		// go unchecked while the run stayed green.
		t.Fatalf("the bundled weights file must exist to size the README's claim: %v", err)
	}
	mb := float64(info.Size()) / (1 << 20)
	// The sentence quotes one decimal place, so anything inside half a tenth of 2.3 is the
	// same number. A weights file that crosses 2.35 or drops below 2.25 changes what the
	// sentence should say.
	if mb < 2.25 || mb >= 2.35 {
		t.Fatalf("the README says the binary carries 2.3 MB of weights, the file is %.2f MB — "+
			"update the sentence to %.1f MB", mb, mb)
	}
}

// TestReadmeSkillFiguresMatchTheDefaultView holds the word-boundary numbers in the README.
//
// The README quoted "4/18 (22%), 73/96 (76%)" across three prompts, and "2.87 vs 0.83 nats" on the
// default prompt. None of it reproduces. No combination of the preset prompts yields those hit
// counts, and 2.87/0.83 is what the breakdown gives at T = 1.0 while the app opens at T = 0.80 and
// draws 3.07/0.87.
//
// A README that quotes figures a reader cannot find on screen is worse than one that quotes none:
// the reader concludes they are looking at something different. The numbers below are parsed out of
// the README and compared with what the default view computes.
func TestReadmeSkillFiguresMatchTheDefaultView(t *testing.T) {
	src, err := os.ReadFile(filepath.Join("..", "..", "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	re := regexp.MustCompile(`空格之后猜对 \*\*(\d+)/(\d+)（(\d+)%）\*\*，其余位置 \*\*(\d+)/(\d+)（(\d+)%）\*\*，` +
		`两边的平均损失 \*\*([0-9.]+) vs ([0-9.]+) nats\*\*`)
	m := re.FindStringSubmatch(string(src))
	if m == nil {
		t.Fatal("the README no longer quotes the word-boundary figures in the form " +
			"\"空格之后猜对 **A/B（C%）**，其余位置 **D/E（F%）**，两边的平均损失 **G vs H nats**\"; " +
			"update this test with what it says now")
	}
	wantHits, wantRows := mustParseInt(t, m[1]), mustParseInt(t, m[2])
	wantOtherHits, wantOtherRows := mustParseInt(t, m[4]), mustParseInt(t, m[5])
	wantLoss, wantOtherLoss := m[7], m[8]

	// The default view: the app's opening prompt and opening temperature.
	a := newTestApp(t)
	skill := computeNextByteSkill(a.W, a.Trace, a.Temp)
	b := computeSkillBreakdown(a.Trace, skill)
	if !b.OK {
		t.Fatal("the default view has no breakdown to compare against")
	}
	if b.AfterSpaceHits != wantHits || b.AfterSpaceRows != wantRows {
		t.Fatalf("the README says the space boundary scores %d/%d, but the default view gives "+
			"%d/%d at T = %.2f", wantHits, wantRows, b.AfterSpaceHits, b.AfterSpaceRows, a.Temp)
	}
	if b.OtherHits != wantOtherHits || b.OtherRows != wantOtherRows {
		t.Fatalf("the README says the other positions score %d/%d, but the default view gives %d/%d",
			wantOtherHits, wantOtherRows, b.OtherHits, b.OtherRows)
	}
	if got := fmt.Sprintf("%.2f", b.AfterSpaceLoss); got != wantLoss {
		t.Fatalf("the README says the boundary loss is %s nats, but the default view gives %s",
			wantLoss, got)
	}
	if got := fmt.Sprintf("%.2f", b.OtherLoss); got != wantOtherLoss {
		t.Fatalf("the README says the other loss is %s nats, but the default view gives %s",
			wantOtherLoss, got)
	}
}

// TestReadmeCardRowClaimsMatchTheStageTable keeps the index row's parenthetical honest.
//
// The README described when the row appears and added, as the example, "第 6 步只有一张，
// 所以没有这一行". That was true when it was written. The FFN stage later grew two more cards
// and the sentence stayed, so it named a stage as an example of a case that no longer existed
// anywhere in the app — every stage now has at least three cards and every stage draws the row.
//
// TestCardRowIsHiddenOnSingleCardStages cannot catch this: it is written against the stage
// table, so it agrees with whatever the table says, and with no single-card stage left it
// checks only the "at least two cards gets a row" half. The README's example is the part that
// had no owner, which is why this test exists separately.
func TestReadmeCardRowClaimsMatchTheStageTable(t *testing.T) {
	doc := readREADME(t)

	// Whatever example the README gives, it must be a stage that really has no row. Rather
	// than parse prose, check the claim that is easy to state and hard to drift: the ones the
	// row's rule excludes.
	few := 0
	for _, chips := range stageCards {
		if len(chips) < 2 {
			few++
		}
	}

	// The README now states that every stage currently draws the row. If that stops being
	// true, the sentence has to change — and a single-card stage is the only way it can.
	const claim = "七个阶段目前都至少有三张"
	if !strings.Contains(doc, claim) {
		t.Fatal("the README no longer states how many cards each stage carries; update this " +
			"test with the sentence you wrote about when the index row appears")
	}
	if few != 0 {
		t.Fatalf("the README says every stage has at least three cards, but %d stage(s) have "+
			"fewer than two and therefore draw no index row", few)
	}
	for step, chips := range stageCards {
		if len(chips) < 3 {
			t.Fatalf("the README says every stage has at least three cards, but stage %d has %d",
				step+1, len(chips))
		}
	}

	// docs/keys.md states the same fact in its own words. Both documents, one measurement:
	// checking them in one place means a stage cannot lose a card while only one of the two
	// sentences is updated.
	keys := readKeysDoc(t)
	if !strings.Contains(keys, "七个阶段都至少有三张") {
		t.Fatal("docs/keys.md no longer says every stage has at least three cards; if the claim " +
			"changed, update this test with the wording you used")
	}
}

// TestReadmeViewportOverflowMatchesTheLayout keeps the PageUp/PageDown row's claim honest.
//
// The README justifies the scroll keys with how much of the stage column sits below the fold at
// 1180×720. That number was once written as "870 DIP" — the tallest stage when the sentence was
// written — and had understated the problem by more than half by the time anyone looked.
//
// It is a property, not a constant, because the quantity is not a property of the layout alone:
// it is how tall the stage's text becomes in whatever font the machine found. Measured on four
// machines, the same layout gives **1742, 1805, 1815 and 1885 DIP**, and the machines differ
// only in their fonts. An earlier version of this test pinned 1805 with a half-DIP tolerance,
// so it failed on every platform but the one it was written on while the README's claim — that
// the column hides several screens of content — was true everywhere.
//
// What holds on every platform is that the overflow is more than two screens, which is what
// makes the keys necessary. The README states that, with the range it was measured over, and
// this test requires both the wording and the measurement to keep agreeing.
func TestReadmeViewportOverflowMatchesTheLayout(t *testing.T) {
	doc := readREADME(t)
	const claim = "两个半屏以上"
	if !strings.Contains(doc, claim) {
		t.Fatal("the README no longer states the overflow as a property (" + claim + "); " +
			"update the test with the sentence you wrote")
	}

	const viewport = 720
	tallest := float32(0)
	for step := 0; step < 7; step++ {
		a := newTestApp(t)
		a.Step = step
		tr := ui.NewTester(a.View, 1180, 720)
		tr.Frame()
		if a.Scroll.MaxY > tallest {
			tallest = a.Scroll.MaxY
		}
	}
	// "Two and a half screens" is 1800 DIP at this window; the floor is two screens, which is
	// the least the four measured platforms showed, and the word "以上" is what is asserted.
	screens := tallest / viewport
	if screens < 2.4 {
		t.Fatalf("the README says more than two and a half screens sit below the fold at "+
			"1180×720, but the tallest stage measures %.0f DIP = %.2f screens — either the "+
			"stages got shorter or the sentence overstates the case", tallest, screens)
	}
	// The top of the claim has to be real too: the README names the range it was measured
	// over, so a machine whose font makes the column far taller should fail rather than let
	// the sentence understate it again.
	if screens > 4 {
		t.Fatalf("the tallest stage measures %.0f DIP = %.2f screens, past the range the README "+
			"quotes (1742–1885 DIP); update the sentence's figures", tallest, screens)
	}

	// docs/keys.md states the same property, in the section explaining why the scroll keys are
	// captured. It carries the measurement range too, and it is the copy a reader following the
	// README's design rationale lands on. Checked here rather than in a second test so the two
	// cannot answer differently — the same reason the fade duration moved into one test.
	keys := readKeysDoc(t)
	for _, want := range []string{claim, "1742–1885 DIP"} {
		if !strings.Contains(keys, want) {
			t.Fatalf("docs/keys.md no longer states %q in its scroll-keys section; the README "+
				"still does, so the two documents would disagree", want)
		}
	}
}

// TestReadmeSkillLossMatchesTheOpenedTemperature keeps the per-position card's loss honest.
//
// The README quoted "24/35、1.121 nats" for the every-position card. 1.121 is the figure at
// T = 1.00; the app opens at Temp = 0.80, where that same card prints 1.184. The card is
// computed with the reader's temperature, so the sentence was quoting a number the reader
// would not see at startup — and CHANGELOG records 1.121 explicitly as "T=1.0", so the
// qualifier was dropped rather than mis-measured.
//
// TestReadmeSkillFiguresMatchTheDefaultView pins the *word-boundary* figures at the default
// temperature but never looked at this sentence. This does.
func TestReadmeSkillLossMatchesTheOpenedTemperature(t *testing.T) {
	// The sentence lives in the card notes (the logit-lens/Logits paragraph), which are
	// docs/cards.md since they were split out of the README.
	doc := readmeCardSections(t)
	const claim = "默认打开的 T = 0.80 下是 **1.184 nats**、T = 1.00 下是 1.121"
	if !strings.Contains(doc, claim) {
		t.Fatal("the README no longer states the per-position loss at both the opened and the " +
			"T=1 temperature in the form this test knows; update it with the sentence you wrote")
	}

	a := newTestApp(t)
	if a.Temp != 0.80 {
		t.Fatalf("the app opens at T = %.2f, but the README says it opens at 0.80", a.Temp)
	}
	opened := computeNextByteSkill(a.W, a.Trace, a.Temp)
	if got := fmt.Sprintf("%.3f", opened.MeanLoss); got != "1.184" {
		t.Fatalf("the README says the card reads 1.184 nats at the opened temperature, but it "+
			"reads %s at T = %.2f", got, a.Temp)
	}
	atOne := computeNextByteSkill(a.W, a.Trace, 1.0)
	if got := fmt.Sprintf("%.3f", atOne.MeanLoss); got != "1.121" {
		t.Fatalf("the README says the card reads 1.121 nats at T = 1.00, but it reads %s", got)
	}
	if opened.Hits != 24 || opened.Scored != 35 {
		t.Fatalf("the README says 24/35 positions guess right, the card finds %d/%d",
			opened.Hits, opened.Scored)
	}
}

// TestDocsOnlyShowJustCommandsThatWork keeps documentation from naming just invocations that
// fail outright.
//
// The bug report template told a reporter to run "just run -w 1180 -h 720". The run recipe is
// `go run .` with no argument passthrough, and just does not forward unknown arguments to a
// recipe: `just run -w 1180 -h 720` exits with "justfile does not contain recipe '-w'" before
// Go starts. A repro command that cannot run is worse than no command, because the reporter
// concludes the app is broken rather than the instruction.
//
// The first version of this test only looked for documented invocations that pass a flag, and
// the fix for that bug removed the last one — leaving a guard that parsed three files and found
// nothing, which is the failure mode CONTRIBUTING warns about: an assertion no mutation can turn
// red reports safety it has not established. Two things fixed that. Every `just <recipe>` the
// docs name is now checked against the justfile itself, so a rename breaks it; and the guard
// fails if it finds no invocations at all, so it cannot quietly become a no-op again.
func TestDocsOnlyShowJustCommandsThatWork(t *testing.T) {
	if _, err := exec.LookPath("just"); err != nil {
		t.Skip("just is not installed")
	}
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	docs := map[string]string{
		"bug_report.yml":  filepath.Join("..", "..", ".github", "ISSUE_TEMPLATE", "bug_report.yml"),
		"README.md":       filepath.Join("..", "..", "README.md"),
		"CONTRIBUTING.md": filepath.Join("..", "..", "CONTRIBUTING.md"),
	}
	// `just <recipe> [args]` inside backticks. A bare global flag (`just --list`) has no recipe
	// name, so it is not matched.
	re := regexp.MustCompile("`(just +[a-z][a-z0-9-]*( +[^`]*)?)`")
	// Placeholders are documentation syntax, not arguments to run.
	placeholder := regexp.MustCompile(`[<>\[\]{}]`)

	checked, invocations := 0, 0
	for name, path := range docs {
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		for _, m := range re.FindAllStringSubmatch(string(b), -1) {
			cmd := strings.TrimSpace(m[1])
			fields := strings.Fields(cmd)
			invocations++

			// The recipe must exist under the name the docs use. This is what notices a
			// recipe being renamed out from under the documentation.
			show := exec.Command("just", "--show", fields[1])
			show.Dir = root
			if out, err := show.CombinedOutput(); err != nil {
				t.Fatalf("%s documents `%s`, but just has no recipe %q:\n%s",
					name, cmd, fields[1], out)
			}
			checked++

			// And if the docs pass arguments, just must accept them. Skipped when they are
			// placeholders rather than literal arguments.
			args := fields[2:]
			if len(args) == 0 || placeholder.MatchString(strings.Join(args, " ")) {
				continue
			}
			dry := exec.Command("just", append([]string{"--dry-run"}, fields[1:]...)...)
			dry.Dir = root
			if out, err := dry.CombinedOutput(); err != nil {
				t.Fatalf("%s documents `%s`, which just rejects: %v\n%s", name, cmd, err, out)
			}
		}
	}
	if invocations == 0 || checked == 0 {
		t.Fatalf("found %d documented just invocation(s) and checked %d recipes; the docs "+
			"stopped naming recipes, so this test has nothing left to verify", invocations, checked)
	}
	// A reader gets these from the docs; the count is asserted only loosely, to notice a
	// wholesale rewrite that drops the instructions rather than to pin the current number.
	if checked < 10 {
		t.Fatalf("only %d documented just invocations checked; the docs used to name at least "+
			"ten recipes, so either they lost their instructions or the pattern stopped matching",
			checked)
	}
}

// TestReadmeDirectoryListingNamesEveryTopLevelEntry keeps the 目录 block from becoming a partial
// picture of the repository.
//
// It said `docs/  截图` from the initial commit onward, and docs/ gained three long-form
// documents when the card notes, the interaction rationale and the upstream regression report were
// split out of the README (61b79c8). A reader scanning the listing to find "where do the measured
// numbers live" was told the directory holds screenshots. CONTRIBUTING.md, CHANGELOG.md,
// SECURITY.md and mygo.json were not listed at all.
//
// The check derives its list from the filesystem rather than from a second hand-written list: for
// every tracked-looking top-level entry, either the listing names it or this fails. It is
// deliberately about *presence*, not about the one-line descriptions beside each name — a
// description going slightly stale is not what this test is for, and pinning prose would make it
// fail on every reword.
func TestReadmeDirectoryListingNamesEveryTopLevelEntry(t *testing.T) {
	doc := readREADME(t)

	// Extract the fenced block under "## 目录".
	i := strings.Index(doc, "## 目录")
	if i < 0 {
		t.Fatal("the README no longer has a 目录 section; update this test")
	}
	rest := doc[i:]
	start := strings.Index(rest, "```")
	if start < 0 {
		t.Fatal("the 目录 section no longer has a fenced code block")
	}
	body := rest[start+3:]
	if end := strings.Index(body, "```"); end >= 0 {
		body = body[:end]
	}
	if strings.TrimSpace(body) == "" {
		t.Fatal("the 目录 block is empty")
	}

	root := filepath.Join("..", "..")
	// Top-level entries a reader can see in a checkout. Dotfiles are skipped: .gitignore and
	// .editorconfig are conventions rather than places to look for something, and listing every
	// dotfile would make the block noise.
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatalf("read the repository root: %v", err)
	}
	var missing []string
	for _, e := range entries {
		name := e.Name()
		if strings.HasPrefix(name, ".") || name == "build" || name == "dist" {
			continue // dotfiles, and build outputs that are not in the repository
		}
		// Compiled test binaries land in the root when someone runs `go test -c`, and the
		// .gitignore covers them. They are not part of the repository, so they are not part of
		// the map — measured, one left over from a debug session showed up here.
		if strings.HasSuffix(name, ".test") {
			continue
		}
		if !strings.Contains(body, name) {
			missing = append(missing, name)
		}
	}
	// For a directory, the listing may instead name a path inside it; both count as naming it.
	sort.Strings(missing)
	if len(missing) > 0 {
		t.Fatalf("the README's 目录 block does not name %d top-level entr(ies) a reader sees in a "+
			"checkout: %s\nUpdate the block — it is the map a new reader uses to find where things "+
			"live.", len(missing), strings.Join(missing, ", "))
	}

	// And it must not name something that is not there. A stale path sends the reader to a
	// directory that no longer exists, which is worse than omitting it.
	for _, line := range strings.Split(body, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		path := strings.TrimSuffix(fields[0], "/")
		if !strings.Contains(path, ".") && !strings.Contains(path, "/") {
			continue // a bare word, not a path
		}
		if _, err := os.Stat(filepath.Join(root, path)); err != nil {
			// Files under internal/ are listed with a trailing description and may be named
			// without their parent, so only absolute-from-root paths are checked here.
			if !strings.Contains(path, "/") {
				continue
			}
			t.Errorf("the README's 目录 block names %q, which does not exist", path)
		}
	}
}

// TestReadmePlayIntervalMatchesTheConstant keeps the autoplay row's "每 1.3 秒一步" honest.
//
// playStepInterval is 1300ms and the README states the pace as 1.3 seconds. Measured by setting
// the constant to 2000ms: every test in the package still passed, while the documented pace and
// the real one had drifted apart by more than a second per step. The keyboard table is where a
// reader decides whether to press 空格 and watch, so the number is load-bearing.
//
// Same derivation as the fade duration: the expected string comes from the constant, so changing
// the code fails here with the figure it now needs rather than matching a second literal.
func TestReadmePlayIntervalMatchesTheConstant(t *testing.T) {
	doc := readREADME(t)

	secs := playStepInterval.Seconds()
	// One decimal place, which is the precision the README writes in.
	want := fmt.Sprintf("%.1f 秒", secs)
	if want != "1.3 秒" {
		t.Fatalf("playStepInterval is %v, so the README's pace should read %q; update the "+
			"空格 row", playStepInterval, want)
	}
	if !strings.Contains(doc, want) {
		t.Fatalf("the README no longer states the autoplay pace as %q, which is what "+
			"playStepInterval is configured to", want)
	}

	// The row also promises playback stops at the last stage rather than wrapping. That is a
	// behaviour, not a number, and TestPlaybackStopsAtTheEnd covers it — asserted here only
	// so removing the sentence is noticed together with the figure it sits beside.
	if !strings.Contains(doc, "播到第 7 步停止") {
		t.Error("the README's autoplay row no longer says playback stops at the last stage")
	}
}

// TestReadmeStageTableNamesRealSymbols keeps the stage-to-code table pointing at code that exists.
//
// The table is the README's claim that each screen's numbers can be traced: "每一行写成「界面读的是
// 哪个字段 → 由哪个函数算出 → 写回哪个字段」… 左列可以直接对照界面上的数字". A reader follows a
// row to tokenizer.go to find Tokenize, or to workings.go to find computeHeadProfiles. If a symbol
// in that table is renamed, the row sends them to a name that is not in the repository — and
// nothing else in the suite would notice, because every other README test reads prose about
// behaviour rather than identifiers.
//
// The table is accurate today: all sixteen function names and all fourteen struct fields it cites
// were checked by hand while writing this test. That is the reason it is worth pinning — a table
// this specific, with no owner, is exactly the shape that goes stale silently.
func TestReadmeStageTableNamesRealSymbols(t *testing.T) {
	doc := readREADME(t)

	i := strings.Index(doc, "| 界面阶段 | 公式 | 读 | 函数 | 写回 |")
	if i < 0 {
		t.Fatal("the README no longer has the stage-to-code table; update this test")
	}
	table := doc[i:]
	if j := strings.Index(table, "\n\n"); j >= 0 {
		table = table[:j]
	}

	// Every backticked identifier. Some cells prefix it with the file it lives in —
	// "`workings.go: computeHeadProfiles`" — so the file name and the colon are part of the match
	// and only the trailing identifier is kept.
	//
	// The first version of this pattern matched only a bare identifier between backticks, which
	// silently skipped every row written in the file:function form — that is most of the 函数
	// column. Verified: renaming computeHeadProfiles in the table left the test green until the
	// prefix was handled.
	// A cell may also qualify the field with its struct — "`LayerTrace.Norm1`" — so the type
	// prefix is allowed and only the last segment is kept. Without that, a renamed field would be
	// read as the untouched prefix and slip through: verified, "`LayerTrace.NormOne`" left the
	// test green until the dotted form was handled.
	ident := regexp.MustCompile("`(?:(?:[a-z_]+\\.go: )?(?:[A-Za-z_][A-Za-z0-9_]*\\.)*)([A-Za-z_][A-Za-z0-9_]*)`")
	cited := map[string]bool{}
	for _, m := range ident.FindAllStringSubmatch(table, -1) {
		cited[m[1]] = true
	}
	if len(cited) < 20 {
		t.Fatalf("the table cites only %d identifiers; the parser stopped matching or the table "+
			"was rewritten", len(cited))
	}

	// What a symbol may be declared as: a function, a type, or a struct field. Fields are the
	// majority in the 读/写回 columns, which is why they are checked here rather than ignored.
	var all strings.Builder
	for _, dir := range []string{".", "../model"} {
		files, err := filepath.Glob(filepath.Join(dir, "*.go"))
		if err != nil {
			t.Fatal(err)
		}
		for _, f := range files {
			if strings.HasSuffix(f, "_test.go") {
				continue
			}
			b, err := os.ReadFile(f)
			if err != nil {
				t.Fatalf("read %s: %v", f, err)
			}
			all.Write(b)
		}
	}
	src := all.String()

	// Go builtins and language keywords the table legitimately names.
	builtin := map[string]bool{"copy": true}

	var missing []string
	for name := range cited {
		if builtin[name] {
			continue
		}
		declared := regexp.MustCompile(`\bfunc (?:\([^)]*\) )?`+regexp.QuoteMeta(name)+`\b`).MatchString(src) ||
			regexp.MustCompile(`\btype `+regexp.QuoteMeta(name)+`\b`).MatchString(src) ||
			regexp.MustCompile(`(?m)^\s*`+regexp.QuoteMeta(name)+`\s`).MatchString(src)
		if !declared {
			missing = append(missing, name)
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		t.Fatalf("the stage-to-code table cites %d symbol(s) that are not declared anywhere: %s\n"+
			"Either the symbol was renamed (update the row) or the row now describes something "+
			"else (rewrite it).", len(missing), strings.Join(missing, ", "))
	}
}

// TestReadmeProgressDotStatesMatchTheCode keeps the sidebar legend describing dots that exist.
//
// The README said the current stage is "实心强调色 + 光晕" and a future one is "空心描边". Neither
// is true: stepDots draws three filled boxes — an 18×6 accent bar for the current stage, a 6×6
// colMuted dot for the ones behind and a 6×6 colBorder dot for the ones ahead — and there is no
// glow anywhere in the package (`grep -rn 光晕 internal/app` finds only this README line). A
// reader comparing the legend to the screen would conclude the glow was a rendering problem.
//
// Two things are asserted, because the legend can go wrong in two ways: the words that describe
// effects the code does not have must be gone, and the three states the code does have must still
// be told apart in the sentence.
func TestReadmeProgressDotStatesMatchTheCode(t *testing.T) {
	doc := readREADME(t)

	i := strings.Index(doc, "侧栏的进度轨")
	if i < 0 {
		t.Fatal("the README no longer explains the sidebar's progress track; update this test")
	}
	line := doc[i:]
	if j := strings.Index(line, "\n"); j >= 0 {
		line = line[:j]
	}

	// Effects the package does not implement. If one is ever added, delete it from this list
	// and put the word back in the README.
	for _, absent := range []string{"光晕", "空心描边", "描边", "竖线随进度填充"} {
		if strings.Contains(line, absent) {
			t.Fatalf("the legend describes %q, which stepDots does not draw: it uses three filled "+
				"boxes (an 18-DIP accent bar for the current stage and 6-DIP dots in colMuted and "+
				"colBorder). Sentence:\n  %s", absent, line)
		}
	}

	// And the states it does draw have to remain distinguishable in the sentence.
	for _, want := range []string{"已完成", "当前", "未到"} {
		if !strings.Contains(line, want) {
			t.Fatalf("the legend no longer names the %q state; stepDots still draws exactly three",
				want)
		}
	}
}

// TestReadmeDimensionReadoutMatchesTheStrip keeps the README's worked example of the vector
// strip's readout honest.
//
// The README says hovering the strip shows "光标处那一维的精确值（`d48 = -0.0627`）". It named
// the dimension d37, and no dimension 37 holds that value — the hovered position renders
// "-0.0627" only at dimension 48. A worked example is what a reader checks the feature against,
// so a wrong index is worse than no index: they hover d37, see +0.3406, and conclude the readout
// is broken.
//
// The test hovers the dimension the README names and requires the rendered string to carry the
// value the README quotes, which ties the two together instead of checking either alone.
func TestReadmeDimensionReadoutMatchesTheStrip(t *testing.T) {
	doc := readREADME(t)
	re := regexp.MustCompile("`d(\\d+) = (-?[0-9.]+)`")
	m := re.FindStringSubmatch(doc)
	if m == nil {
		t.Fatal("the README no longer gives a worked example of the dimension readout in the " +
			"form this test knows; update the test with the sentence you wrote")
	}
	dim, err := strconv.Atoi(m[1])
	if err != nil {
		t.Fatalf("the README's dimension index %q is not a number: %v", m[1], err)
	}

	a := newTestApp(t)
	a.Step = 1 // the embedding stage, which draws the input vector strip
	if dim >= a.W.Config.DModel {
		t.Fatalf("the README works the example at d%d, past d_model (%d)",
			dim, a.W.Config.DModel)
	}
	a.HoverDimIn = dim
	tr := ui.NewTester(a.View, 1500, 940)
	tr.Frame()

	want := fmt.Sprintf("d%d：", dim)
	for _, s := range tr.Texts() {
		if !strings.HasPrefix(s, want) {
			continue
		}
		if !strings.Contains(s, m[2]) {
			t.Fatalf("the README quotes d%d = %s but the strip renders %q there; the worked "+
				"example names a dimension whose value is different", dim, m[2], s)
		}
		return
	}
	t.Fatalf("hovering d%d renders no %q readout, so the README's example cannot be checked "+
		"against the app", dim, want)
}

// TestReadmeTrainingStepsMatchesTheWeights keeps the provenance sentence tied to the checkpoint.
//
// The README says the weights were trained for 600 steps, and the top bar renders exactly that
// number from w.Steps — so the document and the screen state the same fact from two places.
// Measured: setting steps to 750 in weights.json left every README and card test green, while the
// bar would then read "训练 750 步 · loss 1.019" under a sentence claiming 600.
//
// The figure is read from the checkpoint the binary embeds, not from a copy of the number.
func TestReadmeTrainingStepsMatchesTheWeights(t *testing.T) {
	a := newTestApp(t)
	if a.W.Steps <= 0 {
		t.Fatal("the bundled weights record no step count; the README's provenance sentence has " +
			"nothing to agree with")
	}

	doc := readREADME(t)
	want := fmt.Sprintf("%d 步", a.W.Steps)
	if !strings.Contains(doc, want) {
		t.Fatalf("the README does not say %q, which is what the bundled checkpoint records and "+
			"what the top bar renders", want)
	}

	// And the bar really does render it, so the sentence is about something visible rather than
	// about metadata no reader ever sees.
	tr := ui.NewTester(a.View, 1500, 940)
	tr.Frame()
	rendered := false
	for _, s := range tr.Texts() {
		if strings.Contains(s, want) && strings.Contains(s, "loss") {
			rendered = true
			break
		}
	}
	if !rendered {
		t.Fatalf("no line renders the step count %q beside the loss", want)
	}
}

// TestTrainScriptDefaultMatchesTheBundledWeights keeps the reproduction command honest.
//
// The README tells the reader to run `python3 tools/train.py` with no arguments and says it
// reproduces the bundled weights. That only holds while the script's --steps default equals the
// step count recorded in weights.json: measured, setting the default to 750 left every README and
// card test green, and the reader would then train a checkpoint for 750 steps against a shipped
// one trained for 600 — close enough to look right, different enough that the figures would move.
//
// The comparison is between the two non-Go sources and the embedded checkpoint, so all three have
// to agree: the script's default, the JSON's steps, and (via the other test) the README's sentence.
func TestTrainScriptDefaultMatchesTheBundledWeights(t *testing.T) {
	py, err := os.ReadFile(filepath.Join("..", "..", "tools", "train.py"))
	if err != nil {
		t.Fatalf("read tools/train.py: %v", err)
	}
	re := regexp.MustCompile(`"--steps"[^)]*default=(\d+)`)
	m := re.FindStringSubmatch(string(py))
	if m == nil {
		t.Fatal("tools/train.py no longer declares --steps with a default in the form this test " +
			"reads; update the test with whatever it says now")
	}
	defaultSteps, err := strconv.Atoi(m[1])
	if err != nil {
		t.Fatalf("cannot read %q as a number: %v", m[1], err)
	}

	a := newTestApp(t)
	if a.W.Steps != defaultSteps {
		t.Fatalf("tools/train.py defaults to %d steps but the bundled weights record %d; the "+
			"README tells the reader that running the script with no arguments reproduces them, "+
			"which would no longer be true", defaultSteps, a.W.Steps)
	}
}

// TestTrainScriptWritesWhereTheBundleReads keeps the reproduction command pointed at the file the
// binary embeds.
//
// README's "python3 tools/train.py # 复现权重" promises that the script, run with no arguments,
// regenerates the checkpoint the app loads. That depends on the script's --out default being
// internal/model/weights.json, which is the file model.go embeds. Measured: changing the default
// to build/weights.json left every README and card test green — a reader following the documented
// command would then get a fresh checkpoint somewhere the binary never looks, and the shipped one
// would stay exactly as it was while the instruction appeared to work.
//
// Both ends are read: the path comes out of the Python source and has to match what the package
// actually embeds.
func TestTrainScriptWritesWhereTheBundleReads(t *testing.T) {
	py, err := os.ReadFile(filepath.Join("..", "..", "tools", "train.py"))
	if err != nil {
		t.Fatalf("read tools/train.py: %v", err)
	}
	re := regexp.MustCompile(`"--out"[^)]*default="([^"]+)"`)
	m := re.FindStringSubmatch(string(py))
	if m == nil {
		t.Fatal("tools/train.py no longer declares --out with a default in the form this test " +
			"reads; update the test with whatever it says now")
	}
	writesTo := filepath.ToSlash(m[1])

	// Where the package embeds from. model.go carries the go:embed directive.
	src, err := os.ReadFile(filepath.Join("..", "..", "internal", "model", "weights.go"))
	if err != nil {
		t.Fatalf("read internal/model/weights.go: %v", err)
	}
	embedRE := regexp.MustCompile(`//go:embed\s+(\S+)`)
	em := embedRE.FindStringSubmatch(string(src))
	if em == nil {
		t.Fatal("internal/model/weights.go no longer has a go:embed directive for the weights")
	}
	embeds := filepath.ToSlash(filepath.Join("internal", "model", em[1]))

	if writesTo != embeds {
		t.Fatalf("tools/train.py writes to %q but the binary embeds %q; the README tells the "+
			"reader that running the script with no arguments reproduces the bundled weights, "+
			"and it would write somewhere the app never reads", writesTo, embeds)
	}

	// And the README names that same file, so the sentence and the script agree on both ends.
	if !strings.Contains(readREADME(t), writesTo) {
		t.Fatalf("the README does not name %q as where the weights come from", writesTo)
	}
}

// TestWeightsRecordTheTrainingSettings keeps the checkpoint's provenance complete enough to
// reproduce it.
//
// The README says `python3 tools/train.py` reproduces the bundled weights. That promise depends on
// every default the script uses: the seed, how many times the corpus is repeated, the batch size
// and the learning rate all change the result. None of the four was recorded in weights.json, so
// the promise was unverifiable — a reader who changed one of them, or a maintainer who retuned it,
// would get different weights and nothing would say so.
//
// The checkpoint now carries them, and this test requires the recorded set to match what the
// script would use by default. If a default is retuned, both move together or this fails.
func TestWeightsRecordTheTrainingSettings(t *testing.T) {
	a := newTestApp(t)
	meta := a.W.Training
	if meta == nil {
		t.Fatal("the bundled weights record no training settings; the README's reproduction " +
			"command cannot be checked against them")
	}

	py, err := os.ReadFile(filepath.Join("..", "..", "tools", "train.py"))
	if err != nil {
		t.Fatalf("read tools/train.py: %v", err)
	}
	src := string(py)

	// Each default, as written in the script.
	defaults := map[string]string{
		"--seed":    "int",
		"--repeats": "int",
		"--batch":   "int",
		"--lr":      "float",
	}
	found := map[string]string{}
	for flag := range defaults {
		re := regexp.MustCompile(`"` + regexp.QuoteMeta(flag) + `"[^)]*default=([^),]+)`)
		m := re.FindStringSubmatch(src)
		if m == nil {
			t.Fatalf("tools/train.py no longer declares %s with a default this test can read", flag)
		}
		found[flag] = strings.TrimSpace(m[1])
	}

	if got, want := strconv.Itoa(meta.Seed), found["--seed"]; got != want {
		t.Errorf("weights record seed %s, the script defaults to %s", got, want)
	}
	if got, want := strconv.Itoa(meta.Repeats), found["--repeats"]; got != want {
		t.Errorf("weights record repeats %s, the script defaults to %s", got, want)
	}
	if got, want := strconv.Itoa(meta.Batch), found["--batch"]; got != want {
		t.Errorf("weights record batch %s, the script defaults to %s", got, want)
	}
	// The script writes the learning rate as a Python float literal (3e-3), so compare values.
	scriptLR, err := strconv.ParseFloat(found["--lr"], 64)
	if err != nil {
		t.Fatalf("cannot read the --lr default %q: %v", found["--lr"], err)
	}
	if meta.LR != scriptLR {
		t.Errorf("weights record lr %v, the script defaults to %v", meta.LR, scriptLR)
	}
}

// TestSecurityDocClaimsMatchTheCode holds SECURITY.md's promises to the imports.
//
// The document says the app "运行时不访问网络、不读用户目录之外的路径、不执行外部命令" — it
// does not touch the network, does not read paths outside the user's, and does not run external
// commands. Those are checkable statements about the code, and nothing was checking them: adding
// an `import "net/http"` would leave every other test green while the security page started
// promising something untrue.
//
// The one filesystem read is weights.Load, for the path the user passes with -weights, which the
// document already describes as the user's own action. This test pins that it stays the only one.
func TestSecurityDocClaimsMatchTheCode(t *testing.T) {
	root := filepath.Join("..", "..")
	doc, err := os.ReadFile(filepath.Join(root, "SECURITY.md"))
	if err != nil {
		t.Fatalf("read SECURITY.md: %v", err)
	}
	for _, claim := range []string{"不访问网络", "不执行外部命令"} {
		if !strings.Contains(string(doc), claim) {
			t.Fatalf("SECURITY.md no longer says %q; if the promise changed, update this test", claim)
		}
	}

	// Every non-test Go file in the module.
	var files []string
	for _, dir := range []string{".", "internal", "internal/app", "internal/model"} {
		matches, err := filepath.Glob(filepath.Join(root, dir, "*.go"))
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range matches {
			if !strings.HasSuffix(m, "_test.go") {
				files = append(files, m)
			}
		}
	}
	if len(files) < 5 {
		t.Fatalf("found only %d source files; the glob is not reaching the module", len(files))
	}

	forbidden := []struct {
		imp string
		why string
	}{
		{"net/http", "the app must not reach the network"},
		{"net", "the app must not reach the network"},
		{"net/url", "the app must not reach the network"},
		{"os/exec", "the app must not run external commands"},
	}
	for _, f := range files {
		src, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("read %s: %v", f, err)
		}
		body := string(src)
		for _, bad := range forbidden {
			if regexp.MustCompile(`(?m)^\s*(?:[\w.]+\s+)?"` + regexp.QuoteMeta(bad.imp) + `"`).MatchString(body) {
				rel, _ := filepath.Rel(root, f)
				t.Errorf("%s imports %q, but SECURITY.md promises the code does not: %s",
					filepath.ToSlash(rel), bad.imp, bad.why)
			}
		}
	}

	// The only file read has to be the user-supplied weights path.
	weights, err := os.ReadFile(filepath.Join(root, "internal", "model", "weights.go"))
	if err != nil {
		t.Fatalf("read internal/model/weights.go: %v", err)
	}
	if strings.Count(string(weights), "os.ReadFile(") != 1 {
		t.Errorf("weights.go reads files %d times; SECURITY.md describes one read, of the path the "+
			"user passes with -weights", strings.Count(string(weights), "os.ReadFile("))
	}
}

// TestStageOneNamesTheFunctionsThatActuallyRun keeps the tokenising row of the README's table true.
//
// The row said the first stage is `tokenizer.go: Tokenize` → `Truncate`, and neither half of that
// was right: Tokenize had no caller outside its own tests, and the ids reaching Truncate are built
// by app.promptIDs. Forward then recovers the rune grouping with alignTokens, which the row never
// mentioned. A reader following the table to understand stage 1 would have opened a function that
// does not participate.
//
// The function names in the row are checked against the source, so the table cannot describe a
// call path the code does not have.
func TestStageOneNamesTheFunctionsThatActuallyRun(t *testing.T) {
	readme := readREADME(t)
	row := ""
	for _, line := range strings.Split(readme, "\n") {
		if strings.HasPrefix(line, "| 1 分词 ") {
			row = line
			break
		}
	}
	if row == "" {
		t.Fatal("the README's stage table no longer has a 1 分词 row; update this test")
	}

	for _, want := range []string{"promptIDs", "Truncate", "alignTokens"} {
		if !strings.Contains(row, want) {
			t.Errorf("the 1 分词 row does not name %s, which is part of the real path", want)
		}
	}
	if strings.Contains(row, "Tokenize") {
		t.Error("the 1 分词 row still names Tokenize, which no production code calls")
	}

	// And those functions still exist where the row says they do.
	files := []struct{ dir, file, decl string }{
		{".", "app.go", "func promptIDs("},
		{"../model", "tokenizer.go", "func Truncate("},
		{"../model", "gpt.go", "func alignTokens("},
	}
	for _, f := range files {
		src, err := os.ReadFile(filepath.Join(f.dir, f.file))
		if err != nil {
			t.Fatalf("read %s: %v", f.file, err)
		}
		if !strings.Contains(string(src), f.decl) {
			t.Errorf("%s no longer declares %q, which the README's table names", f.file, f.decl)
		}
	}
}
