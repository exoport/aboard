package aboard

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// "One resolved root. Paths are joined in layout.go and nowhere else" is a rule
// CLAUDE.md states and nothing enforced — so it had four violations when
// somebody went looking, and one of them (a temp file created beside the state
// document) was added while fixing something else in the same review.
//
// It matters because the rule is not tidiness. Every path under `.aboard/` is
// derived from one resolved root, and the port is derived from that same root:
// a second place that builds a path is a second place that can disagree about
// where the board is, which is exactly the symlink bug this review already
// found in FindRoot.
//
// The check walks the AST rather than grepping, because a grep matches the word
// inside a comment — this repo's comments discuss filepath.Join by name — and a
// check that fires on its own documentation gets muted.
func TestNothingOutsideLayoutJoinsAPath(t *testing.T) {
	// Files under the module that are allowed to call it. layout.go is the rule;
	// _test.go files are not shipped code and build their own fixtures.
	allowed := func(path string) bool {
		base := filepath.Base(path)
		return base == "layout.go" || strings.HasSuffix(base, "_test.go")
	}

	// The whole module, not just pkg/. The rule CLAUDE.md states is "no
	// filepath.Join outside pkg/aboard/layout.go" — a rule about the TREE — and
	// a walk that started at pkg/ would have left cmd/ as a standing exemption,
	// which is how a rule stops being one. (cmd/ is clean today; that is the
	// point of checking it.)
	var offences []string
	err := filepath.WalkDir("../..", func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if !strings.HasSuffix(path, ".go") || allowed(path) {
			return nil
		}
		// Anything git ignores is not this module's source: a scratch board or a
		// vendored copy under the root must not fail somebody else's rule.
		if strings.Contains(path, "/.aboard/") || strings.Contains(path, "/dist/") {
			return nil
		}
		fset := token.NewFileSet()
		file, perr := parser.ParseFile(fset, path, nil, 0)
		if perr != nil {
			return perr
		}
		ast.Inspect(file, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			pkg, ok := sel.X.(*ast.Ident)
			if !ok || pkg.Name != "filepath" {
				return true
			}
			if sel.Sel.Name == "Join" {
				offences = append(offences, path+":"+
					strings.TrimPrefix(fset.Position(call.Pos()).String(), fset.Position(call.Pos()).Filename+":"))
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, o := range offences {
		t.Errorf("%s calls filepath.Join — paths are joined in pkg/aboard/layout.go and nowhere else; add a helper there", o)
	}
}

// The rule is only worth having if layout.go actually does the joining, so the
// negative is asserted too: a layout.go that had stopped joining anything would
// mean the joins had moved somewhere this check does not look.
func TestLayoutIsWhereThePathsAreJoined(t *testing.T) {
	body, err := os.ReadFile("layout.go")
	if err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(string(body), "filepath.Join("); n < 10 {
		t.Errorf("layout.go joins only %d paths; the rule says every one of them lives here", n)
	}
}
