package aboard

import (
	"strings"
	"testing"
)

// One minimal document per detector class.
//
// The review found that five of the six detectors could each be deleted with
// `go test ./...` green: the only coverage was TestExampleBoardWarnsOnlyAbout-
// ItsDemonstration, which counts warnings on the example board and therefore
// only ever exercised the unknown-component case. A count assertion on a fixture
// is not a test of a detector; it is a test that the fixture has not changed.
//
// Each case names the substring that identifies ITS detector, so disabling one
// fails one case rather than shifting a total everyone then re-baselines.
func TestWriteWarningsPerDetector(t *testing.T) {
	cases := []struct {
		name string
		doc  string
		want string // a substring only this detector produces
	}{
		{
			name: "unknown component",
			doc: `{"tabs":[{"id":"bb1","type":"ui","state":{
				"root":{"type":"col","children":[{"type":"sparkline","values":[1,2]}]}}}]}`,
			want: `bb1 (ui): root.children[0].type = "sparkline" is not in the catalog`,
		},
		{
			name: "unknown prop on a known component",
			doc: `{"tabs":[{"id":"bb1","type":"ui","state":{
				"root":{"type":"stat","value":"3","caption":"widgets"}}}]}`,
			want: `bb1 (ui): root is a stat, which does not read "caption"`,
		},
		{
			name: "undeclared state key",
			doc:  `{"tabs":[{"id":"bb1","type":"kanban","state":{"nodes":[],"colums":[]}}]}`,
			want: `bb1 (kanban): state.colums is not declared by the kanban renderer`,
		},
		{
			name: "bad block field",
			doc: `{"tabs":[{"id":"bb1","type":"stack","state":{"blocks":[
				{"id":"bb2","type":"notes","tittle":"Notes","state":{}}]}}]}`,
			want: `bb1/bb2 (stack block): tittle is not a block field`,
		},
		{
			name: "dead bind",
			doc: `{"tabs":[{"id":"bb1","type":"ui","state":{
				"data":{"demo":{"n":null}},
				"root":{"type":"text","value":{"bind":"demo.missing"}}}}]}`,
			want: `bb1 (ui): root.value binds to data.demo.missing, which is not in state.data`,
		},
		{
			name: "unknown ui tone",
			doc: `{"tabs":[{"id":"bb1","type":"ui","state":{
				"root":{"type":"text","value":"hi","tone":"claude"}}}]}`,
			want: `bb1: root.tone = "claude" is not a tone this board has`,
		},
		{
			name: "unknown markup colour",
			doc: `{"tabs":[{"id":"bb1","type":"markup","state":{"images":[
				{"id":"bb2","marks":[{"id":"bb3","color":"claude"}]}]}}]}`,
			want: `bb1: bb3.color = "claude" is not a colour this board has`,
		},
		{
			name: "wrong item shape inside a fixed-shape array prop",
			doc: `{"tabs":[{"id":"bb1","type":"ui","state":{
				"root":{"type":"kv","pairs":[{"k":"rev","v":"41"}]}}}]}`,
			want: `bb1 (ui): root.pairs[0].k is not read — a kv pairs item is { key, value }`,
		},
		{
			name: "a ui tree nested inside a stack block is checked too",
			doc: `{"tabs":[{"id":"bb1","type":"stack","state":{"blocks":[
				{"id":"bb2","type":"ui","state":{"root":{"type":"stat","value":"3","caption":"x"}}}]}}]}`,
			want: `bb1/bb2 (ui): root is a stat, which does not read "caption"`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := writeWarnings(WebFS(), []byte(tc.doc))
			if !containsSubstring(got, tc.want) {
				t.Errorf("no warning containing %q\ngot %d warnings:\n%s",
					tc.want, len(got), strings.Join(got, "\n"))
			}
		})
	}
}

