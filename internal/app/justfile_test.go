package app

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
)

// TestArtifactsRecipeSaysWhenThereIsNothing checks `just artifacts` reports an
// empty build directory instead of exiting silently.
//
// The recipe used to be `du -sh build/*/* 2>/dev/null | sort -k2 || echo "..."`.
// With no build/ the stderr goes to /dev/null, du fails, sort succeeds on empty
// input, and the pipeline exits 0 — so the fallback never ran and the command
// printed nothing. A reader could not tell "nothing built yet" from "the recipe is
// broken".
func TestArtifactsRecipeSaysWhenThereIsNothing(t *testing.T) {
	if _, err := exec.LookPath("just"); err != nil {
		t.Skip("just is not installed")
	}
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}

	out, err := exec.Command("just", "artifacts").Output()
	if err != nil {
		t.Fatalf("just artifacts failed: %v", err)
	}
	got := strings.TrimSpace(string(out))

	_, statErr := os.Stat(filepath.Join(root, "build"))
	if os.IsNotExist(statErr) {
		if got == "" {
			t.Fatal("just artifacts prints nothing when build/ is absent; it should " +
				"say so")
		}
		if !strings.Contains(got, "build") {
			t.Fatalf("just artifacts says %q, which does not mention build/", got)
		}
		return
	}
	// A build/ tree exists in this workspace: either it lists files or it says
	// the directory is empty, but it must say something.
	if got == "" {
		t.Fatal("just artifacts prints nothing")
	}
}

// TestCIConfigIsRunnable keeps the workflow's test commands within reach of the
// timeout they run under.
//
// The race step used to be `go test -race ./...`. Only internal/model has
// goroutines; internal/app is single-threaded UI code, and under the race detector
// one rendered frame costs 24ms instead of 0.8ms. The app package alone needs about
// 11 minutes that way, past Go's default 10-minute test timeout — so the step could
// not pass on a clean checkout. Racing a package with no concurrency bought
// nothing and cost the whole job.
func TestCIConfigIsRunnable(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "..", ".github", "workflows", "ci.yml"))
	if err != nil {
		t.Fatalf("read ci.yml: %v", err)
	}
	ci := string(b)

	// The race step must be scoped to the packages that actually have goroutines,
	// and must state its timeout rather than inheriting Go's default.
	if strings.Contains(ci, "go test -race ./...") {
		t.Fatal("ci.yml still races every package; internal/app has no concurrency " +
			"and takes ~11 minutes under -race")
	}
	if !strings.Contains(ci, "go test -race -timeout") {
		t.Fatal("the race step does not set an explicit -timeout")
	}
	if !strings.Contains(ci, "./internal/model/") {
		t.Fatal("the race step no longer covers internal/model, which is the only " +
			"package with goroutines")
	}

	// Every `go test` step should carry a timeout so a slow machine fails with a
	// clear message instead of hanging into the job timeout.
	for _, line := range strings.Split(ci, "\n") {
		trimmed := strings.TrimSpace(line)
		// Only the command line, not the step's name or comments.
		if !strings.HasPrefix(trimmed, "run:") || !strings.Contains(trimmed, "go test") {
			continue
		}
		if !strings.Contains(trimmed, "-timeout") {
			t.Fatalf("a go test step inherits the default timeout: %q", trimmed)
		}
	}
}

// TestConcurrencyLivesOnlyInModel pins the reason the race scope is what it is.
func TestConcurrencyLivesOnlyInModel(t *testing.T) {
	hasConcurrency := func(pattern string) bool {
		files, err := filepath.Glob(pattern)
		if err != nil {
			return false
		}
		for _, f := range files {
			if strings.HasSuffix(f, "_test.go") {
				continue
			}
			b, err := os.ReadFile(f)
			if err != nil {
				continue
			}
			src := string(b)
			if strings.Contains(src, "go func(") || strings.Contains(src, "sync.WaitGroup") {
				return true
			}
		}
		return false
	}
	if !hasConcurrency("../model/*.go") {
		t.Fatal("internal/model no longer has goroutines; the -race step could be " +
			"dropped or the comment explaining it is stale")
	}
	if hasConcurrency("*.go") {
		t.Fatal("internal/app has goroutines now, so the CI race step should cover it")
	}
}

