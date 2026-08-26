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
// Uploads land in .aboard/uploads/ rather than in the embedded assets/, and that
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
	"bytes"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
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

// Where the RIFF form tag sits: bytes 8..12, after "RIFF" and the 4-byte size.
const (
	riffFormStart = 8
	riffFormEnd   = 12
)

// maxSlugLen caps the readable half of an upload's filename. The id keeps it
// unique, so the slug only has to be recognisable in a directory listing.
const maxSlugLen = 40

func sniffUpload(body []byte) (ext, mime string, ok bool) {
	for _, kind := range uploadKinds {
		if len(body) < len(kind.magic) || !bytes.Equal(body[:len(kind.magic)], kind.magic) {
			continue
		}
		// A webp is a RIFF container: "RIFF", a 4-byte length, then "WEBP". The
		// magic table only reaches the first four bytes, so the form tag has to
		// be checked here or every RIFF file (wav, avi) would sniff as an image.
		if kind.ext == "webp" && (len(body) <= riffFormEnd || string(body[riffFormStart:riffFormEnd]) != "WEBP") {
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
		s.writeJSON(w, http.StatusRequestEntityTooLarge, map[string]string{
			wireError: fmt.Sprintf("image is larger than %d MiB", uploadMaxBytes>>20),
		})
		return
	}
	if len(body) == 0 {
		s.writeJSON(w, http.StatusBadRequest, map[string]string{wireError: "empty body"})
		return
	}

	ext, mime, ok := sniffUpload(body)
	if !ok {
		s.writeJSON(w, http.StatusUnsupportedMediaType, map[string]string{
			wireError: "not a png, jpeg, gif or webp (checked against the bytes, not the name)",
		})
		return
	}

	if err := os.MkdirAll(s.root.UploadsDir(), 0o755); err != nil {
		s.writeJSON(w, http.StatusInternalServerError, map[string]string{wireError: "cannot create the uploads directory"})
		return
	}

	name := fmt.Sprintf("%s-%s.%s", time.Now().UTC().Format("20060102-150405"), slugUpload(r.URL.Query().Get("name")), ext)
	if err := os.WriteFile(s.root.UploadFile(name), body, 0o644); err != nil { //nolint:gosec // 0o644 is the board's repo-wide file-mode policy; see the note in init.go
		s.writeJSON(w, http.StatusInternalServerError, map[string]string{wireError: "cannot write the file"})
		return
	}

	s.writeJSON(w, http.StatusOK, map[string]any{
		"url":     uploadDir + "/" + name,
		wireBytes: len(body),
		wireType:  mime,
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
		if b.Len() >= maxSlugLen {
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
		s.writeJSON(w, http.StatusOK, map[string]any{"files": []any{}})
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
			"url":     uploadDir + "/" + e.Name(),
			wireBytes: info.Size(),
			wireAt:    info.ModTime().UTC().Format(time.RFC3339),
		})
	}
	s.writeJSON(w, http.StatusOK, map[string]any{"files": files})
}

/* ---------- accounting and prune ---------- */

// UploadRow is one file in `.aboard/uploads/`: what it costs, and who names it.
//
// `Tabs` holds every tab in the PROJECT that mentions the file, qualified by
// board: a bare `bb12` is the default board's, `review:bb12` is the named board
// `review`'s. Qualified by name and never by "the board you asked about", so two
// runs of this command from two different boards print the same string for the
// same tab.
type UploadRow struct {
	Name  string   `json:"name"           yaml:"name"`
	URL   string   `json:"url"            yaml:"url"`
	Bytes int64    `json:"bytes"          yaml:"bytes"`
	At    string   `json:"at"             yaml:"at"`
	Tabs  []string `json:"tabs,omitempty" yaml:"tabs,omitempty"`
}

// Referenced reports whether any tab mentions this file.
func (u UploadRow) Referenced() bool { return len(u.Tabs) > 0 }

// UploadReport is the whole directory, with the totals a reader wants first.
// `Orphaned` counts the files no tab mentions and `OrphanedBytes` is what pruning
// them would reclaim.
type UploadReport struct {
	Dir   string      `json:"dir"   yaml:"dir"`
	Files []UploadRow `json:"files" yaml:"files"`
	Bytes int64       `json:"bytes" yaml:"bytes"`
	// Boards are the documents that were scanned for references, by file name.
	// Part of the answer rather than decoration: "no tab mentions it" is a claim
	// about a set of documents, and this is the set — so a reader about to delete
	// something can see whether the board they are thinking of was in it.
	Boards        []string `json:"boards"        yaml:"boards"`
	Orphaned      int      `json:"orphaned"      yaml:"orphaned"`
	OrphanedBytes int64    `json:"orphanedBytes" yaml:"orphanedBytes"`
}

// Uploads lists every file under `.aboard/uploads/` and the tabs that mention it.
//
// The reference scan reads each tab's RAW state text, not its declared fields,
// and that is the whole correctness of it: an `html` widget's markup can name an
// upload in a string no spec knows about, and a scan over declared fields would
// report that file as an orphan and offer to delete an image the human is
// looking at. So the question asked is the crude one — does this tab's JSON
// contain this filename — and the failure mode is a file kept that could have
// gone, which is the right way round for a delete path.
//
// EVERY board in the project is scanned, which is why this takes no board name.
// `uploads/` is shared by all of them on purpose — an image is content a human
// pasted, and either board may put it on a tab — so accounting for it from ONE
// board's tabs asked a question narrower than the directory it was answering
// about: an image referenced only by the review board came back "no tab mentions
// it" from the default board, and `--prune --yes` deleted a picture somebody was
// looking at.
//
// Reads the state files directly: no server needed, exactly as `export` and
// `journal` do not need one. A document that will not parse is a hard error and
// not a skipped file — a board whose references cannot be read is a board that
// might be referencing everything, and the next thing the caller does with this
// report may be a deletion.
func Uploads(root Root) (UploadReport, error) {
	rep := UploadReport{Dir: root.UploadsDir(), Files: []UploadRow{}, Boards: []string{}}

	boards, err := root.StateFiles()
	if err != nil {
		return rep, err
	}
	if len(boards) == 0 {
		return rep, fmt.Errorf("no board document in %s — run `aboard init`", root.Dir())
	}
	docs := make([]boardDoc, 0, len(boards))
	for _, b := range boards {
		raw, err := os.ReadFile(b.Path)
		if err != nil {
			return rep, fmt.Errorf("reading %s: %w", b.Path, err)
		}
		doc, err := decodeDocument(raw)
		if err != nil {
			return rep, fmt.Errorf("%s does not parse: %w", b.Path, err)
		}
		docs = append(docs, boardDoc{name: b.Name, doc: doc})
		rep.Boards = append(rep.Boards, filepath.Base(b.Path))
	}

	entries, err := os.ReadDir(root.UploadsDir())
	if err != nil {
		// A board that has never had an image pasted into it has no directory,
		// and that is an empty list rather than a failure.
		if os.IsNotExist(err) {
			return rep, nil
		}
		return rep, fmt.Errorf("reading %s: %w", root.UploadsDir(), err)
	}

	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		row := UploadRow{
			Name:  e.Name(),
			URL:   uploadDir + "/" + e.Name(),
			Bytes: info.Size(),
			At:    info.ModTime().UTC().Format(time.RFC3339),
			Tabs:  tabsMentioning(docs, e.Name()),
		}
		rep.Bytes += row.Bytes
		if !row.Referenced() {
			rep.Orphaned++
			rep.OrphanedBytes += row.Bytes
		}
		rep.Files = append(rep.Files, row)
	}
	sort.Slice(rep.Files, func(i, j int) bool { return rep.Files[i].Name < rep.Files[j].Name })
	return rep, nil
}