// The two negatives. A checker that calls correct state a mistake is worse than
// no checker: it is the noise that teaches people to skip stderr, which is where
// the real warnings are. Both of these were real false positives once.
func TestWriteWarningsStaysQuietOnCorrectDocuments(t *testing.T) {
	cases := []struct {
		name string
		doc  string
	}{
		{
			// `field.bind` is a plain string naming where to WRITE. It is not
			// required to exist yet — that is the whole point of an empty form.
			name: "a field's write path is not a dead read",
			doc: `{"tabs":[{"id":"bb1","type":"ui","state":{
				"data":{},
				"root":{"type":"field","label":"Name","bind":"demo.name","field":"text"}}}]}`,
		},
		{
			// "was the key found" and "is the value non-nil" are different
			// questions; an empty number field is initialised to JSON null.
			name: "a JSON null at a key that exists is found, not missing",
			doc: `{"tabs":[{"id":"bb1","type":"ui","state":{
				"data":{"demo":{"n":null}},
				"root":{"type":"text","value":{"bind":"demo.n"}}}}]}`,
		},
		{
			name: "a bare string child is a paragraph, not an unknown component",
			doc: `{"tabs":[{"id":"bb1","type":"ui","state":{
				"root":{"type":"col","children":["just some prose"]}}}]}`,
		},
		{
			name: "no tone at all means the default",
			doc: `{"tabs":[{"id":"bb1","type":"ui","state":{
				"root":{"type":"text","value":"hi"}}}]}`,
		},
		{
			name: "actions and intents are shell-level and every renderer may carry them",
			doc: `{"tabs":[{"id":"bb1","type":"kanban","state":{
				"nodes":[],"columns":[],"actions":[{"id":"bb2","label":"go"}],"intents":[]}}]}`,
		},
		{
			name: "a well-formed kv with both literal and bound values",
			doc: `{"tabs":[{"id":"bb1","type":"ui","state":{
				"data":{"rev":41},
				"root":{"type":"kv","pairs":[{"key":"rev","value":{"bind":"rev"}},{"key":"host","value":"local"}]}}}]}`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := writeWarnings(WebFS(), []byte(tc.doc)); len(got) != 0 {
				t.Errorf("want no warnings, got %d:\n%s", len(got), strings.Join(got, "\n"))
			}
		})
	}
}

func containsSubstring(haystack []string, needle string) bool {
	for _, s := range haystack {
		if strings.Contains(s, needle) {
			return true
		}
	}
	return false
}

// The tenth detector, and the one whose absence was worst: a `version` no
// renderer accepts blanks the WHOLE board rather than one component.
//
// It happened. `docs`-era schema.md showed `"version": 2` for a schema that had
// been 3 since v3 shipped, an agent copied the example it was reading, `apply`
// wrote it through, and aboard.html blanked the board in front of the human one
// round trip after being told it was ready. The write is STAMPED rather than
// refused — the content was fine, and failing a good write over a field the
// caller should not set is the worse trade — plus this warning, so the stale
// source still gets fixed. Both halves matter: the stamp saves the human, the
// warning reaches the agent.
func TestAStaleSchemaVersionIsReportedToTheWriter(t *testing.T) {
	got := wrongVersion([]byte(`{"version":2,"tabs":[]}`))
	if !strings.Contains(got, `says "version": 2`) {
		t.Errorf("a stale version was not reported: %q", got)
	}
	if !strings.Contains(got, "stamped") {
		t.Errorf("the warning does not say the server fixed it anyway: %q", got)
	}

	// Absent is not wrong: omitting `version` is the CORRECT thing for a caller
	// to do, since the server owns the field. Warning about it would be the noise
	// that teaches people to skip stderr.
	if got := wrongVersion([]byte(`{"tabs":[]}`)); got != "" {
		t.Errorf("omitting version was reported as a mistake: %q", got)
	}
	if got := wrongVersion([]byte(`{"version":3,"tabs":[]}`)); got != "" {
		t.Errorf("the current version was reported as stale: %q", got)
	}
}
