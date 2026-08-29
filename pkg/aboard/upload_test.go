package aboard

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"encoding/json"
	"hash/crc32"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

// Uploads were covered only by test/smoke.sh, which is being retired — and
// "covered by the suite that never runs in CI" is how the sniffing rule below
// could have been relaxed without anything noticing.
//
// The rule: the type is decided by the BYTES, never by the Content-Type the
// caller sends. This server has no authentication, so a caller who could name
// the type could store a .html at a URL the board serves back on its own origin.

func TestAnUploadLandsInUploadsAndServesAsAnImage(t *testing.T) {
	srv := testServer(t, twoTabs)

	req := httptest.NewRequest(http.MethodPost, "http://localhost/upload?name=smoke%20probe", bytes.NewReader(tinyPNG()))
	req.Header.Set("Content-Type", "image/png")
	rec := httptest.NewRecorder()
	srv.route(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /upload = %d: %s", rec.Code, strings.TrimSpace(rec.Body.String()))
	}

	var body struct {
		URL string `json:"url"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("unreadable reply: %v", err)
	}
	// uploads/, not assets/. assets/ is compiled into the binary; a human's
	// pasted screenshot is that project's content and belongs beside its board.
	if !strings.HasPrefix(body.URL, "uploads/") {
		t.Fatalf("the upload was stored at %q, want it under uploads/", body.URL)
	}

	get := httptest.NewRequest(http.MethodGet, "http://localhost/"+body.URL, http.NoBody)
	getRec := httptest.NewRecorder()
	srv.route(getRec, get)
	if getRec.Code != http.StatusOK {
		t.Fatalf("GET /%s = %d", body.URL, getRec.Code)
	}
	if ct := getRec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "image/") {
		t.Errorf("the upload is served as %q, not as an image", ct)
	}
}

func TestANonImageUploadIsRefused(t *testing.T) {
	srv := testServer(t, twoTabs)

	// The lie is the point: a caller naming a type it is not.
	req := httptest.NewRequest(http.MethodPost, "http://localhost/upload",
		strings.NewReader("plain text, not an image"))
	req.Header.Set("Content-Type", "image/png")
	rec := httptest.NewRecorder()
	srv.route(rec, req)
	if rec.Code != http.StatusUnsupportedMediaType {
		t.Errorf("POST /upload with non-image bytes = %d, want 415", rec.Code)
	}
}

// The uploads directory is served from disk, which is the one place in this
// server where a URL becomes a filename.
func TestAnEncodedTraversalUnderUploadsIsRefused(t *testing.T) {
	srv := testServer(t, twoTabs)

	// httptest keeps RawPath, which is what a traversal needs to survive to the
	// handler at all — a pre-decoded path would be a different test.
	req := httptest.NewRequest(http.MethodGet, "http://localhost/uploads/%2e%2e%2faboard.json", http.NoBody)
	rec := httptest.NewRecorder()
	srv.route(rec, req)
	if rec.Code == http.StatusOK {
		t.Fatalf("a traversal out of uploads/ was served: %s", rec.Body.String())
	}
	if body, err := os.ReadFile(srv.stateFile); err == nil && bytes.Contains(rec.Body.Bytes(), body) {
		t.Error("the board document came back through the uploads route")
	}
}

// tinyPNG builds a valid 4×4 PNG. Built rather than committed as testdata
// because the bytes are what is being tested — a fixture file could be replaced
// with something that is not a PNG and the sniffing test would still pass while
// asserting nothing.
func tinyPNG() []byte {
	chunk := func(kind string, data []byte) []byte {
		out := make([]byte, 0, len(data)+12)
		out = binary.BigEndian.AppendUint32(out, uint32(len(data)))
		payload := append([]byte(kind), data...)
		out = append(out, payload...)
		return binary.BigEndian.AppendUint32(out, crc32.ChecksumIEEE(payload))
	}

	ihdr := make([]byte, 0, 13)
	ihdr = binary.BigEndian.AppendUint32(ihdr, 4) // width
	ihdr = binary.BigEndian.AppendUint32(ihdr, 4) // height
	ihdr = append(ihdr, 8, 2, 0, 0, 0)            // 8-bit truecolour, no interlace

	var raw bytes.Buffer
	for range 4 {
		raw.WriteByte(0) // filter: none
		raw.Write(bytes.Repeat([]byte{0x78}, 12))
	}
	var idat bytes.Buffer
	zw := zlib.NewWriter(&idat)
	_, _ = zw.Write(raw.Bytes())
	_ = zw.Close()

	out := []byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a}
	out = append(out, chunk("IHDR", ihdr)...)
	out = append(out, chunk("IDAT", idat.Bytes())...)
	return append(out, chunk("IEND", nil)...)
}

/* ---------- accounting and prune ---------- */

const uploadBoard = `{
  "version": 1,
  "rev": 1,
  "nextId": 9,
  "tabs": [
    {"id": "ab1", "name": "Screen", "type": "markup",
     "state": {"images": [{"src": "uploads/kept.png"}]}},
    {"id": "ab2", "name": "Widget", "type": "html",
     "state": {"html": "<img src=\"uploads/in-a-widget.png\">"}}
  ]
}`

func seedUploads(t *testing.T, root Root, names ...string) {
	t.Helper()
	if err := os.MkdirAll(root.UploadsDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range names {
		if err := os.WriteFile(root.UploadFile(name), []byte("not really a png"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func uploadBoardRoot(t *testing.T) Root {
	t.Helper()
	root := Root(t.TempDir())
	if err := os.MkdirAll(root.Dir(), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(root.StateFile(""), []byte(uploadBoard), 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}

// The reference scan reads each tab's RAW state text and not its declared
// fields, and that is the whole correctness of it: an html widget's markup can
// name a file no spec knows about, and a scan over declared fields would call
// that image an orphan and offer to delete something the human is looking at.
func TestUploadsFindsAFileNamedOnlyInAWidgetsMarkup(t *testing.T) {
	root := uploadBoardRoot(t)
	seedUploads(t, root, "kept.png", "in-a-widget.png", "orphan.png")

	rep, err := Uploads(root, DefaultInvocation)
	if err != nil {
		t.Fatal(err)
	}
	byName := map[string]UploadRow{}
	for _, f := range rep.Files {
		byName[f.Name] = f
	}
	if got := byName["in-a-widget.png"].Tabs; len(got) != 1 || got[0] != "ab2" {
		t.Errorf("a file named only inside an html widget must be found: %v", got)
	}
	if got := byName["kept.png"].Tabs; len(got) != 1 || got[0] != "ab1" {
		t.Errorf("kept.png: %v", got)
	}
	if byName["orphan.png"].Referenced() {
		t.Errorf("orphan.png is mentioned nowhere: %+v", byName["orphan.png"])
	}
	if rep.Orphaned != 1 {
		t.Errorf("orphaned = %d, want 1", rep.Orphaned)
	}
}

// Deletion is irreversible and `.aboard/` is gitignored, so there is no copy
// anywhere to go back to: pruning removes the unreferenced files and NOTHING
// else, whatever else is in the directory.
func TestPruneRemovesOnlyTheUnreferencedFiles(t *testing.T) {
	root := uploadBoardRoot(t)
	seedUploads(t, root, "kept.png", "in-a-widget.png", "orphan.png")

	rep, err := Uploads(root, DefaultInvocation)
	if err != nil {
		t.Fatal(err)
	}
	removed, err := PruneUploads(root, rep.Files)
	if err != nil {
		t.Fatal(err)
	}
	if len(removed) != 1 || removed[0] != "orphan.png" {
		t.Fatalf("removed %v, want just orphan.png", removed)
	}
	for _, keep := range []string{"kept.png", "in-a-widget.png"} {
		if _, err := os.Stat(root.UploadFile(keep)); err != nil {
			t.Errorf("%s was deleted and is referenced: %v", keep, err)
		}
	}
	if _, err := os.Stat(root.UploadFile("orphan.png")); !os.IsNotExist(err) {
		t.Errorf("orphan.png survived the prune: %v", err)
	}
}

// A board with no uploads directory at all is an empty list, not a failure: an
// image has simply never been pasted into it.
func TestUploadsOnABoardThatHasNeverHadOne(t *testing.T) {
	root := uploadBoardRoot(t)
	rep, err := Uploads(root, DefaultInvocation)
	if err != nil {
		t.Fatalf("a board with no uploads directory must not be an error: %v", err)
	}
	if len(rep.Files) != 0 || rep.Orphaned != 0 {
		t.Errorf("unexpected report: %+v", rep)
	}
}

// The listing has to say on what evidence it calls a file unreferenced, because
// the next thing the reader does is delete things.
func TestTheUploadsListingSaysHowItDecided(t *testing.T) {
	root := uploadBoardRoot(t)
	seedUploads(t, root, "orphan.png")
	rep, err := Uploads(root, DefaultInvocation)
	if err != nil {
		t.Fatal(err)
	}
	got := rep.Human(false, DefaultInvocation)
	if !strings.Contains(got, "text search") {
		t.Errorf("the listing must say the scan is textual:\n%s", got)
	}
	if !strings.Contains(got, "* 1 unreferenced") {
		t.Errorf("the totals must count the orphans:\n%s", got)
	}
}

// `uploads/` belongs to the PROJECT and every board in it shares the directory,
// so the accounting has to read every board's tabs. It read ONE board's, so an
// image referenced only by the review board came back "no tab mentions it" from
// the default board — and `--prune --yes` deleted a picture somebody was looking
// at. Measured, and the delete is irreversible: `.aboard/` is gitignored, so
// there is no copy anywhere to go back to.
func TestUploadsSeesEveryBoardInTheProject(t *testing.T) {
	root := uploadBoardRoot(t)
	const reviewBoard = `{"version":1,"rev":1,"nextId":2,"tabs":[
	  {"id":"ab1","name":"Side note","type":"markup",
	   "state":{"images":[{"src":"uploads/only-on-review.png"}]}}]}`
	if err := os.WriteFile(root.StateFile("review"), []byte(reviewBoard), 0o644); err != nil {
		t.Fatal(err)
	}
	seedUploads(t, root, "kept.png", "in-a-widget.png", "only-on-review.png", "orphan.png")

	rep, err := Uploads(root, DefaultInvocation)
	if err != nil {
		t.Fatal(err)
	}
	byName := map[string]UploadRow{}
	for _, f := range rep.Files {
		byName[f.Name] = f
	}
	row := byName["only-on-review.png"]
	if !row.Referenced() {
		t.Fatalf("an image referenced only by the named board is not an orphan: %+v", row)
	}
	// Qualified, because tab ids are allocated per board: both documents have a
	// ab1 and they are different tabs, so a bare id in a project-wide listing is
	// not an answer.
	if len(row.Tabs) != 1 || row.Tabs[0] != "review:ab1" {
		t.Errorf("the reference must name the board that holds it: %v", row.Tabs)
	}
	if got := byName["kept.png"].Tabs; len(got) != 1 || got[0] != "ab1" {
		t.Errorf("the default board's ids stay bare: %v", got)
	}
	if rep.Orphaned != 1 || !strings.Contains(rep.Human(true, DefaultInvocation), "aboard.review.json") {
		t.Errorf("orphaned = %d, and the listing must say which documents it scanned:\n%s", rep.Orphaned, rep.Human(true, DefaultInvocation))
	}

	// And the prune agrees with the listing, which is the half that deletes.
	removed, err := PruneUploads(root, rep.Files)
	if err != nil {
		t.Fatal(err)
	}
	if len(removed) != 1 || removed[0] != "orphan.png" {
		t.Fatalf("removed %v, want just orphan.png", removed)
	}
	if _, err := os.Stat(root.UploadFile("only-on-review.png")); err != nil {
		t.Errorf("the named board's image was deleted: %v", err)
	}
}
