package aboard

import (
	"encoding/json"
	"testing"

	"github.com/exoport/aboard/pkg/aboard/web"
)

// capsHash fingerprints the DESCRIBED SURFACE. If it moved between two calls in
// one process, every `capabilities --check` would report drift that does not
// exist, and the warning that is meant to catch a stale skill would be noise
// within a week.
func TestCapsHashIsStable(t *testing.T) {
	first, err := buildManifest(web.FS)
	if err != nil {
		t.Fatalf("buildManifest: %v", err)
	}
	second, err := buildManifest(web.FS)
	if err != nil {
		t.Fatalf("buildManifest (again): %v", err)
	}
	if first.Hash == "" {
		t.Fatal("capsHash is empty")
	}
	if first.Hash != second.Hash {
		t.Fatalf("capsHash moved between two calls: %s then %s", first.Hash, second.Hash)
	}
}

// The manifest describes the BOARD, not the process serving it, so a board
// hosted by ape must hash identically to a standalone one. Asserted on the field
// rather than trusted, because it is the reason the rename was ordered last.
func TestManifestAppIsHostIndependent(t *testing.T) {
	m, err := buildManifest(web.FS)
	if err != nil {
		t.Fatal(err)
	}
	if m.App != AppName {
		t.Fatalf("manifest app = %q, want %q", m.App, AppName)
	}
}

// The manifest must round-trip: the browser reads it at boot for the help panel
// and the skill reference is generated from it, so an unmarshalable field would
// break both at once.
func TestManifestMarshals(t *testing.T) {
	m, err := buildManifest(web.FS)
	if err != nil {
		t.Fatal(err)
	}
	body, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var back map[string]any
	if err := json.Unmarshal(body, &back); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	for _, key := range []string{"app", "schema", "capsHash", "types", "commands", "routes"} {
		if _, ok := back[key]; !ok {
			t.Errorf("manifest has no %q", key)
		}
	}
}

// Every declared type needs a spec file to have been read; a manifest with no
// types would still hash and still pass the stability check above.
func TestManifestHasTypes(t *testing.T) {
	m, err := buildManifest(web.FS)
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Types) < 15 {
		t.Fatalf("manifest declares %d types, want at least the 15 renderers", len(m.Types))
	}
	if len(m.Commands) != len(Commands()) {
		t.Fatalf("manifest carries %d commands, table declares %d", len(m.Commands), len(Commands()))
	}
}
