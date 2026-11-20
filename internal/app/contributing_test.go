package app

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
)

// TestDocsNameRealTests keeps the documentation from citing tests that no longer exist.
//
// The guide was written after several rounds of fixing "the number does not support the
// sentence" defects and points at the tests that lock each one down; a rename would leave a
// reader hunting for names that are gone.
//
// It scans every prose file, not just CONTRIBUTING. The check used to read CONTRIBUTING alone,
// and docs/keys.md had meanwhile come to cite `TestReadmeCardIndexRowMatchesTheStages` — a name
// that never existed under that spelling in either package. A guard over one of six documents
// reports on one of six documents; the other five were free to rot, and one had.
//
// CHANGELOG.md is deliberately excluded. It is a record of what each round measured and changed,
// so it names tests as they were spelled when the round was written — `TestDataColoursStayOutOfText`
// became `TestDataColoursAreNotUsedOnText` later, and the entry describing the original defect is
// still correct about what happened. Requiring history to match the present would mean rewriting
// the record every time a test is renamed, which is the opposite of what it is for. The live
// documents are the ones that have to be true now.
func TestDocsNameRealTests(t *testing.T) {
	root := filepath.Join("..", "..")
	docs, err := filepath.Glob(filepath.Join(root, "*.md"))
	if err != nil {
		t.Fatal(err)
	}
	more, err := filepath.Glob(filepath.Join(root, "docs", "*.md"))
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range more {
		docs = append(docs, path)
	}
	// Drop the changelog, for the reason in the comment above.
	live := docs[:0]
	for _, path := range docs {
		if filepath.Base(path) == "CHANGELOG.md" {
			continue
		}
		live = append(live, path)
	}
	docs = live
	if len(docs) < 5 {
		t.Fatalf("only %d live prose files found; the globs no longer cover the documentation", len(docs))
	}

	// Every TestXxx a document mentions must exist in this package or in model.
	sources, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	modelSources, _ := filepath.Glob("../model/*.go")
	sources = append(sources, modelSources...)
	var all strings.Builder
	for _, f := range sources {
		b, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		all.Write(b)
	}
	declared := all.String()

	// Every TestXxx the documents name, without guessing at suffixes: the point is to catch
	// renames, and a pattern that only matched certain endings would silently skip most of them.
	//
	// The leading \b is load-bearing. Without it the pattern also matches the "TestApp" inside
	// "newTestApp", which is a helper rather than a test, and the guard reports a dangling name
	// for a call that is entirely fine.
	re := regexp.MustCompile(`\bTest[A-Z][A-Za-z0-9]*`)

	named, citations := map[string]bool{}, 0
	for _, path := range docs {
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		for _, m := range re.FindAllString(string(b), -1) {
			named[m] = true
			citations++
		}
	}
	if citations == 0 {
		t.Fatal("the documentation names no tests; this test is no longer checking anything")
	}
	for name := range named {
		if !strings.Contains(declared, "func "+name+"(") {
			t.Fatalf("%s cites %s, which no longer exists", docCiting(t, docs, name), name)
		}
	}
}

// docCiting returns the file that names test, for the failure message.
func docCiting(t *testing.T, docs []string, name string) string {
	t.Helper()
	for _, path := range docs {
		b, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		if strings.Contains(string(b), name) {
			return filepath.Base(path)
		}
	}
	return "the documentation"
}

// TestContributingCoversTheTeachingChecks checks the guide still carries the
// list of figure-reading mistakes, since that list is the part of the document
// that a test cannot enforce on its own.
func TestContributingCoversTheTeachingChecks(t *testing.T) {
	doc, err := os.ReadFile(filepath.Join("..", "..", "CONTRIBUTING.md"))
	if err != nil {
		t.Fatalf("read CONTRIBUTING.md: %v", err)
	}
	text := string(doc)
	for _, want := range []string{
		"同一组图必须用同一个尺度",
		"颜色深浅要说清上限",
		"图形要标出参照点",
		"数字要支撑它旁边那句话",
		"孤立数字要有量纲参照",
		"切片要说清总体",
		"文档里的数字必须从代码派生",
		"成对的东西要由一个来源决定",
		"状态的生命周期要定义完整",
		"「发生时对不对」和「结束后对不对」是两件事",
		"断言要从**渲染出来的文本**里读数字",
		"破坏性验证要用**非默认样本**",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("the guide no longer states %q", want)
		}
	}
}

