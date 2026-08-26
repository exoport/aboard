package aboard

import (
	"bytes"
	"io/fs"
	"os"
	"strings"
	"testing"

	"github.com/exoport/aboard/pkg/aboard/web"
)

// `.gitattributes` marks the embedded tree, the fixtures and the generated skill
// files `-text` so git never rewrites their line endings. That is not tidiness:
// `capsHash` is computed over the BYTES of views/*.spec.json, and the Go suite
// compares the committed controls module against one generated in memory — which
// always has LF endings, whatever the checkout did. A Windows checkout with
// core.autocrlf=true and no `-text` fails `make test` on the windows-latest
// runner for a tree that is byte-identical to the one that passed on Linux.
//
// The attribute file is a claim like any other. This is the check that makes it
// one that can fail: if the attribute is ever dropped, or a new byte-compared
// file lands outside its patterns, a CRLF gets in and this says so on every
// platform rather than only on Windows.
func TestNoCarriageReturnsInTheBytesCapsHashIsComputedOver(t *testing.T) {
	var offenders []string
	err := fs.WalkDir(web.FS, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		// The mermaid bundle is a committed third-party artifact and is large;
		// its bytes feed no hash and no comparison.
		if strings.HasPrefix(path, "lib/") {
			return nil
		}
		body, err := fs.ReadFile(web.FS, path)
		if err != nil {
			return err
		}
		if bytes.Contains(body, []byte("\r\n")) {
			offenders = append(offenders, "pkg/aboard/web/"+path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, o := range offenders {
		t.Errorf("%s has CRLF line endings — capsHash is computed over these bytes; check .gitattributes still marks the tree `-text`", o)
	}
}

// The fixtures and the generated skill files, which are compared byte-for-byte
// against something generated with LF endings.
func TestNoCarriageReturnsInTheByteComparedFiles(t *testing.T) {
	root := repoRoot(t)
	for _, path := range []string{
		root.SkillReference(),
		root.SkillRecipes(),
	} {
		body, err := os.ReadFile(path)
		if err != nil {
			t.Errorf("%s: %v", path, err)
			continue
		}
		if bytes.Contains(body, []byte("\r\n")) {
			t.Errorf("%s has CRLF line endings — `make caps` regenerates it with LF and the gate compares the bytes", path)
		}
	}

	body, err := fs.ReadFile(exampleFS, exampleFile)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(body, []byte("\r\n")) {
		t.Error("the example board has CRLF line endings")
	}
}
