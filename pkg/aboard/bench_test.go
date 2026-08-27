package aboard

// bench_test.go — what a write, a read and a watcher tick cost as a board grows.
//
// Every cost on the write path used to be proportional to the WHOLE document
// rather than to the edit — seven full-document unmarshals per POST, a
// re-marshal of every tab's state to compare it, a recursive id walk over
// everything twice, and a sha256 of the whole file every 200 ms at idle. None of
// that is measurable on a 65 KB board, which is why it survived; the only honest
// way to talk about it is to synthesise a board that is big enough for the shape
// to show and then read the numbers off.
//
// So this file is the instrument, not a test: it synthesises N tabs with mixed
// state sizes (a few 1 MB html states, the rest small notes), and times the three
// operations that matter — one POST that edits ONE small tab, one GET, one watcher
// tick. The numbers it produced, and what they mean, are written up in
// docs/explanation/how-aboard-runs.md under "What a write costs". Run it with:
//
//	go test -run xxx -bench . -benchmem ./pkg/aboard/
//
// The number that matters is not the absolute ns/op, which is this machine's;
// it is how the POST scales with N. Work proportional to the edit stays flat as
// N grows, and work proportional to the document does not.

import (
	"bytes"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/exoport/aboard/pkg/aboard/web"
)

// The three board sizes: today's boards, a big one, and one nobody
// has yet — 15 is the example board, 5 000 is the size at which a cost
// proportional to the document is impossible to miss.
var benchSizes = []int{15, 500, 5000}

// benchBigState is how large one "a human pasted a widget in" tab is. html and
// markup states really do reach this; they are the reason a whole-document cost
// is not theoretical.
const benchBigState = 1 << 20

// benchBigTabs is how many of the N tabs carry one, and it is a CONSTANT rather
// than a fraction of N. That is what makes the three rows comparable: a board of
// 15 and a board of 500 then differ by 485 unchanged small tabs and almost no
// bytes, so "does the POST scale with the number of unchanged tabs" is a
// question the table can actually answer. Scaling the big tabs with N instead
// measured document size wearing the label of tab count.
const benchBigTabs = 3

// benchDoc writes a board of n tabs, big of which carry a ~1 MB state, as raw
// JSON. Hand-built rather than marshalled from structs so the shape is visible
// here and so the two patch points below sit at a fixed width.
//
// The LAST tab is the one the POST benchmark edits: a small notes tab whose text
// carries a fixed-width counter, so one iteration differs from the next by ten
// bytes in place and the benchmark measures the server rather than its own
// document builder.
func benchDoc(n int) []byte {
	var b strings.Builder
	filler := "the quick brown fox jumps over the lazy dog."
	bigFiller := strings.Repeat("x", benchBigState)

	b.WriteString(`{"version":3,"rev":1,"nextId":`)
	fmt.Fprintf(&b, "%d", n+1000)
	b.WriteString(`,"updatedAt":"2026-08-26T00:00:00.000Z","lastEditedBy":"agent-bench","tabs":[`)
	for i := 1; i <= n; i++ {
		if i > 1 {
			b.WriteString(",")
		}
		switch {
		case i == n:
			// The edited tab. "iteration " appears nowhere else in the document.
			fmt.Fprintf(&b, `{"id":"bb%d","name":"Scratch","type":"notes","state":{"text":"iteration 0000000000"}}`, i)
		case i <= benchBigTabs:
			fmt.Fprintf(&b, `{"id":"bb%d","name":"Widget %d","type":"html","state":{"html":"%s"}}`, i, i, bigFiller)
		default:
			fmt.Fprintf(&b, `{"id":"bb%d","name":"Tab %d","type":"notes","state":{"text":"%s"}}`, i, i, filler)
		}
	}
	b.WriteString(`]}`)
	return []byte(b.String())
}

// benchBody is benchDoc plus the three envelope fields a real POST carries. The
// `__base` is fixed-width so the benchmark can patch the revision in place.
func benchBody(n int) []byte {
	doc := benchDoc(n)
	head := []byte(`{"__origin":"bench","__by":"agent-bench","__base":"0000000001",`)
	return append(head, doc[1:]...)
}

// benchServer is testServer without the *testing.T — a state file in a temp
// directory and the maps the write path touches.
func benchServer(b *testing.B, doc []byte) *server {
	b.Helper()
	dir := b.TempDir()
	root := Root(dir)
	if err := os.MkdirAll(root.RunDir(), 0o755); err != nil {
		b.Fatal(err)
	}
	state := root.StateFile("")
	if err := os.WriteFile(state, doc, 0o644); err != nil {
		b.Fatal(err)
	}
	return &server{
		opts:      Options{Logger: log.New(io.Discard, "", 0)},
		root:      root,
		assets:    web.FS,
		stateFile: state,
		clients:   map[chan string]struct{}{},
		watchers:  map[chan string]struct{}{},
		waits:     newWaitHub(),
		ui:        newUIWatcher(false),
		journal:   newJournal(root, ""),
	}
}