// TestContributingDoesNotRestateTheColourRange keeps one copy of a number that has to move.
//
// The score-colour ceiling depends on the prompt, the layer and the head. README states the measured per-layer spans and
// a test pins them to the model; CONTRIBUTING carried a second, independent copy reading "本模型 5.4–8.3". Neither figure
// matched anything the app produces, and because the two live in different files, changing one never flagged the other.
// The guide now points at the README instead of repeating it.
func TestContributingDoesNotRestateTheColourRange(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "..", "CONTRIBUTING.md"))
	if err != nil {
		t.Fatalf("read CONTRIBUTING.md: %v", err)
	}
	doc := string(b)
	if strings.Contains(doc, "5.4–8.3") {
		t.Fatal("CONTRIBUTING restates the colour-ceiling range that README owns; the two will drift apart")
	}
	// It should still describe the rule, just not with its own numbers.
	if !strings.Contains(doc, "色深") {
		t.Fatal("the colour-depth point disappeared from CONTRIBUTING")
	}
	if !strings.Contains(doc, "README") {
		t.Fatal("CONTRIBUTING no longer points at README for the measured range")
	}
}

// TestContributingCountsEveryTooltip keeps the guide's "九处 .Tooltip(...)" honest.
//
// The guide tells a reader that tooltips cannot be observed headless and must be checked by eye, and gives the count so
// they know how much that is. A tenth tooltip with the guide still saying nine would send someone away believing they had
// checked them all.
//
// The count is computed from every non-test file in the package. It used to be computed from a
// hard-coded list of three, which had silently lost theme.go — the workings block's 复制 button
// lives there. The guard counted 8 while the package had 9, the guide had been written to agree
// with the guard, and both were wrong together for as long as the check stayed green. Counting
// "the files I remember" is not counting.
func TestContributingCountsEveryTooltip(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "..", "CONTRIBUTING.md"))
	if err != nil {
		t.Fatalf("read CONTRIBUTING.md: %v", err)
	}
	doc := string(b)
	if !strings.Contains(doc, "tooltip") {
		t.Fatal("the guide no longer mentions tooltips; if they became testable, remove this test")
	}

	// Every non-test file in the package, discovered rather than listed.
	//
	// The list used to be hard-coded to app.go, stages.go and widgets.go, and it had
	// silently lost a file: theme.go builds the workings block's 复制 button and calls
	// .Tooltip on it. The guard therefore counted 8 while the package had 9, and because
	// the guide's count had been written to agree with the guard, both were wrong together
	// and the check stayed green. A count is only a check if the count is of everything.
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		src, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		n += strings.Count(string(src), ".Tooltip(")
	}
	if n == 0 {
		t.Fatal("no tooltips left; the guide's paragraph is stale")
	}

	// The guide spells the count in Chinese numerals up to ten.
	cn := [...]string{"零", "一", "两", "三", "四", "五", "六", "七", "八", "九", "十"}
	if !strings.Contains(doc, cn[n]) || !strings.Contains(doc, cn[n]+"处") {
		t.Fatalf("there are %d tooltips (%s处) but the guide does not say so", n, cn[n])
	}

	// And it has to say that the count is of call sites, not of tooltips on screen. The
	// paragraph used to read "界面上有九处 .Tooltip(...)" and then, one clause later, "这个数会随
	// 状态变化" — which contradicts the number it just gave. Three of the nine are in loops or
	// inside controlGroup, which is itself called three times, so the number of elements that
	// carry a tooltip runs into the dozens and changes with the stage. A reader who took nine
	// as "nine things to check by eye" would check a fraction of them.
	if !strings.Contains(doc, "不等于界面上 tooltip 的个数") {
		t.Fatal("the guide states the call-site count without saying that the number of " +
			"tooltips on screen is different; three call sites are in loops or shared by three " +
			"controls, so 9 call sites do not mean 9 tooltips")
	}
}

