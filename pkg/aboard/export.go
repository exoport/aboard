// export.go — a tab, as text you can paste into the project's own documents.
//
//	aboard export bb128                 markdown
//	aboard export bb128 --format csv    rows, where the tab has rows
//
// Why this exists on the CLI when the browser already has an export menu: that
// menu is useless to an agent, so promoting a tab's conclusions into a spec meant
// retyping them. The strategy we settled on is not to promote early — it is to
// make LATE promotion cheap, and retyping is the cost that made it expensive.
//
// Reads the board document from disk, so it works with no server running.
//
// Deliberately not the same output as views/export.js. That one produces a
// faithful dump for a human to download; this one is tuned for PROMOTION —
// decisions lead with their reasons, answers appear next to their questions, and
// anything with no useful text form says so instead of emitting an empty section.
// Where the two differ, that is the intent, not drift.

package aboard

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
)

// Export prints one tab as text for pasting into the project's own documents.
func Export(stateFile, tabID, format string, out, errOut io.Writer) error {
	raw, err := os.ReadFile(stateFile)
	if err != nil {
		return fmt.Errorf("cannot read %s: %w", stateFile, err)
	}
	var doc struct {
		Tabs []struct {
			ID    string          `json:"id"`
			Key   string          `json:"key"`
			Name  string          `json:"name"`
			Type  string          `json:"type"`
			Note  string          `json:"note"`
			State json.RawMessage `json:"state"`
		} `json:"tabs"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		return fmt.Errorf("%s is not readable: %w", stateFile, err)
	}

	for _, t := range doc.Tabs {
		if t.ID != tabID && t.Key != tabID {
			continue
		}
		var st map[string]any
		if len(t.State) > 0 {
			_ = json.Unmarshal(t.State, &st)
		}
		if format == "csv" {
			rows, err := tabCSV(st)
			if err != nil {
				return err
			}
			fmt.Fprint(out, rows)
			return nil
		}
		fmt.Fprint(out, tabMarkdown(t.Name, t.Type, t.Note, st))
		return nil
	}
	// Not found: show what IS there rather than making them go and look. A wrong
	// id is nearly always a forgotten one.
	fmt.Fprintf(errOut, "no tab %q. This board has:\n\n", tabID)
	_ = exportList(stateFile, errOut)
	return errors.New("pick one of the ids or keys above")
}

func exportList(stateFile string, out io.Writer) error {
	raw, err := os.ReadFile(stateFile)
	if err != nil {
		return fmt.Errorf("cannot read %s: %w", stateFile, err)
	}
	var doc struct {
		Tabs []struct {
			ID   string `json:"id"`
			Key  string `json:"key"`
			Name string `json:"name"`
			Type string `json:"type"`
		} `json:"tabs"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		return err
	}
	for _, t := range doc.Tabs {
		fmt.Fprintf(out, "%-8s %-10s %-22s %s\n", t.ID, t.Type, t.Name, t.Key)
	}
	return nil
}

/* ---------- helpers over the generic map ---------- */

func mapsOf(v any) []map[string]any {
	list, ok := v.([]any)
	if !ok {
		return nil
	}
	out := make([]map[string]any, 0, len(list))
	for _, item := range list {
		if m, ok := item.(map[string]any); ok {
			out = append(out, m)
		}
	}
	return out
}

func str(m map[string]any, key string) string {
	if s, ok := m[key].(string); ok {
		return s
	}
	return ""
}

func stringsOf(v any) []string {
	list, ok := v.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(list))
	for _, item := range list {
		if s, ok := item.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

/* ---------- markdown ---------- */

func tabMarkdown(name, typ, note string, st map[string]any) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n\n", name)
	if note != "" {
		// The tab's own statement of what it was for. Usually the sentence you
		// want at the top of the promoted section anyway.
		fmt.Fprintf(&b, "%s\n\n", note)
	}
	body := typeMarkdown(typ, st)
	if body == "" {
		fmt.Fprintf(&b, "_A %s tab has no useful text form — look at it instead._\n", typ)
		return b.String()
	}
	b.WriteString(body)
	return b.String()
}