// benchTenMiB is the idle-cost case: a board big enough that
// hashing all of it five times a second is real disk and CPU spent on nothing.
func benchTenMiB() []byte {
	doc := benchDoc(5000)
	pad := 10<<20 - len(doc)
	if pad <= 0 {
		return doc
	}
	// One more oversized tab, appended before the closing `]}`.
	tail := fmt.Sprintf(`,{"id":"bb90001","name":"Ballast","type":"html","state":{"html":%q}}]}`, strings.Repeat("x", pad))
	return append(doc[:len(doc)-2], tail...)
}

// putDigits patches a fixed-width decimal in place, so preparing an iteration
// costs ten byte stores rather than rebuilding a multi-megabyte body.
func putDigits(dst []byte, v int) {
	for i := len(dst) - 1; i >= 0; i-- {
		dst[i] = byte('0' + v%10)
		v /= 10
	}
}

// nullResponse is a ResponseWriter that keeps the status and throws the body
// away. httptest.NewRecorder would buffer a 4 MB GET into the measurement.
type nullResponse struct {
	head http.Header
	code int
	n    int
}

func (r *nullResponse) Header() http.Header {
	if r.head == nil {
		r.head = http.Header{}
	}
	return r.head
}

func (r *nullResponse) Write(p []byte) (int, error) { r.n += len(p); return len(p), nil }
func (r *nullResponse) WriteHeader(code int)        { r.code = code }

// BenchmarkPostOneSmallTab is the headline: one agent edits one small tab on a
// board of N. Nothing about that edit involves the other N-1 tabs, so the time
// per write is the measure of how much whole-document work the write path does.
func BenchmarkPostOneSmallTab(b *testing.B) {
	for _, n := range benchSizes {
		b.Run(fmt.Sprintf("tabs=%d", n), func(b *testing.B) {
			srv := benchServer(b, benchDoc(n))
			body := benchBody(n)
			baseAt := bytes.Index(body, []byte(`"__base":"`)) + len(`"__base":"`)
			iterAt := bytes.Index(body, []byte(`"iteration `)) + len(`"iteration `)
			if baseAt < 10 || iterAt < 10 {
				b.Fatal("the benchmark body lost one of its patch points")
			}

			rev := 1
			iter := 0
			b.SetBytes(int64(len(body)))
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				putDigits(body[baseAt:baseAt+10], rev)
				putDigits(body[iterAt:iterAt+10], iter)
				w := &nullResponse{}
				req, err := http.NewRequest(http.MethodPost, "http://localhost/aboard.json", bytes.NewReader(body))
				if err != nil {
					b.Fatal(err)
				}
				req.ContentLength = int64(len(body))
				srv.postState(w, req)
				if w.code != http.StatusOK {
					b.Fatalf("POST returned %d on iteration %d", w.code, iter)
				}
				rev++
				iter++
			}
		})
	}
}

// BenchmarkGetState goes through route(), not the handler, so it keeps measuring
// the same thing when the handler's signature moves.
func BenchmarkGetState(b *testing.B) {
	for _, n := range benchSizes {
		b.Run(fmt.Sprintf("tabs=%d", n), func(b *testing.B) {
			doc := benchDoc(n)
			srv := benchServer(b, doc)

			b.SetBytes(int64(len(doc)))
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				w := &nullResponse{}
				req, err := http.NewRequest(http.MethodGet, "http://localhost/aboard.json", http.NoBody)
				if err != nil {
					b.Fatal(err)
				}
				srv.route(w, req)
				if w.code != 0 && w.code != http.StatusOK {
					b.Fatalf("GET returned %d", w.code)
				}
			}
		})
	}
}

// BenchmarkWatcherTick is the idle cost: what the poll loop spends every 200 ms
// on a board nobody is writing to. The 10 MiB case is the one the write-up names
// — at 5 ticks a second, a whole-file sha256 there is sustained disk and CPU for
// nothing at all.
func BenchmarkWatcherTick(b *testing.B) {
	cases := make([]struct {
		name string
		doc  []byte
	}, 0, len(benchSizes)+1)
	for _, n := range benchSizes {
		cases = append(cases, struct {
			name string
			doc  []byte
		}{fmt.Sprintf("tabs=%d", n), benchDoc(n)})
	}
	cases = append(cases, struct {
		name string
		doc  []byte
	}{"bytes=10MiB", benchTenMiB()})

	for _, tc := range cases {
		b.Run(tc.name, func(b *testing.B) {
			srv := benchServer(b, tc.doc)
			b.SetBytes(int64(len(tc.doc)))
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				if srv.stateSignature() == "" {
					b.Fatal("the watcher tick could not read the state file")
				}
			}
		})
	}
}