// TestCheckCIRecipeMatchesTheWorkflow keeps the local full gate in step with CI.
//
// `just check` deliberately stops at gofmt, vet and test, while CI also verifies go.mod is
// tidy, races the concurrent package and re-runs everything under GOEXPERIMENT=simd. That split
// was already documented as a known difference, but the alignment had to be done by hand from a
// list in CONTRIBUTING.md, which is the kind of list that goes stale the first time CI gains a
// step.
//
// This comment used to give `just check` as "about 35 seconds", which was right when the recipe
// was written — that commit recorded race 3.0s, SIMD 34.4s and check-ci 39s warm. MyGo v0.9.1
// then raised a fresh window's first frame from 13.2ms to 50.3ms (see
// docs/mygo-first-frame-regression.md), every tester is a fresh window, and the same gate now
// takes about six minutes. The figure is gone rather than corrected: a duration in a comment has
// no owner, and CONTRIBUTING carries the numbers a reader actually plans with. This test is
// about which commands run, not how long they take.
//
// check-ci runs all of it. This test requires the recipe to exist and to run exactly the
// commands the workflow runs, so adding a CI step without teaching the recipe fails here rather
// than surprising a contributor with a red build.
func TestCheckCIRecipeMatchesTheWorkflow(t *testing.T) {
	read := func(name string) string {
		b, err := os.ReadFile(filepath.Join("..", "..", name))
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		return string(b)
	}
	recipe := read("justfile")
	workflow := read(".github/workflows/ci.yml")

	const target = "check-ci: check\n"
	i := strings.Index(recipe, target)
	if i < 0 {
		t.Fatal("the justfile has no check-ci recipe")
	}
	// The recipe body runs to the next blank line.
	body := recipe[i+len(target):]
	if j := strings.Index(body, "\n\n"); j >= 0 {
		body = body[:j]
	}

	// The SIMD step's timeout is 600s rather than 300s since MyGo v0.10.0: a window's first
	// frame went from 13ms to 51ms, which puts internal/app at about 380s under SIMD on this
	// machine. The command is spelled out here so a change to either the recipe or the
	// workflow has to change this line too — the point of the test is that the two agree.
	for _, want := range []string{
		"go mod tidy",
		"git diff --exit-code -- go.mod go.sum",
		"go test -race -timeout 300s ./internal/model/",
		"GOEXPERIMENT=simd go test -timeout 600s ./...",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("check-ci does not run %q", want)
		}
		if !strings.Contains(workflow, want) {
			t.Errorf("the workflow does not run %q either; this list is stale", want)
		}
	}
}

// TestCIBuildsThePhoneTargets keeps the mobile job in the workflow, and its two platforms.
//
// The app has a layout for phones and a `just mobile` recipe for building them, but the
// recipe is for a developer's machine: CI is what notices when the phone targets stop
// linking, which is how a platform silently rots between the releases that exercise it.
// The job is easy to drop in a workflow edit — it builds nothing the desktop jobs need —
// so this pins its existence and the two platforms, and pins the JDK the Android host
// requires: its Gradle 8.14.3 cannot run on a newer JDK (upstream issue #2064), and an
// image that moves to JDK 21 would fail the job for a reason the workflow should state.
func TestCIBuildsThePhoneTargets(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "..", ".github", "workflows", "ci.yml"))
	if err != nil {
		t.Fatalf("read ci.yml: %v", err)
	}
	ci := string(b)

	if !strings.Contains(ci, "\n  mobile:") {
		t.Fatal("ci.yml has no mobile job; nothing builds the iOS or Android packages, so " +
			"a phone target that stops linking is found by whoever next tries to ship one")
	}
	for _, want := range []struct{ what, needle string }{
		{"the iOS simulator build", "ios/simulator"},
		{"the Android build", "android/arm64"},
		{"JDK 17 for the Android host", "openjdk-17-jdk-headless"},
		{"the NDK version", "ndk;29.0.14206865"},
	} {
		if !strings.Contains(ci, want.needle) {
			t.Errorf("ci.yml does not name %s (%q)", want.what, want.needle)
		}
	}
}