// TestContributingFocusTableMatchesTheApp measures the Tab cycle and compares it to the table.
//
// The guide's table is where a reader learns what Tab reaches, and it had rotted into something
// false: it listed six stops where the attention stage has eleven, and every coordinate past the
// third was wrong, because it described an interface from before the layer and head segmented
// controls and the card-index chips existed.
//
// The old version of this test could not notice. It checked that six control names appeared in
// the document, that a heading said 六个, and that the table had seven lines — all true while the
// numbers beside them described a different app — and it only required Tab to reach five stops,
// so a cycle of eleven passed. The check is now against the pixels.
func TestContributingFocusTableMatchesTheApp(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "..", "CONTRIBUTING.md"))
	if err != nil {
		t.Fatalf("read CONTRIBUTING.md: %v", err)
	}
	doc := string(b)

	if !strings.Contains(doc, "1180×720") {
		t.Fatal("the guide no longer states the window the focus table was measured at")
	}

	// Stage 3 (index 2): the attention stage, where layer and head are both live and the stage
	// carries a three-chip card row, so the cycle includes every kind of stop.
	rings := focusRings(t, 2, 1180, 720)
	if len(rings) < 6 {
		t.Fatalf("Tab found only %d focus rings on stage 3; the cycle has collapsed", len(rings))
	}

	// The heading states the cycle's length. A count in a heading is a claim like any other and
	// goes stale the same way — it once read 五个 over a six-row table.
	cn := [...]string{"零", "一", "两", "三", "四", "五", "六", "七", "八", "九", "十",
		"十一", "十二", "十三", "十四", "十五", "十六", "十七", "十八", "十九", "二十"}
	if len(rings) >= len(cn) {
		t.Fatalf("%d focus rings is past what this test can spell", len(rings))
	}
	if !strings.Contains(doc, cn[len(rings)]+" 个 Tab 停点") &&
		!strings.Contains(doc, cn[len(rings)]+"个 Tab 停点") {
		t.Fatalf("the attention stage has %d Tab stops but the guide's heading does not say %q",
			len(rings), cn[len(rings)]+"个 Tab 停点")
	}

	// The table's control names, in order, are the invariant: they are what tells a keyboard
	// reader which control Tab will reach next, and they do not depend on the machine.
	//
	// The boxes do. They used to be checked too, and a box that disagreed was called a stale
	// row — but the boxes are pixels of text, so the same window measures them differently
	// wherever the fonts differ. Measured on the three CI runners, stop 1 came out 390, 394 and
	// 387 DIP wide against the 390 recorded here, and every one of the three failed while the
	// table's order and names were right. CONTRIBUTING still carries the boxes, labelled with
	// the window and the fact that they were measured there, because a reader comparing their
	// own screen wants them; what this test holds is the part that is the same everywhere.
	// Which control each stop lands on. The table names the card chips by position ("卡片索引
	// chip 1"), the app knows them by the card they open ("Q / K / V"), and both describe the
	// same stop, so a chip row is satisfied by either.
	stops := focusStops(t, 2, 1180, 720)
	for i, label := range stops {
		if label == "" {
			t.Fatalf("Tab stop %d lands on no control this test knows; add it to focusStops", i+1)
		}
		// The guide may name a control by what the reader sees rather than by its
		// accessibility label; both name the same stop.
		aliases := map[string]string{
			"输入":     "提示词输入框",
			"◀ 采样":   "采样",
			"层 分段控件": "层 分段控件",
			"头 分段控件": "头 分段控件",
		}
		named := strings.Contains(doc, "| "+label+" |")
		if a, ok := aliases[label]; ok {
			named = named || strings.Contains(doc, "| "+a+" |")
		}
		if !named && strings.HasPrefix(label, "卡片索引 chip") {
			named = strings.Contains(doc, "| "+label+" |")
		}
		if !named {
			// A card chip is listed by its position in the table and known here by its card.
			if i+1 <= len(stops) && i >= 8 {
				if strings.Contains(doc, fmt.Sprintf("| %d | 卡片索引 chip %d |", i+1, i-7)) {
					continue
				}
			}
			t.Fatalf("Tab stop %d lands on %q, which the guide's table does not name; a control "+
				"the reader is not told they can reach is one they will not find", i+1, label)
		}
	}

	// The table's data rows must equal the cycle. This catches a row added to the document for
	// a control that does not exist, which the box check alone cannot see.
	i := strings.Index(doc, "| Tab | 控件 | 焦点环位置 |")
	if i < 0 {
		t.Fatal("the guide no longer has a focus table")
	}
	rest := doc[i:]
	if j := strings.Index(rest, "\n\n"); j >= 0 {
		rest = rest[:j]
	}
	rows := 0
	for _, line := range strings.Split(rest, "\n") {
		if strings.HasPrefix(line, "| ") {
			rows++
		}
	}
	if got := rows - 2; got != len(rings) {
		t.Fatalf("the focus table has %d data rows for a cycle of %d stops", got, len(rings))
	}
}