// typeMarkdown renders one tab type's state as markdown, and is only the
// DISPATCH. Every renderer's text form is its own function below.
//
// It used to be one switch carrying all eleven, which measured 70 branches: a
// function nobody could change one arm of without reading the other ten. The
// arms share nothing but the builder, so splitting them costs nothing and the
// dispatch stays a table you can read in one screen.
//
// An unknown type returns "" on purpose — tabMarkdown turns that into "look at
// it instead", which is the honest answer for a type with no text form.
func typeMarkdown(typ string, st map[string]any) string {
	switch typ {
	case "gate":
		return gateMarkdown(st)
	case "dag":
		return dagMarkdown(st)
	case "kanban":
		return kanbanMarkdown(st)
	case "form":
		return formMarkdown(st)
	case "table":
		return tableMarkdown(st)
	case "vote":
		return voteMarkdown(st)
	case "notes":
		return str(st, "text") + "\n"
	case "chat":
		return chatMarkdown(st)
	case "markup":
		return markupMarkdown(st)
	case "diagram":
		return diagramMarkdown(st)
	case tabTypeStack:
		return stackMarkdown(st)
	default:
		return ""
	}
}

// isoDateLen is the length of the date half of an RFC3339 stamp. A decision is
// promoted with its DAY, not its second: the minute a verdict was recorded is
// noise in someone else's spec.
const isoDateLen = 10

func gateMarkdown(st map[string]any) string {
	var b strings.Builder
	if decided := mapsOf(st["decided"]); len(decided) > 0 {
		b.WriteString("## Decisions\n\n")
		for _, d := range decided {
			writeDecision(&b, d)
		}
		b.WriteString("\n")
	}
	if pending := mapsOf(st["pending"]); len(pending) > 0 {
		b.WriteString("## Still waiting on a decision\n\n")
		for _, p := range pending {
			fmt.Fprintf(&b, "- **%s**", str(p, "title"))
			if risk := str(p, "risk"); risk != "" {
				fmt.Fprintf(&b, " (%s risk)", risk)
			}
			b.WriteString("\n")
			if detail := str(p, "detail"); detail != "" {
				fmt.Fprintf(&b, "  %s\n", detail)
			}
		}
		b.WriteString("\n")
	}
	return b.String()
}

// writeDecision renders one decided row. Decisions lead with their reasons,
// because the reason is the half that evaporates and the half that stops the
// argument recurring — so a decision with no reason says so out loud rather
// than promoting a bare verdict into someone's spec.
func writeDecision(b *strings.Builder, d map[string]any) {
	verdict := str(d, "verdict")
	if d["undone"] == true {
		verdict += " (reversed)"
	}
	fmt.Fprintf(b, "- **%s** — %s", str(d, "title"), verdict)
	if at := str(d, "at"); at != "" {
		fmt.Fprintf(b, ", %s", at[:min(isoDateLen, len(at))])
	}
	b.WriteString("\n")
	if reason := str(d, "reason"); reason != "" {
		late := ""
		if str(d, "reasonAddedAt") != "" {
			late = " _(reason added after the fact)_"
		}
		fmt.Fprintf(b, "  - Why: %s%s\n", reason, late)
	} else {
		b.WriteString("  - Why: _not recorded_ — ask before relying on this\n")
	}
	if edited := str(d, "editedTo"); edited != "" {
		fmt.Fprintf(b, "  - Changed before allowing:\n\n    ```\n    %s\n    ```\n",
			strings.ReplaceAll(edited, "\n", "\n    "))
	}
}

func dagMarkdown(st map[string]any) string {
	nodes := mapsOf(st["nodes"])
	if len(nodes) == 0 {
		return ""
	}
	var b strings.Builder
	var walk func(parent string, depth int)
	walk = func(parent string, depth int) {
		for _, n := range nodes {
			if str(n, "parent") != parent {
				continue
			}
			pad := strings.Repeat("  ", depth)
			fmt.Fprintf(&b, "%s- **%s**", pad, str(n, "title"))
			if status := str(n, "status"); status != "" {
				fmt.Fprintf(&b, " — _%s_", status)
			}
			b.WriteString("\n")
			if note := str(n, "note"); note != "" {
				fmt.Fprintf(&b, "%s  %s\n", pad, note)
			}
			walk(str(n, "id"), depth+1)
		}
	}
	walk("", 0)
	return b.String()
}

func kanbanMarkdown(st map[string]any) string {
	nodes := mapsOf(st["nodes"])
	if len(nodes) == 0 {
		return ""
	}
	var b strings.Builder
	for _, col := range stringsOf(st["columns"]) {
		items := make([]map[string]any, 0, len(nodes))
		for _, n := range nodes {
			if str(n, "status") == col {
				items = append(items, n)
			}
		}
		fmt.Fprintf(&b, "## %s (%d)\n\n", col, len(items))
		for _, n := range items {
			fmt.Fprintf(&b, "- **%s**\n", str(n, "title"))
			if note := str(n, "note"); note != "" {
				fmt.Fprintf(&b, "  %s\n", note)
			}
		}
		b.WriteString("\n")
	}
	return b.String()
}