// TestWorkflowExpressionsUseOnlyRealFunctions catches a workflow GitHub rejects outright.
//
// An unknown expression function does not fail the job it is in: it rejects the whole
// workflow before any job is planned. The run shows "This run likely failed because of a
// workflow file issue", zero jobs and no logs, which says nothing about which expression
// is wrong. That is how a `replace(matrix.platform, '/', '-')` I wrote in the mobile job
// cost a push to find: `replace` is not one of GitHub's functions, and every job of the
// run disappeared with it.
//
// The list is GitHub's own (docs: Evaluate expressions in workflows and actions), and the
// check is deliberately narrow: it looks at identifiers called as functions inside `${{ }}`,
// so a context property like `matrix.artifact` is not mistaken for one.
func TestWorkflowExpressionsUseOnlyRealFunctions(t *testing.T) {
	// GitHub's expression functions, plus the status checks allowed in `if`.
	known := map[string]bool{
		"contains": true, "startsWith": true, "endsWith": true, "format": true,
		"join": true, "toJSON": true, "fromJSON": true, "hashFiles": true, "case": true,
		"success": true, "failure": true, "cancelled": true, "always": true,
	}
	files, err := filepath.Glob(filepath.Join("..", "..", ".github", "workflows", "*.yml"))
	if err != nil {
		t.Fatal(err)
	}
	// `<name>(` inside an expression, with the name not preceded by a dot: `matrix.x(`
	// would be a property, not a function, and does not occur, but the guard costs nothing.
	call := regexp.MustCompile(`(^|[^.[:alnum:]_-])([A-Za-z_][A-Za-z0-9_]*)[[:space:]]*\(`)
	expr := regexp.MustCompile(`\$\{\{([^}]*)\}\}`)
	checked := 0
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range expr.FindAllStringSubmatch(stripYAMLComments(string(b)), -1) {
			checked++
			for _, c := range call.FindAllStringSubmatch(m[1], -1) {
				name := c[2]
				if known[name] {
					continue
				}
				t.Errorf("%s calls %q, which is not a GitHub Actions expression function; "+
					"the whole workflow is rejected before any job runs", filepath.Base(f), name)
			}
		}
	}
	if checked < 5 {
		t.Fatalf("only %d expressions found; the guard is not reading the workflows", checked)
	}
}

// stripYAMLComments removes `#` comments, so a comment that names a function — as the one
// explaining why `replace` is not used does — is not read as a call to it.
func stripYAMLComments(src string) string {
	lines := strings.Split(src, "\n")
	for i, line := range lines {
		if j := strings.Index(line, " #"); j >= 0 && !strings.Contains(line[:j], "'") {
			lines[i] = line[:j]
			continue
		}
		if strings.HasPrefix(strings.TrimSpace(line), "#") {
			lines[i] = ""
		}
	}
	return strings.Join(lines, "\n")
}

// TestWorkflowActionsAreOneMajor pins every workflow to a major and keeps them in step.
//
// The actions this repository uses are bumped by dependabot, which is how they got four majors
// behind on upload-artifact: nothing in the code notices, because an old action still runs. The
// two things worth holding here are that every `uses:` names an action at all (a typo'd ref
// fails only in CI), and that the same action is used at one version throughout — a workflow
// where checkout is v4 in one job and v7 in another works until the day someone copies the
// wrong one.
//
// The versions are read from the files, not listed here: the test asks whether they agree and
// are majors, not whether they are the versions the registry has today, so it does not need
// editing when dependabot opens its next PR.
func TestWorkflowActionsAreOneMajor(t *testing.T) {
	files, err := filepath.Glob(filepath.Join("..", "..", ".github", "workflows", "*.yml"))
	if err != nil {
		t.Fatal(err)
	}
	use := regexp.MustCompile(`uses:\s*([\w.-]+/[\w.-]+(?:/[\w.-]+)?)@(\S+)`)
	seen := map[string]string{} // action -> version, across every workflow
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range use.FindAllStringSubmatch(string(b), -1) {
			action, version := m[1], m[2]
			if strings.HasPrefix(action, "./") {
				continue // a local action, which has no version
			}
			if !regexp.MustCompile(`^v\d+$`).MatchString(version) {
				t.Errorf("%s: %s is pinned to %q, not a major; pinning a major is what lets "+
					"dependabot's group update move it with a reviewable diff",
					filepath.Base(f), action, version)
			}
			if prev, ok := seen[action]; ok && prev != version {
				t.Errorf("%s is used at both %s and %s; one version per action across the "+
					"workflows, or the two jobs run different code", action, prev, version)
			}
			seen[action] = version
		}
	}
	if len(seen) < 4 {
		t.Fatalf("found only %d actions across the workflows; the guard is not reading them",
			len(seen))
	}
}