// focusRings walks the Tab cycle on one stage and returns each stop's focus-ring bounding box.
//
// The ring is found by differencing each frame against the *unfocused* frame: it exists only
// while a control has focus. Diffing against the previous stop instead — the obvious thing to
// write — measures two rings at once and yields a box spanning both, which is how a first draft
// reported boxes wider than any control on screen.
//
// Which widget each ring belongs to is deliberately not attempted. Pressing a key to see what
// changes cannot identify it: Right moves no app field in the text input, walks the value in the
// segmented controls, and does nothing on a button, so several distinct stops all look like
// "nothing happened". The box is the only signal that answers both "is there a ring" and "where".

// focusStops names the control each Tab stop lands on, in cycle order.
//
// It asks the tester which element has the focus after each Tab, which is a question about the
// view's state rather than about pixels, so the answer does not depend on font metrics at all.
// The earlier version of this test matched the recorded ring coordinates against a table in
// CONTRIBUTING.md, and the table's boxes are pixels of text: all three CI runners failed on it
// (stop 1 measured 387, 390 and 394 DIP wide against the 390 written down) while the cycle it
// describes — which control comes next — was right on every one of them.
//
// The candidates are the app's real control texts. Ordering matters because Focused also
// reports true for a container: the prompt field is labelled "输入", a segmented control's
// value ("0", "H0") is inside the focused segment, and a card chip's ring is drawn around the
// card's own title. The longest name whose element reports focus is the one recorded.
func focusStops(t *testing.T, step, w, h int) []string {
	t.Helper()
	a := newTestApp(t)
	a.Step = step
	tr := ui.NewTester(a.View, w, h)
	tr.Frame()

	controls := []string{
		"输入", "重置", "撤销", "采样下一个 token",
		"▶ 自动播放", "⏸ 暂停", "← 上一步", "下一步 →",
		"温度",
		"Q / K / V", "分数矩阵", "掩码反事实", "加权求和",
		"注意力矩阵", "四个头", "头之间重合吗",
		"嵌入向量", "嵌入有含义吗", "位置向量", "位置编码的形状",
		"残差流", "每层说什么", "每层改多少", "LayerNorm", "残差为何不爆",
		"FFN 内部", "FFN 写出了答案", "哪个单元在干活",
		"最后位置", "每位置预测", "绑定权重", "Top-12", "采样",
	}
	// The segmented controls report through their values, which are not unique across the
	// window ("1" is both a layer and a head), so they are named by the label the reader sees
	// beside them and resolved together with that label's row.
	segments := []struct{ name, value string }{
		{"层 分段控件", "0"}, {"头 分段控件", "H0"},
	}

	rings := focusRings(t, step, w, h)
	out := make([]string, 0, len(rings))
	for n := range rings {
		// Replay the cycle up to stop n, then ask what has the focus: Focused answers about
		// the current frame, and the frame depends on how many Tabs came before it.
		a := newTestApp(t)
		a.Step = step
		tr := ui.NewTester(a.View, w, h)
		tr.Frame()
		for i := 0; i <= n; i++ {
			tr.Key(0, ui.KeyTab)
			tr.Frame()
		}
		best := ""
		for _, label := range controls {
			if tr.Focused(label) && len(label) > len(best) {
				best = label
			}
		}
		if best == "" {
			for _, seg := range segments {
				if tr.Focused(seg.value) {
					best = seg.name
					break
				}
			}
		}
		out = append(out, best)
	}
	return out
}