func formMarkdown(st map[string]any) string {
	fields := mapsOf(st["fields"])
	if len(fields) == 0 {
		return ""
	}
	var b strings.Builder
	if intro := str(st, "intro"); intro != "" {
		fmt.Fprintf(&b, "%s\n\n", intro)
	}
	for _, f := range fields {
		label := firstNonEmpty(str(f, "label"), str(f, "id"))
		fmt.Fprintf(&b, "- **%s** — %s\n", label, jsonText(f["value"]))
	}
	return b.String()
}

func tableMarkdown(st map[string]any) string {
	cols := mapsOf(st["columns"])
	rows := mapsOf(st["rows"])
	if len(cols) == 0 || len(rows) == 0 {
		return ""
	}
	var b strings.Builder
	head := make([]string, 0, len(cols))
	for _, c := range cols {
		head = append(head, firstNonEmpty(str(c, "label"), str(c, "id")))
	}
	fmt.Fprintf(&b, "| %s |\n|%s|\n", strings.Join(head, " | "), strings.Repeat("---|", len(head)))
	for _, r := range rows {
		cells := make([]string, 0, len(cols))
		for _, c := range cols {
			cells = append(cells, cellText(r[str(c, "id")]))
		}
		fmt.Fprintf(&b, "| %s |\n", strings.Join(cells, " | "))
	}
	return b.String()
}

func voteMarkdown(st map[string]any) string {
	options := mapsOf(st["options"])
	if len(options) == 0 {
		return ""
	}
	var b strings.Builder
	if q := str(st, "question"); q != "" {
		fmt.Fprintf(&b, "**%s**\n\n", q)
	}
	ballots, _ := st["ballots"].(map[string]any)
	for _, o := range options {
		id := str(o, "id")
		fmt.Fprintf(&b, "- **%s**", firstNonEmpty(str(o, "label"), id))
		if scores := ballotScores(ballots, id); len(scores) > 0 {
			fmt.Fprintf(&b, " — %s", strings.Join(scores, ", "))
		}
		b.WriteString("\n")
		if note := str(o, "note"); note != "" {
			fmt.Fprintf(&b, "  %s\n", note)
		}
		// The comments are the reasoning, which is the point of exporting a
		// vote at all — a bare score promotes nothing.
		writeComments(&b, o["comments"])
	}
	return b.String()
}

// ballotScores returns "<actor> <score>" for every actor who scored this
// option, in actor order so two exports of the same board are byte-identical.
func ballotScores(ballots map[string]any, optionID string) []string {
	actors := make([]string, 0, len(ballots))
	for actor := range ballots {
		actors = append(actors, actor)
	}
	sort.Strings(actors)
	scores := make([]string, 0, len(actors))
	for _, actor := range actors {
		m, ok := ballots[actor].(map[string]any)
		if !ok {
			continue
		}
		if v, ok := m[optionID]; ok {
			scores = append(scores, fmt.Sprintf("%s %v", actor, v))
		}
	}
	return scores
}

func writeComments(b *strings.Builder, v any) {
	comments, ok := v.(map[string]any)
	if !ok {
		return
	}
	keys := make([]string, 0, len(comments))
	for k := range comments {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if text, ok := comments[k].(string); ok && text != "" {
			fmt.Fprintf(b, "  - %s: %s\n", k, text)
		}
	}
}

func chatMarkdown(st map[string]any) string {
	msgs := mapsOf(st["messages"])
	if len(msgs) == 0 {
		return ""
	}
	var b strings.Builder
	for _, m := range msgs {
		fmt.Fprintf(&b, "**%s**", str(m, "by"))
		if at := str(m, "at"); at != "" {
			fmt.Fprintf(&b, " · %s", at)
		}
		fmt.Fprintf(&b, "\n\n%s\n\n", str(m, "text"))
	}
	return b.String()
}

func markupMarkdown(st map[string]any) string {
	images := mapsOf(st["images"])
	if len(images) == 0 {
		return ""
	}
	var b strings.Builder
	for _, im := range images {
		fmt.Fprintf(&b, "## %s\n\n![%s](%s)\n\n",
			firstNonEmpty(str(im, "caption"), str(im, "id")), str(im, "caption"), str(im, "src"))
		for _, r := range mapsOf(im["regions"]) {
			writeMark(&b, r, "at "+pctBox(r))
		}
		for _, s := range mapsOf(im["strokes"]) {
			writeMark(&b, s, "freehand")
		}
		b.WriteString("\n")
	}
	return b.String()
}