// TestSnapshotRecipesExist keeps the visual gate runnable without hand-written shell.
//
// Comparing 39 PNGs was done by hand every time the UI changed, and the hand version failed
// twice in ways worth encoding: once against an empty baseline directory, which reported
// every file as changed, and once against a stale directory from a round that had rendered
// nothing. A recipe that refuses an empty baseline turns both into a clear error.
//
// The test checks the recipes exist and that the diff one guards the empty case, since that
// guard is the part a future edit is most likely to drop.
func TestSnapshotRecipesExist(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "..", "justfile"))
	if err != nil {
		t.Fatal(err)
	}
	src := string(b)

	for _, want := range []string{"\nsnapshots dir=", "\nsnapshots-diff base "} {
		if !strings.Contains(src, want) {
			t.Fatalf("the justfile has no %q recipe", strings.TrimSpace(want))
		}
	}

	// The render recipe must go through RENDER_DIR, or it would write to /tmp and the
	// diff would compare against whatever happened to be there.
	if !strings.Contains(src, `RENDER=1 RENDER_DIR="{{dir}}" go test`) {
		t.Fatal("the snapshots recipe does not render into its argument")
	}

	// And the diff must refuse an empty baseline.
	if !strings.Contains(src, "基线目录是空的") {
		t.Fatal("snapshots-diff no longer rejects an empty baseline; an empty directory " +
			"would report every file as changed")
	}
}

// TestContributingSnapshotCountMatchesTheSet keeps the guide's "43 张" honest.
//
// The count is a claim of the same shape as the tooltip number, and of the same shape as the
// Tab-stop heading that was once "五个" over a six-row table: a number written beside a list,
// which nothing re-derives when an entry is added. It is unusually easy to get wrong here
// because the set is produced by *two* test functions — `just snapshots` runs TestRenderSteps
// and TestRenderSizes together — so counting either alone gives 8 or 35 and neither is the
// number the guide means.
//
// Counting writeSnapshot call sites in the source does not work: several of them sit inside
// loops (three window sizes, seven stages, two widths), so 13 call sites produce 43 files. The
// count has to come from the render itself. That is affordable — about 3.5 seconds, once — and
// it is the only number that cannot drift from the set.
func TestContributingSnapshotCountMatchesTheSet(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "..", "CONTRIBUTING.md"))
	if err != nil {
		t.Fatalf("read CONTRIBUTING.md: %v", err)
	}
	doc := string(b)

	dir := t.TempDir()
	// The two functions `just snapshots` runs, together, into a scratch directory. This is
	// the same command the recipe issues, so the number counted here is the number a reader
	// gets from `just snapshots`.
	cmd := exec.Command("go", "test", ".", "-run", "TestRenderSteps|TestRenderSizes", "-count=1")
	cmd.Env = append(os.Environ(), "RENDER=1", "RENDER_DIR="+dir)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("rendering the snapshot set failed: %v\n%s", err, out)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read the rendered set: %v", err)
	}
	n := 0
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".png") {
			n++
		}
	}
	if n == 0 {
		t.Fatal("the snapshot set is empty; the guide's count is stale")
	}

	// Both mentions have to agree with the set, not just one. The guide states the number
	// twice (the recipe paragraph and the "screenshots cannot judge taste" note), and a check
	// that only looks for *a* match passes while the other copy says something else — verified
	// by mutation: changing one of the two to 44 left this test green until it counted them.
	want := fmt.Sprintf("%d 张", n)
	if got := strings.Count(doc, want); got != 2 {
		t.Fatalf("`just snapshots` writes %d PNGs, so CONTRIBUTING.md should say %q twice — "+
			"once in the recipe paragraph and once in the pixel-diff note — but the string "+
			"appears %d time(s); update both when the set changes on purpose", n, want, got)
	}
}

