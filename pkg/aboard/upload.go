// upload.go — the human puts an image on the board.
//
//	POST /upload   an image, from a paste or a drop; returns { url }
//
// This closes the only one-way street in the design. `markup` is the board's best
// feature and it only ever worked on images an AGENT had put in assets/ — so
// "look at this" was something I could say to the human and they could not say
// back. A pasted screenshot with two circles on it is the fastest bug report
// there is.
//
// Uploads land in .board/uploads/ rather than in the embedded assets/, and that
// is not arbitrary: assets/ is compiled into the binary with //go:embed, so a
// file written there at runtime is invisible until the next build. Uploads are
// served from disk in both modes, and they are CONTENT — a markup tab references
// them by name — so they sit beside the board document rather than under run/.
//
// This is an unauthenticated server on localhost, so the write path is narrow on
// purpose: a size cap, an allow-list of image types checked against the bytes
// rather than the claimed name, and a filename the SERVER chooses. Nothing the
// caller sends is ever used as a path.
package aboard

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	uploadDir      = "uploads"
	uploadMaxBytes = 12 << 20 // 12 MiB — a screenshot, not a video
)

// Sniffed from the first bytes, so a .png that is really something else is
// refused. SVG is deliberately absent: it can carry script, and while an <img>
// context will not run it, a file the server hands back with an svg content type
// is not a risk worth taking for a paste.
var uploadKinds = []struct {
	ext   string
	mime  string
	magic []byte
}{
	{"png", "image/png", []byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A}},
	{"jpg", "image/jpeg", []byte{0xFF, 0xD8, 0xFF}},
	{"gif", "image/gif", []byte("GIF8")},
	{"webp", "image/webp", []byte("RIFF")}, // RIFF....WEBP, checked further below
}

func sniffUpload(body []byte) (ext, mime string, ok bool) {
	for _, kind := range uploadKinds {
		if len(body) < len(kind.magic) || string(body[:len(kind.magic)]) != string(kind.magic) {
			continue
		}
		if kind.ext == "webp" && !(len(body) > 12 && string(body[8:12]) == "WEBP") {
			continue
		}
		return kind.ext, kind.mime, true
	}
	return "", "", false
}

// handleUpload accepts the raw image body (what a paste or a drop gives the page)
// and answers with the URL to reference it by. The caller may suggest a label via
// ?name=, which is slugged into the filename — never used as a path.
func (s *server) handleUpload(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, uploadMaxBytes))
	if err != nil {
		writeJSON(w, http.StatusRequestEntityTooLarge, map[string]string{
			"error": fmt.Sprintf("image is larger than %d MiB", uploadMaxBytes>>20),
		})
		return
	}
	if len(body) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "empty body"})
		return
	}

	ext, mime, ok := sniffUpload(body)
	if !ok {
		writeJSON(w, http.StatusUnsupportedMediaType, map[string]string{
			"error": "not a png, jpeg, gif or webp (checked against the bytes, not the name)",
		})
		return
	}

	if err := os.MkdirAll(s.root.UploadsDir(), 0o755); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "cannot create the uploads directory"})
		return
	}

	name := fmt.Sprintf("%s-%s.%s", time.Now().UTC().Format("20060102-150405"), slugUpload(r.URL.Query().Get("name")), ext)
	if err := os.WriteFile(s.root.UploadFile(name), body, 0o644); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "cannot write the file"})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"url":   uploadDir + "/" + name,
		"bytes": len(body),
		"type":  mime,
	})
}

// slugUpload reduces a caller's label to something safe and recognisable. The
// result never contains a separator, so it cannot escape the directory however
// hostile the input.
func slugUpload(raw string) string {
	if raw == "" {
		return "pasted"
	}
	var b strings.Builder
	for _, r := range strings.ToLower(raw) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == ' ' || r == '-' || r == '_' || r == '.':
			b.WriteByte('-')
		}
		if b.Len() >= 40 {
			break
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		return "pasted"
	}
	return out
}

// serveUpload hands back a file from uploads/. Always from disk — these arrive
// after the binary was built, which is the whole reason they do not live in
// assets/. The path is rebuilt from the base name, so ".." cannot survive it.
func (s *server) serveUpload(w http.ResponseWriter, rest string) {
	name := filepath.Base(rest)
	if name == "." || name == ".." || name == "/" || name == "" {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	body, err := os.ReadFile(s.root.UploadFile(name))
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	_, mime, ok := sniffUpload(body)
	if !ok {
		// Something that is not an image got into the directory: do not serve it
		// with a guessed type.
		http.Error(w, "not an image", http.StatusUnsupportedMediaType)
		return
	}
	w.Header().Set("Content-Type", mime)
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	_, _ = w.Write(body)
}

// uploadList is what the markup view offers when it asks what is already there.
func (s *server) handleUploads(w http.ResponseWriter, _ *http.Request) {
	entries, err := os.ReadDir(s.root.UploadsDir())
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"files": []any{}})
		return
	}
	files := []map[string]any{}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		files = append(files, map[string]any{
			"url":   uploadDir + "/" + e.Name(),
			"bytes": info.Size(),
			"at":    info.ModTime().UTC().Format(time.RFC3339),
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"files": files})
}