// boardDoc is one board's parsed document with the name it answers to, so a
// reference can say WHICH board holds the tab it found.
type boardDoc struct {
	name string
	doc  *stateDoc
}

// tabsMentioning finds every tab, on any board in the project, whose raw text
// contains a filename.
//
// The tab's `name` and `note` are searched along with its state, because a tab
// can perfectly well be the record of an image by naming it — and a scan that
// missed that would call the file an orphan.
func tabsMentioning(boards []boardDoc, file string) []string {
	needle := []byte(file)
	out := []string{}
	for _, b := range boards {
		for i := range b.doc.tabs {
			t := &b.doc.tabs[i]
			if bytes.Contains(t.State, needle) ||
				strings.Contains(t.Name, file) ||
				strings.Contains(t.Note, file) {
				out = append(out, qualifiedTab(b.name, t.ID))
			}
		}
	}
	return out
}

// qualifiedTab names a tab across the whole project. Tab ids are allocated PER
// BOARD, so both documents in a two-board project have a `bb1` and they are
// different tabs; an unqualified id in a project-wide listing is therefore not an
// answer. The default board's ids stay bare, so a one-board project — which is
// almost every project — prints exactly what it printed before.
func qualifiedTab(board, id string) string {
	if board == "" {
		return id
	}
	return board + ":" + id
}