// TestEveryRecipeDescriptionIsOneLine keeps `just --list` readable.
//
// just takes the last comment line directly above a recipe as that recipe's description, so a
// multi-line comment block above a recipe silently promotes its *final* line into the listing.
// Four recipes had that: check showed "想一次对齐跑 just check-ci。", check-ci showed "两者分开是
// 有意的——…", docs showed "改完界面后跑一次这个。", and snapshots-diff showed "一张、或者把空目录
// 当基线（会误报「全部变化」）都不会有人发现。" — each a fragment of the sentence above it,
// listed as if it were the recipe's name.
//
// The rule this enforces: whatever comment sits directly above a recipe must be a complete,
// standalone description. Multi-line explanation is welcome, but it goes above a blank line.
func TestEveryRecipeDescriptionIsOneLine(t *testing.T) {
	src, err := os.ReadFile(filepath.Join("..", "..", "justfile"))
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(string(src), "\n")
	var bad []string
	for i, line := range lines {
		// A recipe line: `name:` or `name arg=default:` at column 0. Recipe bodies are
		// indented, so this cannot match one.
		if line == "" || line[0] == '#' || line[0] == ' ' || line[0] == '\t' {
			continue
		}
		head := line
		if j := strings.Index(head, ":"); j >= 0 {
			head = head[:j]
		} else {
			continue
		}
		// Recipes may carry parameters; the name is the first token.
		name := strings.Fields(head)[0]
		if name == "" || strings.HasPrefix(name, "_") {
			continue // private helpers are not listed
		}
		// `just --list` shows recipes only. These are the other things a justfile holds, and
		// they are not listed, so they need no description: settings and exports take `:=`,
		// variables are `name := value`, aliases are `alias x := y`.
		if name == "set" || name == "export" || name == "alias" {
			continue
		}
		if strings.Contains(line, ":=") {
			continue // a variable assignment such as windows_platforms := "windows/amd64"
		}
		// Walk up to the comment block.
		j := i - 1
		var block []string
		for j >= 0 && strings.HasPrefix(lines[j], "#") {
			block = append([]string{lines[j]}, block...)
			j--
		}
		if len(block) == 0 {
			bad = append(bad, fmt.Sprintf("%s has no description above it", name))
			continue
		}
		// The description is the last line. A description ending in a clause connector, or
		// consisting only of punctuation after a dash, is a fragment of the line above.
		desc := strings.TrimPrefix(block[len(block)-1], "#")
		desc = strings.TrimSpace(desc)
		if desc == "" {
			bad = append(bad, fmt.Sprintf("%s has an empty description", name))
			continue
		}
		for _, frag := range []string{"。", "；", "，"} {
			_ = frag
		}
		// A fragment is a description whose previous comment line does not end a sentence.
		if len(block) > 1 {
			prev := strings.TrimSpace(strings.TrimPrefix(block[len(block)-2], "#"))
			if prev != "" && !strings.HasSuffix(prev, "。") {
				bad = append(bad, fmt.Sprintf("%s: %q continues a sentence begun on the line "+
					"above, so it is a fragment of the explanation rather than a description",
					name, desc))
			}
		}
	}
	if len(bad) > 0 {
		t.Fatalf("just takes the last comment line above a recipe as its description, so "+
			"multi-line explanation has to sit above a blank line:\n  %s",
			strings.Join(bad, "\n  "))
	}
}

// TestGitignoreCoversTheLocalBuildOutput keeps `go build .` from leaving an untracked binary.
//
// The repository was renamed from llmlab to llm-xray: go.mod's module line, the sidebar title,
// mygo.json's name and slug, and every import path. .gitignore kept ignoring "/llm-lab", which
// after the rename matches nothing — so the binary that `go build .` writes into the
// repository root appeared as an untracked file, one `git add -A` away from being committed. The
// ignore rule had been correct and simply outlived the name it named, which is the failure mode a
// hand-maintained path list has and a derived one does not.
//
// The name is read out of go.mod rather than listed here, so the next rename moves both.
func TestGitignoreCoversTheLocalBuildOutput(t *testing.T) {
	root := filepath.Join("..", "..")

	mod, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		t.Fatalf("read go.mod: %v", err)
	}
	m := regexp.MustCompile(`(?m)^module\s+(\S+)$`).FindSubmatch(mod)
	if m == nil {
		t.Fatal("go.mod has no module line; this test cannot tell what the binary is called")
	}
	module := string(m[1])

	// `go build .` in the module root writes the executable under the last path element of the
	// module path.
	binary := module
	if i := strings.LastIndex(module, "/"); i >= 0 {
		binary = module[i+1:]
	}

	gitignore, err := os.ReadFile(filepath.Join(root, ".gitignore"))
	if err != nil {
		t.Fatalf("read .gitignore: %v", err)
	}
	want := "/" + binary
	if !strings.Contains(string(gitignore), want) {
		t.Fatalf(".gitignore does not ignore %q, the file `go build .` writes into the repository "+
			"root from module %q; the built binary would show up as untracked", want, module)
	}
}

