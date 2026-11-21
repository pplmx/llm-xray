package model

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestNoUnusedFunctionsInModel finds unexported functions in this package that
// nothing refers to.
//
// The same check runs over internal/app. Adding it here matters because the model
// package is where the arithmetic lives: a helper left behind after a rewrite looks
// like the definition of how a tensor is computed, and a reader has no way to tell
// it is no longer called.
//
// Textual rather than type-based on purpose: over-approximating references can only
// produce false negatives, never a report on a function that is actually used.
func TestNoUnusedFunctionsInModel(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	type decl struct {
		file string
		kind string // "()" for a function, "" for a constant or variable
	}
	declared := map[string]decl{}
	stripped := make([]string, 0, len(files))

	for _, name := range files {
		src, err := os.ReadFile(name)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		// Every file gets its own FileSet. Sharing one across the loop makes each
		// later file's offsets cumulative, so the "blank this declaration's own name"
		// edit below lands at the wrong place in all but the first file.
		fileFset := token.NewFileSet()
		f, err := parser.ParseFile(fileFset, name, src, parser.ParseComments)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}

		// Blank the comments from the parser's own ranges rather than scanning bytes.
		// A hand-rolled scanner cannot tell a comment opener from the same two bytes
		// inside a string literal, and this package's tests call filepath.Glob("*.go")
		// — the "/*" in that literal starts a block comment as far as a byte scanner is
		// concerned, which swallows the rest of the file and hides every declaration in
		// it. The app package had exactly this defect and it was live: two files lost
		// eight function declarations between them while the check stayed green.
		// Nothing triggers it here yet, and this is why it will not.
		out := []byte(src)
		for _, group := range f.Comments {
			for _, c := range group.List {
				off := fileFset.Position(c.Pos()).Offset
				end := fileFset.Position(c.End()).Offset
				for i := off; i < end && i < len(out); i++ {
					if out[i] != '\n' {
						out[i] = ' '
					}
				}
			}
		}
		// String literals, for the same reason and by the same AST-based means. A name that
		// appears only inside a string is text, not a reference; treating it as a use used to
		// hide a real orphan in the app package.
		ast.Inspect(f, func(n ast.Node) bool {
			lit, ok := n.(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				return true
			}
			off := fileFset.Position(lit.Pos()).Offset
			end := fileFset.Position(lit.End()).Offset
			for i := off + 1; i < end-1 && i < len(out); i++ {
				if out[i] != '\n' {
					out[i] = ' '
				}
			}
			return true
		})

		for _, d := range f.Decls {
			// Package-level constants and variables. Unused ones are legal Go, so neither
			// the compiler nor vet reports them, and a leftover tuning constant reads like a
			// live parameter of the forward pass.
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
						for i := off; i < off+len(id.Name) && i < len(out); i++ {
							out[i] = ' '
						}
					}
				}
				continue
			}
			fd, ok := d.(*ast.FuncDecl)
			if !ok || fd.Name == nil || fd.Name.IsExported() {
				continue
			}
			if strings.HasPrefix(fd.Name.Name, "Test") ||
				strings.HasPrefix(fd.Name.Name, "Benchmark") ||
				strings.HasPrefix(fd.Name.Name, "Example") {
				continue // entry points, called by the testing package
			}
			declared[fd.Name.Name] = decl{file: name, kind: "()"}
			// Blank the declaration's own name, at the same length, so no offset moves.
			off := fileFset.Position(fd.Name.Pos()).Offset
			for i := off; i < off+len(fd.Name.Name) && i < len(out); i++ {
				out[i] = ' '
			}
			// The receiver's type name as well: it appears in the declaration, not at a use,
			// so a type nobody constructs can keep its methods and still be dead. Measured
			// in the app package, an orphan type with a method was accepted as used.
			if fd.Recv != nil {
				for _, recv := range fd.Recv.List {
					ast.Inspect(recv.Type, func(n ast.Node) bool {
						id, ok := n.(*ast.Ident)
						if !ok {
							return true
						}
						ro := fileFset.Position(id.Pos()).Offset
						for i := ro; i < ro+len(id.Name) && i < len(out); i++ {
							out[i] = ' '
						}
						return true
					})
				}
			}
		}
		stripped = append(stripped, string(out))
	}

	joined := strings.Join(stripped, "\n")
	for name, d := range declared {
		if !hasIdent(joined, name) {
			t.Errorf("%s declares %s%s, which no file in the package refers to",
				d.file, name, d.kind)
		}
	}
}

func hasIdent(s, name string) bool {
	for i := 0; ; {
		j := strings.Index(s[i:], name)
		if j < 0 {
			return false
		}
		start := i + j
		end := start + len(name)
		if (start == 0 || !identByte(s[start-1])) && (end >= len(s) || !identByte(s[end])) {
			return true
		}
		i = start + 1
	}
}

func identByte(b byte) bool {
	return b == '_' || (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || (b >= '0' && b <= '9')
}
