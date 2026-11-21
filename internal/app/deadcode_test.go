package app

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// TestNoUnusedFunctionsInApp finds unexported functions and methods in this
// package that no other file refers to.
//
// The package had accumulated four: heatmap (a wrapper around heatmapAxed fixed at
// axes=false that no call site used, since both real callers pass true), plus
// renderFFN, renderFFNPositive and drawHeatmap in the tests — helpers left behind
// when the tests around them were rewritten inline. Unused code is not just
// clutter: it is where a reader asking "how does this widget work" lands first,
// and it was describing a mode the app never renders.
//
// Deliberately textual: an identifier occurring anywhere outside its own
// declaration counts as a reference, which over-approximates and therefore never
// reports a function that is actually used.
func TestNoUnusedFunctionsInApp(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	// name -> file where it is declared, for unexported funcs, methods, constants and
	// variables. The kind is kept so the failure message uses the right syntax: "foo()"
	// is right for a function and wrong for a constant.
	type decl struct {
		file string
		kind string // "()" for a function, "" for a constant or variable
	}
	declared := map[string]decl{}
	// Source of every file, with each declaration's own name blanked out, so a
	// name that appears only at its definition is not counted as a use.
	stripped := make([]string, 0, len(files))

	for _, name := range files {
		src, err := readFileString(name)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		// Strip comments from the pristine source, before any name is blanked. Blanking a
		// declaration's name makes the file unparseable — `func (a *App) foo(` becomes
		// `func (a *App)    (` — so the parser has to see the original, and the offsets it
		// reports are the ones used for both edits.
		fileFset := token.NewFileSet()
		f, err := parser.ParseFile(fileFset, name, src, parser.ParseComments)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}

		// Blank the comments, keeping newlines so a later failure's line numbers still line
		// up. A space in place of every other comment byte keeps the identifiers on either
		// side from fusing.
		blanked := []byte(src)
		for _, group := range f.Comments {
			for _, c := range group.List {
				off := fileFset.Position(c.Pos()).Offset
				endOff := fileFset.Position(c.End()).Offset
				for i := off; i < endOff && i < len(blanked); i++ {
					if blanked[i] != '\n' {
						blanked[i] = ' '
					}
				}
			}
		}
		// String literals too. A name that appears only inside a string is not a reference:
		// the code never reads the function, and treating the text as a use hid a real orphan
		// — measured, a function named in a nearby string literal was accepted as used.
		// BasicLit keeps the quoting characters; blank everything between them, which is
		// where a name could sit.
		ast.Inspect(f, func(n ast.Node) bool {
			lit, ok := n.(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				return true
			}
			off := fileFset.Position(lit.Pos()).Offset
			endOff := fileFset.Position(lit.End()).Offset
			for i := off + 1; i < endOff-1 && i < len(blanked); i++ {
				if blanked[i] != '\n' {
					blanked[i] = ' '
				}
			}
			return true
		})

		for _, d := range f.Decls {
			// Package-level constants and variables. The compiler does not report these —
			// an unused package-level identifier is legal Go — so a leftover tuning
			// constant survives `go build` and `go vet` and reads like a live parameter.
			// Same treatment as a function: record it, blank its own name, and require some
			// other file position to mention it.
			if gd, ok := d.(*ast.GenDecl); ok &&
				(gd.Tok == token.CONST || gd.Tok == token.VAR || gd.Tok == token.TYPE) {
				for _, spec := range gd.Specs {
					var ids []*ast.Ident
					switch s := spec.(type) {
					case *ast.ValueSpec:
						ids = s.Names
					case *ast.TypeSpec:
						ids = []*ast.Ident{s.Name}
					}
					for _, id := range ids {
						if id == nil || id.IsExported() || id.Name == "_" {
							continue
						}
						declared[id.Name] = decl{file: name}
						off := fileFset.Position(id.Pos()).Offset
						for i := off; i < off+len(id.Name) && i < len(blanked); i++ {
							blanked[i] = ' '
						}
					}
				}
				continue
			}
			fd, ok := d.(*ast.FuncDecl)
			if !ok || fd.Name == nil || fd.Name.IsExported() {
				continue
			}
			if strings.HasSuffix(name, "_test.go") {
				// Test, Benchmark and Example functions are entry points, not
				// helpers, and are called by the testing package.
				if strings.HasPrefix(fd.Name.Name, "Test") ||
					strings.HasPrefix(fd.Name.Name, "Benchmark") ||
					strings.HasPrefix(fd.Name.Name, "Example") {
					continue
				}
			}
			declared[fd.Name.Name] = decl{file: name, kind: "()"}
			// Blank the declaration's own name, so a name that appears only at its
			// definition is not counted as a use. Same length, so no offset moves.
			off := fileFset.Position(fd.Name.Pos()).Offset
			for i := off; i < off+len(fd.Name.Name) && i < len(blanked); i++ {
				blanked[i] = ' '
			}
			// The receiver's type name too. `func (o orphan) m()` mentions the type, but
			// that is part of the declaration, not a use: a type nobody ever constructs
			// keeps its methods and is still dead. Measured, an orphan type with a method
			// was accepted as used because of the receiver.
			if fd.Recv != nil {
				for _, recv := range fd.Recv.List {
					ast.Inspect(recv.Type, func(n ast.Node) bool {
						id, ok := n.(*ast.Ident)
						if !ok {
							return true
						}
						ro := fileFset.Position(id.Pos()).Offset
						for i := ro; i < ro+len(id.Name) && i < len(blanked); i++ {
							blanked[i] = ' '
						}
						return true
					})
				}
			}
		}
		stripped = append(stripped, string(blanked))
	}

	joined := strings.Join(stripped, "\n")
	for name, d := range declared {
		// Word-boundary search: the name followed by a character that cannot
		// continue an identifier.
		if !containsIdentifier(joined, name) {
			t.Errorf("%s declares %s%s, which no file in the package refers to",
				d.file, name, d.kind)
		}
	}
}