// TestReleaseUploadsEveryPackageTheBuildProduces keeps the release from silently dropping an
// installer.
//
// The build job uploads `build/` wholesale, and the publish job then attaches an explicit list of
// extensions to the GitHub Release. `.rpm` was missing from that list while
// `go tool mygo build -platform linux/amd64` wrote one on every run — measured, 5.3 MB, and
// reproduced after deleting the file — so each release shipped the .deb and the tarball and
// dropped the package for RPM-based distributions. Nothing noticed: release.yml had no test at
// all, and the failure only appears on a tag, where the cost is a broken download rather than a
// red build.
//
// The check derives its expectations from the packaging the workflow itself performs rather than
// from a second hand-written list, because a list is what went stale. Two sources:
//
//   - the desktop platforms the build matrix names, whose mygo outputs are known and pinned here;
//   - the `.zip` the workflow creates for the macOS bundle, which is why no `.app` glob is needed.
//
// Deliberately excluded: the raw linux binary, install.sh, PKGBUILD and the unpacked `.app`
// directory. The list is explicit in the workflow so those do not become unlabelled release
// blobs, and requiring them here would undo that.
func TestReleaseUploadsEveryPackageTheBuildProduces(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "..", ".github", "workflows", "release.yml"))
	if err != nil {
		t.Fatalf("read release.yml: %v", err)
	}
	release := string(b)

	// The matrix has to build the three desktop platforms; if it stops, the upload list below is
	// checking installers that will never exist.
	for _, want := range []string{"linux/amd64,linux/arm64", "darwin/universal", "windows/amd64"} {
		if !strings.Contains(release, want) {
			t.Fatalf("release.yml no longer builds %q; update this test with the platforms you "+
				"ship", want)
		}
	}

	// What those platforms produce, and what the release therefore has to attach. `.zip` comes
	// from the workflow's own `ditto` step, not from mygo.
	installers := []struct {
		ext string
		why string
	}{
		{".deb", "Debian/Ubuntu package from linux/amd64"},
		{".rpm", "RPM package from linux/amd64 — the one this test exists for"},
		{".tar.gz", "portable tarball carrying install.sh"},
		{".exe", "NSIS installer from windows/amd64, plus the bare exe"},
		{".zip", "the macOS .app compressed by the ditto step"},
		{".dmg", "built on macOS when the toolchain allows it"},
	}

	// The files: block of the goreleaser action, which is the only place that decides what gets
	// attached.
	i := strings.Index(release, "files: |")
	if i < 0 {
		t.Fatal("release.yml no longer has a `files: |` block; this test cannot tell what the " +
			"release attaches")
	}
	block := release[i:]
	if j := strings.Index(block, "fail_on_unmatched_files"); j >= 0 {
		block = block[:j]
	}

	for _, in := range installers {
		if !strings.Contains(block, "*"+in.ext) {
			t.Errorf("release.yml does not attach *%s to the release (%s); the build produces it "+
				"and uploads it as an artifact, so it reaches the publish job and is then dropped",
				in.ext, in.why)
		}
	}

	// And the block has to stay a block: a single `files: dist/**` would attach everything,
	// including the raw binary and the unpacked .app tree, which is the reason the list is
	// explicit. Assert the globs are not collapsed into that.
	if strings.Contains(block, "dist/**\n") {
		t.Error("release.yml attaches dist/** wholesale; that publishes the raw linux binary, " +
			"install.sh, PKGBUILD and the unpacked .app directory as unlabelled release assets")
	}
}

