package aboard

import (
	"go/ast"
	"go/parser"
	"go/token"
	"log"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// "Server logging goes through Options.Logger" (aboard.go) had two holes, and a
// host embedding this tree could redirect neither: a tab an agent tried to
// delete, and a handler that could not serialise its own reply. Both went to the
// standard logger, so under `ape aboard` they appeared wherever ape's default
// log happened to point — which for a CLI is stderr, in the middle of somebody's
// output.
func TestADroppedTabIsReportedThroughOptionsLogger(t *testing.T) {
	var logged strings.Builder
	logger := log.New(&logged, "", 0)

	current := []byte(`{"tabs":[{"id":"ab1","name":"Queue","type":"kanban","state":{}}]}`)
	incoming := []byte(`{"tabs":[]}`)

	if _, err := reconcileTabs(current, incoming, "agent-1", logger); err != nil {
		t.Fatal(err)
	}
	out := logged.String()
	if !strings.Contains(out, "Queue") || !strings.Contains(out, "ab1") {
		t.Errorf("the dropped tab was not reported through the logger: %q", out)
	}
}

func TestAnUnserialisableReplyIsReportedThroughOptionsLogger(t *testing.T) {
	var logged strings.Builder
	srv := &server{opts: Options{Logger: log.New(&logged, "", 0)}}

	// A channel is not JSON. The status line is already on the wire by then, so
	// the log line is the only trace this failure leaves.
	srv.writeJSON(httptest.NewRecorder(), 500, map[string]any{"bad": make(chan int)})

	if out := logged.String(); !strings.Contains(out, "writeJSON") {
		t.Errorf("the encoding failure was not reported through the logger: %q", out)
	}
}

// The audit, so the next one cannot be added quietly. `log.Printf` / `log.Print`
// / `log.Println` / `log.Fatal*` write to the standard logger, which is exactly
// the thing Options.Logger exists to replace. Nothing in the engine may call
// them — the library rule in aboard.go, made checkable.
//
// The WHOLE module, not just this package: `cli/` and `cmd/` are as embeddable as
// the engine is — cmd/ is the only place allowed to end the process, and it does
// that through cli.Execute's status, not through log.Fatal. A walk that stopped
// at this directory would leave the two packages a host actually mounts
// unchecked, which is the same shape as the joins rule having a package-sized
// exemption.
func TestNothingInTheEngineLogsToTheStandardLogger(t *testing.T) {
	fset := token.NewFileSet()
	err := filepath.WalkDir("../..", func(name string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			return nil
		}
		if strings.Contains(name, "/.aboard/") || strings.Contains(name, "/dist/") {
			return nil
		}
		file, perr := parser.ParseFile(fset, name, nil, 0)
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
			if !ok || pkg.Name != "log" {
				return true
			}
			switch sel.Sel.Name {
			case "Printf", "Print", "Println", "Fatal", "Fatalf", "Fatalln", "Panic", "Panicf", "Panicln":
				t.Errorf("%s:%d calls log.%s — engine logging goes through Options.Log(), which a host can redirect",
					name, fset.Position(call.Pos()).Line, sel.Sel.Name)
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
