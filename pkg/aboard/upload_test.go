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