// PruneUploads deletes the files no tab mentions. It NEVER decides on its own:
// the caller has already printed the list and the human has already said yes.
//
// Returns what it removed and the first error, having tried them all — a
// permission wall on one file is not a reason to leave the other nine.
func PruneUploads(root Root, rows []UploadRow) ([]string, error) {
	removed := []string{}
	var firstErr error
	for _, row := range rows {
		if row.Referenced() {
			continue
		}
		// Rebuilt from the base name, exactly as serveUpload does: nothing that
		// arrived from anywhere is ever used as a path.
		if err := os.Remove(root.UploadFile(filepath.Base(row.Name))); err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		removed = append(removed, row.Name)
	}
	return removed, firstErr
}

// Human is the listing, with the orphans marked and the totals last.
func (r UploadReport) Human(prune bool) string {
	var b strings.Builder
	if len(r.Files) == 0 {
		fmt.Fprintf(&b, "no uploads in %s\n", r.Dir)
		return b.String()
	}
	for _, f := range r.Files {
		mark := " "
		who := joinOr(f.Tabs, "")
		if !f.Referenced() {
			mark = "*"
			who = "no tab mentions it"
		}
		fmt.Fprintf(&b, "%s %-44s %9s  %s\n", mark, f.Name, humanBytes(f.Bytes), who)
	}
	fmt.Fprintf(&b, "\n%d file%s, %s in %s\n", len(r.Files), plural(len(r.Files)), humanBytes(r.Bytes), r.Dir)
	// Which documents the references were looked for in. `uploads/` belongs to
	// the PROJECT, so "no tab mentions it" is a claim about every board in it,
	// and on a project with two boards the reader has to be able to see that both
	// were read — and that a `review:bb1` in the listing is not a tab of theirs.
	if len(r.Boards) > 1 {
		fmt.Fprintf(&b, "references scanned in %d board documents: %s  (a tab id is prefixed with its board name)\n",
			len(r.Boards), strings.Join(r.Boards, ", "))
	}
	if r.Orphaned == 0 {
		b.WriteString("every upload is mentioned by a tab — nothing to prune\n")
		return b.String()
	}
	fmt.Fprintf(&b, "* %d unreferenced, %s\n", r.Orphaned, humanBytes(r.OrphanedBytes))
	if !prune {
		b.WriteString("run `aboard uploads --prune` to see what removing them would do\n")
	}
	// The scan is textual, and a reader about to delete files has to know on what
	// evidence. Said here rather than only in the docs, for the same reason the
	// mount receipts print their own limits.
	b.WriteString("(\"mentioned\" is a text search of each tab's raw state, name and note — an html widget's\n" +
		" markup counts, and a file named nowhere in the document is what unreferenced means)\n")
	return b.String()
}

// humanBytes is a size a person reads at a glance. Deliberately crude — this is
// a listing of screenshots, and the units stop at MB.
func humanBytes(n int64) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.1f kB", float64(n)/(1<<10))
	default:
		return fmt.Sprintf("%d B", n)
	}
}
