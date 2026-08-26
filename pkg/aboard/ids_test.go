package aboard

import "testing"

// reconcileNextID is the safety net compare-and-set cannot provide: CAS refuses
// a second writer who picked the same number, but nothing refuses a document
// whose counter has fallen BEHIND the ids already in it — a hand edit, a restored
// tab, an agent that read an old copy. Every row here is a way that happens, and
// every row fails if the function is reduced to "return the incoming nextId"
// (verified once by gutting it).
func TestReconcileNextID(t *testing.T) {
	cases := []struct {
		name     string
		incoming string
		current  string
		want     int
	}{
		{
			name:     "the floor is 1 for two empty documents",
			incoming: `{}`,
			current:  `{}`,
			want:     1,
		},
		{
			name:     "an empty board with no counter still allocates from 1",
			incoming: `{"tabs":[]}`,
			current:  `{"tabs":[]}`,
			want:     1,
		},
		{
			name:     "a counter of zero is raised to the floor",
			incoming: `{"nextId":0,"tabs":[]}`,
			current:  `{"nextId":0,"tabs":[]}`,
			want:     1,
		},
		{
			name:     "it never goes backwards: an agent reusing a lower nextId",
			incoming: `{"nextId":9,"tabs":[{"id":"bb7"}]}`,
			current:  `{"nextId":42,"tabs":[{"id":"bb7"}]}`,
			want:     42,
		},
		{
			name:     "an id above both counters raises it past that id",
			incoming: `{"nextId":3,"tabs":[{"id":"bb99"}]}`,
			current:  `{"nextId":3,"tabs":[]}`,
			want:     100,
		},
		{
			name:     "an id present only in the CURRENT document still counts",
			incoming: `{"nextId":3,"tabs":[]}`,
			current:  `{"nextId":3,"tabs":[{"id":"bb160"}]}`,
			want:     161,
		},
		{
			name:     "ids nested deep inside a tab's state count too",
			incoming: `{"nextId":2,"tabs":[{"id":"bb1","state":{"nodes":[{"id":"bb77","marks":[{"id":"bb203"}]}]}}]}`,
			current:  `{"nextId":2,"tabs":[]}`,
			want:     204,
		},
		{
			name:     "a stack block's own blocks are walked as well",
			incoming: `{"nextId":1,"tabs":[{"id":"bb1","state":{"blocks":[{"id":"bb310","state":{"rows":[{"id":"bb311"}]}}]}}]}`,
			current:  `{}`,
			want:     312,
		},
		{
			name:     "legacy bare and n-prefixed ids are read, not ignored",
			incoming: `{"nextId":1,"tabs":[{"id":"49"},{"id":"n120"}]}`,
			current:  `{}`,
			want:     121,
		},
		{
			// Two negatives at once: a semantic field id must not raise the
			// counter, and one carrying digits that are not its whole tail
			// ("window-2024") must not be mis-read as id 2024 by an unanchored
			// scan. The answer therefore has to come from the current document,
			// which is also what makes the row fail on a gutted function.
			name:     "a semantic form field id does not raise the counter",
			incoming: `{"nextId":5,"tabs":[{"id":"bb2","state":{"fields":[{"id":"strategy"},{"id":"window-2024"}]}}]}`,
			current:  `{"nextId":6}`,
			want:     6,
		},
		{
			name:     "a nextId recorded as a string is still a counter",
			incoming: `{"nextId":3,"tabs":[]}`,
			current:  `{"nextId":"77","tabs":[]}`,
			want:     77,
		},
		{
			name:     "an unparseable current document does not lose the incoming ids",
			incoming: `{"nextId":31,"tabs":[{"id":"bb44"}]}`,
			current:  `{not json`,
			want:     45,
		},
		{
			name:     "a restored tab in the current document outranks the incoming counter",
			incoming: `{"nextId":12,"tabs":[]}`,
			current:  `{"nextId":12,"tabs":[{"id":"bb11","state":{"nodes":[{"id":"bb400"}]}}]}`,
			want:     401,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := reconcileNextID([]byte(tc.incoming), []byte(tc.current))
			if got != tc.want {
				t.Errorf("reconcileNextID(%s, %s) = %d, want %d", tc.incoming, tc.current, got, tc.want)
			}
		})
	}
}

// The empty-slice case is separate because it is the one the caller actually
// produces on a first write: there is no current document at all.
func TestReconcileNextIDWithNoCurrentDocument(t *testing.T) {
	if got := reconcileNextID([]byte(`{"nextId":4,"tabs":[{"id":"bb9"}]}`), nil); got != 10 {
		t.Errorf("got %d, want 10", got)
	}
	if got := reconcileNextID(nil, nil); got != 1 {
		t.Errorf("got %d, want 1 (the floor)", got)
	}
}

// idCounter's tolerance is the reason no renderer needed changing when ids gained
// their "bb" tag, so it is pinned rather than left to the table above.
func TestIDCounterReadsTheNumericTail(t *testing.T) {
	cases := map[any]int{
		"bb147":     147,
		"147":       147,
		"n147":      147,
		"strategy":  0,
		"bb":        0,
		"bb12a":     0,
		"BB12":      0, // the pattern is lower-case by design
		float64(12): 0, // not a string: somebody else's data
		nil:         0,
	}
	for in, want := range cases {
		if got := idCounter(in); got != want {
			t.Errorf("idCounter(%#v) = %d, want %d", in, got, want)
		}
	}
}