func focusRings(t *testing.T, step, w, h int) [][4]float32 {
	t.Helper()
	a := newTestApp(t)
	a.Step = step
	tr := ui.NewTester(a.View, w, h)
	tr.Frame()
	base := append([]byte(nil), tr.Image().Pix...)
	var rings [][4]float32
	for n := 0; n < 24; n++ {
		before := a.Scroll.Y
		tr.Key(0, ui.KeyTab)
		tr.Frame()
		// Tab can scroll the stage column once the focus leaves the chrome: the ring
		// itself stays small, but every frame after that differs from the base by a
		// whole-screen shift, so the "bounding box" runs to the bottom of the window and
		// looks like a new stop. Measured on stage 7 — Scroll.Y 0 to 1065 at the press
		// right after the last chip, which invented nine stops that do not exist.
		if a.Scroll.Y != before {
			return rings
		}
		img := tr.Image()
		minX, minY, maxX, maxY := 1<<30, 1<<30, -1, -1
		for y := 0; y < h; y++ {
			row := y * img.Stride
			for x := 0; x < w; x++ {
				o := row + x*4
				if o+2 >= len(img.Pix) || o+2 >= len(base) {
					continue
				}
				if img.Pix[o] == base[o] && img.Pix[o+1] == base[o+1] && img.Pix[o+2] == base[o+2] {
					continue
				}
				if x < minX {
					minX = x
				}
				if x > maxX {
					maxX = x
				}
				if y < minY {
					minY = y
				}
				if y > maxY {
					maxY = y
				}
			}
		}
		if maxX < 0 {
			continue
		}
		ring := [4]float32{float32(minX), float32(minY), float32(maxX), float32(maxY)}
		for _, prev := range rings {
			if prev == ring {
				return rings
			}
		}
		rings = append(rings, ring)
	}
	t.Fatal("the Tab cycle did not repeat within 24 presses, so it has no length to state")
	return rings
}

// TestContributingTestCountStaysAboveItsStatedBound keeps the guide's "五百多个测试函数"
// from becoming false in the direction that matters.
//
// The guide states this count so a contributor can judge what running the package costs.
// It used to state it exactly — "489 个测试函数" — and that was wrong within a couple of
// commits, because nothing re-derived it: the package had 510 by the time anyone looked.
// An exact count in prose is a number with no owner. A lower bound can be checked, and,
// being a bound, it does not need editing every time a test is added.
//
// Only the lower bound is enforced. Growth is the normal case and needs no action; a count
// that falls below what the guide promises means either tests were deleted or the sentence
// is now an overstatement, and both are worth a failure.
func TestContributingTestCountStaysAboveItsStatedBound(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "..", "CONTRIBUTING.md"))
	if err != nil {
		t.Fatalf("read CONTRIBUTING.md: %v", err)
	}
	doc := string(b)

	// "五百多" promises strictly more than 500.
	const bound = 500
	if !strings.Contains(doc, "五百多") {
		t.Fatal("CONTRIBUTING no longer states the order of magnitude of the app's test count; " +
			"update this test with the wording you used")
	}
	// And no copy of the number may state a different bound. The guide mentions it twice —
	// the timing paragraph and the note explaining why it is a bound — and checking only
	// that *a* "五百多" exists passes while the other copy says something else. Verified by
	// mutation: changing one of the two left this test green until every occurrence was
	// required to agree.
	if got := strings.Count(doc, "五百多"); got != 2 {
		t.Fatalf("the guide states the test-count bound %d time(s); it should state it twice, "+
			"in the timing paragraph and in the note about why it is a bound", got)
	}

	files, err := filepath.Glob("*_test.go")
	if err != nil {
		t.Fatal(err)
	}
	decl := regexp.MustCompile(`(?m)^func Test`)
	n := 0
	for _, f := range files {
		src, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		n += len(decl.FindAllIndex(src, -1))
	}
	if n == 0 {
		t.Fatal("found no test functions; this guard is no longer checking anything")
	}
	if n <= bound {
		t.Fatalf("the guide says 五百多个测试函数, but this package has %d; either tests were "+
			"removed or the sentence needs correcting", n)
	}
}