// writeMark renders one mark, badged with its own id — the same identifier that
// is drawn on the image and printed in the tab's list, so a sentence naming it
// resolves in all three places.
func writeMark(b *strings.Builder, m map[string]any, where string) {
	fmt.Fprintf(b, "- `%s` %s", str(m, "id"), where)
	if note := str(m, "note"); note != "" {
		fmt.Fprintf(b, " — %s", note)
	}
	b.WriteString("\n")
}

func diagramMarkdown(st map[string]any) string {
	src := str(st, "source")
	if src == "" {
		return ""
	}
	return fmt.Sprintf("```mermaid\n%s\n```\n", strings.TrimSpace(src))
}

func stackMarkdown(st map[string]any) string {
	blocks := mapsOf(st["blocks"])
	if len(blocks) == 0 {
		return ""
	}
	var b strings.Builder
	for _, blk := range blocks {
		inner, _ := blk["state"].(map[string]any)
		text := typeMarkdown(str(blk, "type"), inner)
		if text == "" {
			continue
		}
		fmt.Fprintf(&b, "## %s\n\n%s\n", firstNonEmpty(str(blk, "title"), str(blk, "type")), text)
	}
	return b.String()
}

func cellText(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return strings.ReplaceAll(strings.ReplaceAll(t, "|", "\\|"), "\n", " ")
	case bool:
		if t {
			return "yes"
		}
		return "no"
	default:
		return jsonText(v)
	}
}

// jsonText is the fallback rendering for a value with no text form of its own:
// a form field holding an object, a table cell holding a list. The error is
// SHOWN rather than swallowed — a silently blank cell in a promoted document is
// the failure mode this whole file exists to avoid.
func jsonText(v any) string {
	body, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprintf("_(unrenderable: %v)_", err)
	}
	return string(body)
}

func pctBox(r map[string]any) string {
	// A mark's box is stored normalised 0..1; the export prints whole percents,
	// and adding a half before truncating is what makes int() round rather than
	// floor — 0.335 must read 34%, not 33%.
	const (
		asPercent    = 100
		roundingBias = 0.5
	)
	num := func(key string) int {
		if f, ok := r[key].(float64); ok {
			return int(f*asPercent + roundingBias)
		}
		return 0
	}
	return fmt.Sprintf("%d%%,%d%% %d×%d%%", num("x"), num("y"), num("w"), num("h"))
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

/* ---------- csv ---------- */

func tabCSV(st map[string]any) (string, error) {
	cell := func(v any) string {
		s := cellText(v)
		if strings.ContainsAny(s, ",\"\n") {
			return `"` + strings.ReplaceAll(s, `"`, `""`) + `"`
		}
		return s
	}

	if cols := mapsOf(st["columns"]); len(cols) > 0 {
		if rows := mapsOf(st["rows"]); len(rows) > 0 {
			head := []string{"id"}
			for _, c := range cols {
				head = append(head, firstNonEmpty(str(c, "label"), str(c, "id")))
			}
			var b strings.Builder
			fmt.Fprintf(&b, "%s\n", strings.Join(head, ","))
			for _, r := range rows {
				line := []string{cell(r["id"])}
				for _, c := range cols {
					line = append(line, cell(r[str(c, "id")]))
				}
				fmt.Fprintf(&b, "%s\n", strings.Join(line, ","))
			}
			return b.String(), nil
		}
	}

	if nodes := mapsOf(st["nodes"]); len(nodes) > 0 {
		var b strings.Builder
		b.WriteString("id,title,status,parent,order,note\n")
		for _, n := range nodes {
			fmt.Fprintf(&b, "%s,%s,%s,%s,%s,%s\n",
				cell(n["id"]), cell(n["title"]), cell(n["status"]),
				cell(n["parent"]), cell(n["order"]), cell(n["note"]))
		}
		return b.String(), nil
	}

	// `--format md`, not `-format md`. The single-dash spelling is the spike's
	// grammar and it does not exist here; a message that hands the reader a flag
	// the binary rejects costs them a round trip to find out the tool was wrong.
	return "", errors.New("this tab has no rows or nodes to put in a csv — try `--format md`")
}