// containsIdentifier reports whether s contains name as a whole identifier.
func containsIdentifier(s, name string) bool {
	for i := 0; ; {
		j := strings.Index(s[i:], name)
		if j < 0 {
			return false
		}
		start := i + j
		end := start + len(name)
		beforeOK := start == 0 || !isIdentChar(s[start-1])
		afterOK := end >= len(s) || !isIdentChar(s[end])
		if beforeOK && afterOK {
			return true
		}
		i = start + 1
	}
}

func isIdentChar(b byte) bool {
	return b == '_' || (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || (b >= '0' && b <= '9')
}

// readFileString reads a file in this directory as a string.
func readFileString(name string) (string, error) {
	b, err := os.ReadFile(name)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// TestNoUnusedFunctionParameters finds parameters that a function never reads.
//
// Go allows them — a signature may be fixed by an interface, or a parameter may exist for
// symmetry with a sibling — so neither the compiler nor vet says anything. The cost is real
// though: `bars(c, rows, max, colGreen)` read as if it painted the bars green, and the parameter
// was ignored entirely, the colour coming from each row. Six call sites passed a colour for a
// parameter nothing read, and one of them passed colGreen while its second row was colAccent2.
//
// Parameters named `_` are the explicit way to say "deliberately unused" and are exempt. A
// function whose body is empty is exempt too: it exists to satisfy a shape, not to compute.
func TestNoUnusedFunctionParameters(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	checked := 0
	for _, name := range files {
		src, err := readFileString(name)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, name, src, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		ast.Inspect(f, func(n ast.Node) bool {
			fd, ok := n.(*ast.FuncDecl)
			if !ok || fd.Body == nil {
				return true
			}
			params := map[string]bool{}
			for _, field := range fd.Type.Params.List {
				for _, id := range field.Names {
					if id.Name != "_" {
						params[id.Name] = true
					}
				}
			}
			if len(params) == 0 {
				return true
			}
			checked++
			used := map[string]bool{}
			ast.Inspect(fd.Body, func(m ast.Node) bool {
				if id, ok := m.(*ast.Ident); ok {
					used[id.Name] = true
				}
				return true
			})
			for p := range params {
				if !used[p] {
					t.Errorf("%s: %s never reads its parameter %s; name it _ if that is deliberate",
						fset.Position(fd.Pos()), fd.Name.Name, p)
				}
			}
			return true
		})
	}
	if checked < 50 {
		t.Fatalf("only %d functions with parameters were examined; the scan is not reaching the "+
			"package", checked)
	}
}

// TestNoStaleLineNumberReferences keeps comments from anchoring to line numbers.
//
// A comment said the undo crash "panicked in topbar at app.go:883". That was true when it was
// written — the number came from the stack trace — but app.go:883 is now `if stages > len(digits)`,
// and a reader who follows the anchor lands in unrelated code with no way to tell. A function name
// survives refactors; a line number does not.
//
// The check allows the phrase "at the time" beside the number, which is how the surviving
// reference reads now: it says what the stack said then, not where to look.
func TestNoStaleLineNumberReferences(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	pat := regexp.MustCompile(`\b\w+\.go:\d+\b`)
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		src, err := readFileString(name)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		for i, line := range strings.Split(src, "\n") {
			if !strings.Contains(line, "//") {
				continue
			}
			if pat.MatchString(line) && !strings.Contains(line, "at the time") {
				t.Errorf("%s:%d anchors to a line number, which goes stale: %s", name, i+1,
					strings.TrimSpace(line))
			}
		}
	}
}