// TestContributingPageLengthMatchesTheLayout keeps the guide's page-length claim honest.
//
// The guide justifies scrolling with how long a stage is, and it stated the length as a per-stage
// table: "第 5 步已经要滚 4.4 屏、第 4 步 3.3 屏、第 3 步 2.9 屏". Those were measured when they
// were written — re-measured at the commit that added them, f0cbee9, the layout gave 4.39 / 3.34 /
// 2.93 screens — and were still true then. At HEAD the same measurement gives 4.55 / 4.21 / 3.03:
// the residual stage grew by a screen, so the sentence now understates the tallest stage and the
// reader deciding whether to run the full gate is told the wrong amount.
//
// Three per-stage figures in prose are three numbers with no owner, and this repository has had
// exactly that defect before (the "870 DIP" overflow claim, the "第 6 步只有一张卡片" example).
// The sentence therefore states a property instead — the tallest stage is past four screens — and
// this guard re-derives it. A stage that shrinks below four screens has to change the sentence;
// a stage that grows does not, which is the direction that actually happens.
//
// The screen is the stage column's own scroll viewport, not the window: the column is shorter than
// the window by the header, the token rail and the step bar, and measuring against the window
// reports about three quarters of the true figure for the same frames — which would let the
// sentence overstate the case while this guard stayed green. The viewport's exact height is a
// pixel measurement that moves with the font, so the guard asserts the ratio rather than a number.
func TestContributingPageLengthMatchesTheLayout(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "..", "CONTRIBUTING.md"))
	if err != nil {
		t.Fatalf("read CONTRIBUTING.md: %v", err)
	}
	doc := string(b)

	const claim = "四屏以上"
	if !strings.Contains(doc, claim) {
		t.Fatal("the guide no longer states the page length as a property (" + claim + "); " +
			"update this test with the sentence you wrote")
	}
	// The unit is part of the claim. The same content is more screens of the column than of the
	// window, so a reader who assumed the window would undershoot every figure in the paragraph.
	if !strings.Contains(doc, "阶段栏自己的滚动视口") {
		t.Fatal("the guide states the length in screens without saying which viewport a screen " +
			"is; the stage column is shorter than the window, and using the window undercounts " +
			"the same content by about a third")
	}

	tallest, tallestStep, viewport := float64(0), 0, float64(0)
	for step := 0; step < 7; step++ {
		a := newTestApp(t)
		a.Step = step
		tr := ui.NewTester(a.View, 1180, 720)
		tr.Frame()
		if a.StageViewportH <= 0 {
			t.Fatalf("stage %d reports a viewport of %.0f DIP; nothing was measured",
				step+1, a.StageViewportH)
		}
		screens := float64(a.Scroll.MaxY+a.StageViewportH) / float64(a.StageViewportH)
		if screens > tallest {
			tallest, tallestStep, viewport = screens, step+1, float64(a.StageViewportH)
		}
	}
	// The unit has to be the column, not the window. Both halves are checked because the
	// difference is what the sentence's "not the whole window" clause claims: the column is
	// shorter, so the same content is more screens than it looks like from the window.
	if viewport >= 720 {
		t.Fatalf("the stage column measures %.0f DIP at 1180×720, which is not shorter than the "+
			"window; the guide's distinction between the two no longer holds", viewport)
	}
	// "About a third" is the sentence's wording: the window-based figure has to undercount
	// enough to matter, or the parenthetical is noise a reader will skip.
	if got := float64(viewport) / 720; got < 0.5 || got > 0.85 {
		t.Fatalf("the stage column is %.0f%% of the window height; the guide says measuring "+
			"against the window undercounts by about a third, so update that clause",
			100*got)
	}
	if tallest < 4 {
		t.Fatalf("the guide says the tallest stage scrolls more than four screens, but stage %d "+
			"measures %.2f screens (%.0f DIP of content in a %.0f DIP viewport) — either the "+
			"stages got shorter or the sentence overstates the case",
			tallestStep, tallest, tallest*viewport, viewport)
	}
	// The top of the claim has to be real too. A stage that doubles in length makes "four
	// screens" as misleading in the other direction, and this repository's stages grow.
	if tallest > 8 {
		t.Fatalf("the tallest stage (stage %d) measures %.2f screens, far past the four the "+
			"guide states; update the sentence", tallestStep, tallest)
	}
}