// TestReadmeArtifactTableNamesEveryLinuxPackage keeps the README's artifact table from omitting
// a package the build writes.
//
// The table listed the .deb, the tarball and the PKGBUILD but not the .rpm, which
// `go tool mygo build -platform linux/amd64` produces on every run. That is the same omission the
// release workflow had, in the place a reader looks first: someone on Fedora reads the table,
// concludes there is no RPM, and does not go looking on the release page either.
//
// It is checked against the real build directory when one exists, and against the workflow's own
// upload list otherwise, so it works on a clean checkout as well as after `just linux`. Both
// sources describe the same set of packages; the workflow's list is the one this repository
// controls, and the build directory is what actually happened.
func TestReadmeArtifactTableNamesEveryLinuxPackage(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "..", "README.md"))
	if err != nil {
		t.Fatalf("read README.md: %v", err)
	}
	doc := string(b)

	// The linux row of the artifact table.
	const row = "| `linux/amd64`、`linux/arm64` |"
	i := strings.Index(doc, row)
	if i < 0 {
		t.Fatal("the README no longer has the linux row of the artifact table; update this test")
	}
	end := strings.Index(doc[i:], "\n")
	if end < 0 {
		t.Fatal("the linux row is the last line of the file")
	}
	linuxRow := doc[i : i+end]

	// Every package extension the release attaches for Linux. Kept in step with release.yml by
	// TestReleaseUploadsEveryPackageTheBuildProduces, so this list cannot drift from it alone.
	for _, ext := range []string{".deb", ".rpm", ".tar.gz"} {
		if !strings.Contains(linuxRow, ext) {
			t.Errorf("the README's linux row does not name %s, which the build produces and the "+
				"release attaches; a reader on that package manager concludes there is no such "+
				"package", ext)
		}
	}
	// The PKGBUILD is built but never released as a package, and the row should still say so —
	// it is what an Arch reader needs, and dropping it would be its own regression.
	if !strings.Contains(linuxRow, "PKGBUILD") {
		t.Error("the README's linux row no longer mentions PKGBUILD, which the build writes even " +
			"though it is not a release asset")
	}

	// And the build directory, when present, must not contain a package the row does not name.
	// Skipped on a clean checkout, where no build has run.
	entries, err := os.ReadDir(filepath.Join("..", "..", "build", "linux-amd64"))
	if err != nil {
		return // nothing built here; the extensions above are the whole claim
	}
	for _, e := range entries {
		name := e.Name()
		for _, ext := range []string{".deb", ".rpm", ".tar.gz"} {
			if strings.HasSuffix(name, ext) && !strings.Contains(linuxRow, ext) {
				t.Fatalf("build/linux-amd64 contains %s but the README's linux row does not name "+
					"%s", name, ext)
			}
		}
	}
}

// TestAndroidSplashMatchesTheAppBackground keeps the launch screen from flashing a different
// colour than the window it opens into.
//
// mygo.json sets `android.splash` to #0E1117 "与背景同色，启动时不闪白" per the CHANGELOG, and
// theme.go sets colBg to the same literal. Nothing connected them: measured by changing colBg to
// #101820, the whole suite stayed green while every Android launch would flash the old colour for
// as long as the runtime takes to draw the first frame.
//
// This is the "two values that must agree, with no mechanism keeping them in sync" shape that
// CONTRIBUTING lists as its eighth figure-reading mistake. The fix is the usual one: make the
// comparison a test rather than a habit.
func TestAndroidSplashMatchesTheAppBackground(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "..", "mygo.json"))
	if err != nil {
		t.Fatalf("read mygo.json: %v", err)
	}
	var cfg struct {
		Android struct {
			Splash string `json:"splash"`
		} `json:"android"`
	}
	if err := json.Unmarshal(b, &cfg); err != nil {
		t.Fatalf("parse mygo.json: %v", err)
	}
	if cfg.Android.Splash == "" {
		t.Skip("no android.splash is configured; there is no launch colour to match")
	}

	want := ui.Hex(cfg.Android.Splash)
	if want != colBg {
		t.Fatalf("mygo.json sets the Android splash to %s, but the app's background is "+
			"#%02X%02X%02X — the launch screen will flash a different colour before the first "+
			"frame is drawn", cfg.Android.Splash, colBg.R, colBg.G, colBg.B)
	}
}

