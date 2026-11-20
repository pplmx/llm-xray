package app

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// TestSpacingValuesComeFromASmallSet keeps paddings and gaps from turning into one-off
// numbers.
//
// The layouts use 0, 1, 2, 3, 4, 5, 6, 8, 9, 10, 12, 14, 16 and 24 today. Each value has a
// reason — a chip's inner padding is not a card's padding — but the set itself is what keeps
// elements that should align aligning. A new layout that invents 7 or 11 makes two things
// that sat on the same line stop, and nothing on screen says which is wrong.
//
// The test parses every Gap/Padding/Margin call in the three layout files and requires each
// number to be in the list. Adding a step is allowed; it just has to be a deliberate edit
// here rather than a typo in one call site.
func TestSpacingValuesComeFromASmallSet(t *testing.T) {
	allowed := map[int]bool{
		0: true, 1: true, 2: true, 3: true, 4: true, 5: true, 6: true, 8: true,
		9: true, 10: true, 12: true, 14: true, 16: true, 24: true,
	}

	spacing := regexp.MustCompile(`\.(?:Gap|Padding|Margin)\(([0-9, ]+)\)`)
	total := 0
	// Every non-test file in the package, discovered rather than listed.
	//
	// This used to name three files by hand, and the package has seven: compact.go and theme.go
	// both lay out spacing, and neither was scanned. Measured — introducing `.Gap(13)` (13 is
	// not in the allowed set) into compact.go left this test green, because it never opened the
	// file. A spacing scale that only covers the files someone remembered is not a scale.
	for _, name := range sourceFiles(t) {
		src, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range spacing.FindAllStringSubmatch(string(src), -1) {
			for _, field := range strings.Split(m[1], ",") {
				field = strings.TrimSpace(field)
				if field == "" {
					continue
				}
				v, err := strconv.Atoi(field)
				if err != nil {
					t.Fatalf("%s: cannot parse spacing %q", name, field)
				}
				total++
				if !allowed[v] {
					t.Fatalf("%s uses spacing %d, which is not in the set; either it is a "+
						"typo or the design gained a step that belongs in this list", name, v)
				}
			}
		}
	}
	if total < 50 {
		t.Fatalf("only found %d spacing values; the regexp probably stopped matching",
			total)
	}
	t.Logf("checked %d spacing values against %d allowed steps", total, len(allowed))
}

// sourceFiles returns every non-test .go file in the package, sorted.
//
// Two guards in this package walk the sources looking for a convention — the spacing scale and
// the data-colour rule — and both used to name their files by hand. The package has seven, the
// lists said three and four, and the missing file in both cases was compact.go: the phone layout,
// which was added later and never added to either list. Measured — a `.Gap(13)` (not in the
// allowed set) and a `colGreen` on a text label, both in compact.go, each left its guard green.
//
// Enumerating means a new file is covered the moment it exists, which is what these conventions
// need: they are about the package, not about the files someone remembered.
func sourceFiles(t *testing.T) []string {
	t.Helper()
	paths, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, p := range paths {
		if strings.HasSuffix(p, "_test.go") {
			continue
		}
		out = append(out, p)
	}
	sort.Strings(out)
	if len(out) < 5 {
		t.Fatalf("found only %d source files; the glob stopped matching", len(out))
	}
	return out
}