// TestContributingCodeMapNamesEverySourceFile keeps the guide's code map complete.
//
// The map is where someone who has just been handed this repository looks first — "which file
// does what" — and it had drifted: internal/app/ was one row naming app.go, stages.go, widgets.go
// and compact.go, so theme.go, window.go and workings.go were invisible. workings.go is the
// largest file in the package and holds the arithmetic behind every worked derivation; a reader
// following the map would not know it existed, and the natural first guess for "where is this
// number computed" is stages.go, which only draws it.
//
// internal/model/ was a single row that did not mention the three matmul kernels at all, which
// matter because which one runs depends on GOEXPERIMENT=simd.
//
// The check derives its list from the directory rather than from a second hand-written one, and
// skips _test.go files deliberately: those are named after what they cover, and this file
// already states that.
func TestContributingCodeMapNamesEverySourceFile(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "..", "CONTRIBUTING.md"))
	if err != nil {
		t.Fatalf("read CONTRIBUTING.md: %v", err)
	}
	doc := string(b)

	i := strings.Index(doc, "## 代码地图")
	if i < 0 {
		t.Fatal("CONTRIBUTING no longer has a 代码地图 section; update this test with the heading " +
			"you used")
	}
	rest := doc[i:]
	if j := strings.Index(rest, "\n## "); j >= 0 {
		rest = rest[:j]
	}
	if !strings.Contains(rest, "| 路径 | 职责 |") {
		t.Fatal("the 代码地图 section no longer has a path/responsibility table")
	}

	for _, dir := range []string{".", "../model"} {
		entries, err := filepath.Glob(filepath.Join(dir, "*.go"))
		if err != nil {
			t.Fatal(err)
		}
		var missing []string
		for _, path := range entries {
			name := filepath.Base(path)
			if strings.HasSuffix(name, "_test.go") {
				continue // covered by the paragraph below the table, by design
			}
			if !strings.Contains(rest, name) {
				missing = append(missing, filepath.Join(filepath.Base(dir), name))
			}
		}
		sort.Strings(missing)
		if len(missing) > 0 {
			t.Fatalf("the code map does not name %d source file(s): %s\nA reader following the "+
				"map will not find them; add a row for each, or fold it into the row of the file "+
				"it belongs with.", len(missing), strings.Join(missing, ", "))
		}
	}

	// The test-file policy has to stay stated, or the paragraph promising it can vanish and the
	// next reader will wonder why the test files are absent from the table.
	if !strings.Contains(rest, "*_test.go") {
		t.Error("the code map no longer explains why test files are not in the table")
	}
}