// TestSidebarTitleMatchesTheBundleName keeps the app's own title in step with its package name.
//
// mygo.json's `name` decides what the bundle is called — llm-xray.app, llm-xray.exe, and the
// label under the icon — and the sidebar's first line is the same word, which is what the rename
// to llm-xray deliberately established ("设定 name/slug/identifier 为 llm-xray … 侧栏也随之从
// LLM Lab 改成 llm-xray"). They are two literals with no mechanism between them: measured by
// changing `name` to "LLM X-Ray", the whole suite stayed green while the window would open titled
// llm-xray inside a package called LLM X-Ray.
//
// The occurrence count is asserted too. The title is drawn twice — app.go for the desktop sidebar
// and compact.go for the phone header — and both have to render the same string; a check that only
// greps for *a* match would pass with one of them left on an old name. Verified by changing only
// the compact one.
func TestSidebarTitleMatchesTheBundleName(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "..", "mygo.json"))
	if err != nil {
		t.Fatalf("read mygo.json: %v", err)
	}
	var cfg struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(b, &cfg); err != nil {
		t.Fatalf("parse mygo.json: %v", err)
	}
	if cfg.Name == "" {
		t.Fatal("mygo.json no longer sets a name; the bundle has no identity to compare against")
	}

	// Every non-test source file that draws the sidebar or compact header title.
	for _, file := range []string{"app.go", "compact.go"} {
		src, err := os.ReadFile(file)
		if err != nil {
			t.Fatalf("read %s: %v", file, err)
		}
		if !strings.Contains(string(src), strconv.Quote(cfg.Name)) {
			t.Fatalf("%s does not draw the title %q that mygo.json names the bundle; the app "+
				"opens under one name inside a package called another", file, cfg.Name)
		}
	}

	// And the rendered title has to be findable, not merely present in the source: a string built
	// from pieces would satisfy the grep above and still draw something else.
	for _, size := range [][2]int{{1500, 940}, {375, 667}} {
		a := newTestApp(t)
		tr := ui.NewTester(a.View, size[0], size[1])
		tr.Frame()
		if _, ok := tr.Find(cfg.Name); !ok {
			t.Fatalf("%dx%d: the title %q is not rendered", size[0], size[1], cfg.Name)
		}
	}
}

// TestOutputDirectoryMatchesMygoJson keeps `just clean`, `just artifacts` and .gitignore pointed
// at the directory the build actually writes.
//
// mygo.json's `out` decides where `go tool mygo build` puts things; the justfile's clean recipe
// hardcodes `rm -rf build`, its artifacts recipe reads `build/*/*`, and .gitignore ignores
// /build/. Four copies of one word. Measured by changing `out` to "dist": the whole suite stayed
// green while `just clean` would leave the real output tree on disk and `just artifacts` would
// report an empty build/ that no longer receives anything.
//
// The README's artifact table says `build/<平台>/` too, and is checked here for the same reason —
// it is the fifth copy and the one a reader consults first.
func TestOutputDirectoryMatchesMygoJson(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "..", "mygo.json"))
	if err != nil {
		t.Fatalf("read mygo.json: %v", err)
	}
	var cfg struct {
		Out string `json:"out"`
	}
	if err := json.Unmarshal(b, &cfg); err != nil {
		t.Fatalf("parse mygo.json: %v", err)
	}
	if cfg.Out == "" {
		t.Fatal("mygo.json no longer sets `out`; the build directory is implied and this test " +
			"has nothing to compare against")
	}

	justfile, err := os.ReadFile(filepath.Join("..", "..", "justfile"))
	if err != nil {
		t.Fatalf("read justfile: %v", err)
	}
	jf := string(justfile)

	// The two recipes that touch the output directory.
	for _, want := range []struct{ recipe, needle string }{
		{"clean", "rm -rf " + cfg.Out},
		{"artifacts", "du -sh " + cfg.Out + "/*/*"},
	} {
		if !strings.Contains(jf, want.needle) {
			t.Errorf("the %s recipe does not contain %q, so it is pointed at a directory the "+
				"build no longer writes (mygo.json's out is %q)", want.recipe, want.needle, cfg.Out)
		}
	}

	// .gitignore has to cover it, or every build leaves an untracked tree.
	gitignore, err := os.ReadFile(filepath.Join("..", "..", ".gitignore"))
	if err != nil {
		t.Fatalf("read .gitignore: %v", err)
	}
	if !strings.Contains(string(gitignore), "/"+cfg.Out+"/") {
		t.Errorf(".gitignore does not ignore /%s/, the directory `go tool mygo build` writes to; "+
			"a full build would leave an untracked tree", cfg.Out)
	}

	// And the README tells the reader where to look.
	readme, err := os.ReadFile(filepath.Join("..", "..", "README.md"))
	if err != nil {
		t.Fatalf("read README.md: %v", err)
	}
	if !strings.Contains(string(readme), cfg.Out+"/<平台>/") {
		t.Errorf("the README no longer says the artifacts land in %s/<平台>/; a reader following "+
			"it looks in the wrong directory", cfg.Out)
	}
}