// TestContributingCheckCIBlockMatchesTheJustfile keeps the quoted gate in step with the real one.
//
// The section says `just check-ci` "逐条对应 .github/workflows/ci.yml" and then quotes four
// commands. The race line read `go test -race ./internal/model/` — no -timeout — while the recipe
// and the workflow both run `go test -race -timeout 300s ./internal/model/`. The omission is not
// dangerous by itself (Go's default is ten minutes and the package finishes in about four
// seconds), which is exactly why nobody would notice it: a reader copying the block gets a
// different command from the one the repository actually gates on, in the one place that exists to
// tell them what the gate is.
//
// TestCheckCIRecipeMatchesTheWorkflow already holds the recipe and the workflow together, so the
// only gap was this third copy. It is checked by extracting the fenced block under the
// "改动的验证标准" heading and requiring every `go` line in it to appear verbatim in the recipe.
func TestContributingCheckCIBlockMatchesTheJustfile(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "..", "CONTRIBUTING.md"))
	if err != nil {
		t.Fatalf("read CONTRIBUTING.md: %v", err)
	}
	doc := string(b)

	i := strings.Index(doc, "## 改动的验证标准")
	if i < 0 {
		t.Fatal("CONTRIBUTING no longer has a 改动的验证标准 section; update this test")
	}
	section := doc[i:]
	if j := strings.Index(section, "## 测不到的东西"); j >= 0 {
		section = section[:j]
	}
	start := strings.Index(section, "```")
	if start < 0 {
		t.Fatal("the 改动的验证标准 section no longer quotes a command block")
	}
	body := section[start+3:]
	if end := strings.Index(body, "```"); end >= 0 {
		body = body[:end]
	}

	recipe, err := os.ReadFile(filepath.Join("..", "..", "justfile"))
	if err != nil {
		t.Fatalf("read justfile: %v", err)
	}
	jf := string(recipe)

	quoted := 0
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// The `just check-ci` line names the recipe itself rather than a command it runs.
		if !strings.HasPrefix(line, "go ") && !strings.HasPrefix(line, "GOEXPERIMENT=") {
			continue
		}
		// Strip the trailing comment so the comparison is about the command.
		if k := strings.Index(line, " #"); k >= 0 {
			line = strings.TrimSpace(line[:k])
		}
		quoted++
		// A quoted line may join two recipe lines with && — the tidy check is two commands in
		// the recipe (`go mod tidy`, then `git diff --exit-code`) and the guide writes them as
		// one so a reader can paste it. Every part has to be in the recipe; the joining is
		// presentation.
		for _, part := range strings.Split(line, " && ") {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			if !strings.Contains(jf, part) {
				t.Errorf("the guide quotes %q, but the justfile does not contain that command; "+
					"the block claims to mirror the recipe line for line, so a reader copying it "+
					"runs something the repository does not gate on", part)
			}
		}
	}
	if quoted < 3 {
		t.Fatalf("found %d quoted commands in the verification block; it used to list the three "+
			"steps beyond `just check`, so either the block was rewritten or this parser stopped "+
			"matching", quoted)
	}
}

// TestContributingPerStageFocusCountsMatchTheApp holds the per-stage stop counts to the app.
//
// The guide used to describe the other stages in one sentence — "第 7 步多一个温度滑块、第 1 步少掉
// 两个分段控件和全部 chip" — and both halves were wrong. Stage 1 has three card chips, not none,
// and stage 7 gives up its two segmented controls while gaining a temperature slider and two
// more chips, so its length happens to equal stage 3's. Measured: 8, 10, 11, 12, 12, 10 and 11.
//
// The counts depend on which controls are live and how many cards the stage carries, both of
// which change when a stage gains a card or a control becomes available there. That is why they
// are in a table with a guard rather than in a sentence.
func TestContributingPerStageFocusCountsMatchTheApp(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "..", "CONTRIBUTING.md"))
	if err != nil {
		t.Fatalf("read CONTRIBUTING.md: %v", err)
	}
	doc := string(b)

	for step := 0; step < len(lessons); step++ {
		want := len(focusRings(t, step, 1180, 720))
		if want < 2 {
			t.Fatalf("stage %d has %d focus stops; the measurement is broken, not the table",
				step+1, want)
		}
		row := fmt.Sprintf("| 第 %d 步 | %d |", step+1, want)
		if !strings.Contains(doc, row) {
			// Find what the guide currently says for that stage, so the failure names both sides.
			var got string
			for _, line := range strings.Split(doc, "\n") {
				if strings.HasPrefix(line, fmt.Sprintf("| 第 %d 步 |", step+1)) {
					got = line
				}
			}
			t.Fatalf("stage %d has %d focus stops but the guide's table says %q (want %q); "+
				"the count changes when a stage gains a card or a control becomes available there",
				step+1, want, got, row)
		}
	}
}
