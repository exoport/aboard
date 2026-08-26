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
	"sync"

	"github.com/exoport/aboard/pkg/aboard/web"
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
	case tabTypeUI:
		return uiMarkdown(st)
	case tabTypeStack:
		return stackMarkdown(st)
	// The explicit non-cases. `log` lives in a sidecar file and not in the
	// document at all; `html` is a page, and a promoted screenshot of it is a
	// picture, not text; `trace` is the journal, which `aboard journal` prints.
	// Listed rather than left to the default so that "nobody has written this
	// one yet" and "this one has no text form" are different sentences in the
	// source, the way they are on screen.
	case "log", tabTypeHTML, "trace":
		return ""
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
	if at := str(d, keyAt); at != "" {
		fmt.Fprintf(b, ", %s", at[:min(isoDateLen, len(at))])
	}
	b.WriteString("\n")
	if reason := str(d, keyReason); reason != "" {
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
			if note := str(n, keyNote); note != "" {
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
			if note := str(n, keyNote); note != "" {
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
	// "|---|---|", not "|---|---||". The delimiter row used to be built as
	// "|" + repeat("---|") + "|", which declares one column more than the header
	// has — and a delimiter row that does not match the header is not a table at
	// all to a strict renderer, so every exported table arrived as literal pipes.
	fmt.Fprintf(&b, "| %s |\n|%s\n", strings.Join(head, " | "), strings.Repeat("---|", len(head)))
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
		if note := str(o, keyNote); note != "" {
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
		fmt.Fprintf(&b, "**%s**", str(m, keyBy))
		if at := str(m, keyAt); at != "" {
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
	if note := str(m, keyNote); note != "" {
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
		text := typeMarkdown(str(blk, keyType), inner)
		if text == "" {
			continue
		}
		fmt.Fprintf(&b, "## %s\n\n%s\n", firstNonEmpty(str(blk, "title"), str(blk, keyType)), text)
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
			head := make([]string, 0, 1+len(cols))
			head = append(head, "id")
			for _, c := range cols {
				head = append(head, firstNonEmpty(str(c, "label"), str(c, "id")))
			}
			var b strings.Builder
			fmt.Fprintf(&b, "%s\n", strings.Join(head, ","))
			for _, r := range rows {
				line := make([]string, 0, 1+len(cols))
				line = append(line, cell(r["id"]))
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
				cell(n["parent"]), cell(n["order"]), cell(n[keyNote]))
		}
		return b.String(), nil
	}

	// `--format md`, not `-format md`. The single-dash spelling is the spike's
	// grammar and it does not exist here; a message that hands the reader a flag
	// the binary rejects costs them a round trip to find out the tool was wrong.
	return "", errors.New("this tab has no rows or nodes to put in a csv — try `--format md`")
}

/* ---------- ui ---------- */

// uiSpec is views/ui.spec.json, read once from the EMBEDDED tree.
//
// Embedded rather than through the server's fs.FS, because export deliberately
// runs with no server: it reads the document off disk so a conclusion can be
// promoted from a cold checkout. `--dev` swaps the assets a SERVER serves and has
// nothing to do with a CLI read.
var uiSpec = sync.OnceValues(func() (typeSpec, bool) {
	specs, err := loadSpecs(web.FS)
	if err != nil {
		return typeSpec{}, false
	}
	for _, spec := range specs {
		if spec.Type == tabTypeUI {
			return spec, len(spec.Components) > 0
		}
	}
	return typeSpec{}, false
})

// uiMarkdown prints a `ui` tab as an indented outline.
//
// `ui` is the type CLAUDE.md tells agents to PREFER, and it was the one type
// export could not read — so the board's most-recommended shape was the one that
// could not be promoted into a project's own documents without a browser.
//
// An outline and not a rendering, and the difference is the honest part: this
// cannot see a layout that is legal and unreadable, any more than a clean write
// can. It is the material, not a screenshot and not a substitute for looking.
//
// Which prop is a node's text comes from views/ui.spec.json's `text`
// declaration, not from a table in here. A table would be a second copy of the
// catalog, kept in a language the manifest cannot see — the exact drift the
// declared-surface rule exists to prevent, and `capabilities ui` now answers
// "what does this component say" as well as "what does it read".
func uiMarkdown(st map[string]any) string {
	spec, ok := uiSpec()
	if !ok {
		return ""
	}
	data, _ := st["data"].(map[string]any)
	var b strings.Builder
	writeUINode(&b, spec, st["root"], data, 0)
	return b.String()
}

// writeUINode prints one node and, under it, whatever it contains.
func writeUINode(b *strings.Builder, spec typeSpec, node any, data map[string]any, depth int) {
	pad := strings.Repeat("  ", depth)

	// A bare string is a valid node: the renderer draws it as a paragraph.
	if text, isText := node.(string); isText {
		fmt.Fprintf(b, "%s- %s\n", pad, text)
		return
	}
	obj, ok := node.(map[string]any)
	if !ok {
		return
	}

	typeName, _ := obj[keyType].(string)
	component, known := spec.Components[typeName]
	if !known {
		// The same thing the renderer shows: a marker naming what could not be
		// drawn. Silently omitting it would let an export look complete when the
		// board it came from has a dashed red box in the middle of it.
		fmt.Fprintf(b, "%s- _unknown component `%s`_\n", pad, typeName)
		return
	}

	// A layout node draws nothing of its own, so it gets no line and costs no
	// indent — six nested rows and columns would otherwise push the one sentence
	// that matters twelve spaces to the right.
	if component.Layout {
		writeUIChildren(b, spec, obj, data, depth)
		return
	}

	if text := uiNodeText(component, obj, data); text != "" {
		fmt.Fprintf(b, "%s- %s: %s\n", pad, typeName, text)
	} else {
		fmt.Fprintf(b, "%s- %s\n", pad, typeName)
	}
	writeUIItems(b, spec, typeName, obj, data, depth+1)
	writeUIChildren(b, spec, obj, data, depth+1)
}

func writeUIChildren(b *strings.Builder, spec typeSpec, obj, data map[string]any, depth int) {
	kids, _ := obj["children"].([]any)
	for _, kid := range kids {
		writeUINode(b, spec, kid, data, depth)
	}
}

// writeUIItems prints the components whose content is a LIST rather than a
// subtree. This is the genuinely per-component half, and it is per-component
// because the shapes differ in kind: a checklist item has a tick, a kv pair has
// two halves, a panel has children. The declaration says which props hold items;
// what an item READS AS is the judgement here.
func writeUIItems(b *strings.Builder, spec typeSpec, typeName string, obj, data map[string]any, depth int) {
	pad := strings.Repeat("  ", depth)
	switch typeName {
	case "list":
		for _, item := range resolvedList(obj["items"], data) {
			fmt.Fprintf(b, "%s- %s\n", pad, uiText(resolveUI(item, data)))
		}
	case "kv":
		for _, pair := range resolvedMaps(obj["pairs"], data) {
			// TrimRight, because a pair whose value is empty would otherwise end
			// in a space — invisible in review and real in a golden file.
			line := fmt.Sprintf("%s- %s: %s", pad,
				uiText(resolveUI(pair["key"], data)), uiText(resolveUI(pair["value"], data)))
			fmt.Fprintf(b, "%s\n", strings.TrimRight(line, " "))
		}
	case "checklist":
		for _, item := range resolvedMaps(obj["items"], data) {
			// The tick is real state: an item's `bind` says where its boolean
			// lives, so a checklist an agent rendered is one it can read back —
			// and an export that printed `done` instead would report what the
			// agent asked for rather than what the human answered.
			done, _ := item["done"].(bool)
			if dotted, ok := item["bind"].(string); ok {
				if value, found := lookupBind(dotted, data); found {
					done, _ = value.(bool)
				}
			}
			box := " "
			if done {
				box = "x"
			}
			fmt.Fprintf(b, "%s- [%s] %s\n", pad, box, uiText(resolveUI(item["label"], data)))
		}
	case "table":
		writeUITable(b, obj, data, pad)
	case "tabs":
		for _, panel := range resolvedMaps(obj["panels"], data) {
			fmt.Fprintf(b, "%s- panel: %s\n", pad, uiText(resolveUI(panel["label"], data)))
			for _, kid := range asList(panel["children"]) {
				writeUINode(b, spec, kid, data, depth+1)
			}
		}
	}
}

// writeUITable prints a read-only ui table's rows. Rows are arrays or objects
// keyed by column id, exactly as the renderer accepts them.
//
// Rows, not a pipe table, and that is a deliberate loss. A markdown table nested
// under two levels of bullets is not a table to most renderers — it is five lines
// of literal pipes — so the outline would have looked right here and broken in
// the document it was pasted into. The `table` TAB type still exports as a real
// table, because there it is the whole tab and sits at the left margin.
func writeUITable(b *strings.Builder, obj, data map[string]any, pad string) {
	cols := resolvedMaps(obj["columns"], data)
	head := make([]string, 0, len(cols))
	for _, col := range cols {
		head = append(head, firstNonEmpty(uiText(resolveUI(col["label"], data)), uiText(resolveUI(col["id"], data))))
	}
	if len(head) == 0 {
		return
	}
	bold := make([]string, 0, len(head))
	for _, cell := range head {
		bold = append(bold, "**"+cell+"**")
	}
	fmt.Fprintf(b, "%s- %s\n", pad, strings.Join(bold, " · "))
	for _, row := range asList(resolveUI(obj["rows"], data)) {
		cells := make([]string, 0, len(cols))
		switch r := row.(type) {
		case []any:
			for i := range cols {
				if i < len(r) {
					cells = append(cells, cellText(resolveUI(r[i], data)))
					continue
				}
				cells = append(cells, "")
			}
		case map[string]any:
			for _, col := range cols {
				id, _ := col["id"].(string)
				cells = append(cells, cellText(resolveUI(r[id], data)))
			}
		default:
			continue
		}
		fmt.Fprintf(b, "%s- %s\n", pad, strings.Join(cells, " · "))
	}
}

// uiNodeText reads a node's display text out of the props views/ui.spec.json
// says carry it. Parts are joined with a middot: a `stat` is a number and its
// caption, a `notice` is a lead-in and a line, a `quote` is the words and who
// said them — two halves of one statement in each case, and dropping the second
// loses the half that says what the first one means.
func uiNodeText(component componentSpec, obj, data map[string]any) string {
	parts := make([]string, 0, len(component.Text))
	for _, entry := range component.Text {
		if part := uiTextPart(entry, obj, data); part != "" {
			parts = append(parts, part)
		}
	}
	return strings.Join(parts, " · ")
}

// uiTextPart resolves one entry of a `text` declaration.
//
//	"a|b"  the first of these props that is set — the renderer's own `value ?? text`
//	"=p"   the value in state.data at the path the prop `p` names — a `field`'s answer
func uiTextPart(entry string, obj, data map[string]any) string {
	if prop, isPath := strings.CutPrefix(entry, "="); isPath {
		name, _ := obj[prop].(string)
		if name == "" {
			return ""
		}
		value, found := lookupBind(name, data)
		if !found {
			return ""
		}
		return uiText(value)
	}
	for prop := range strings.SplitSeq(entry, "|") {
		if value, ok := obj[prop]; ok {
			if text := uiText(resolveUI(value, data)); text != "" {
				return text
			}
		}
	}
	return ""
}

// resolveUI turns a `{bind:"path"}` into the value it points at, reusing the
// resolution the write-time checker performs. A path that leads nowhere resolves
// to nothing, which is what the renderer draws too.
func resolveUI(value any, data map[string]any) any {
	dotted, ok := bindPath(value)
	if !ok {
		return value
	}
	resolved, _ := lookupBind(dotted, data)
	return resolved
}

// uiText is views/ui.js's asText: a string as itself, anything else as its JSON,
// and null or absent as nothing at all.
func uiText(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	default:
		return jsonText(v)
	}
}

func asList(v any) []any {
	list, _ := v.([]any)
	return list
}

// resolvedList resolves a prop that may itself be a `{bind}` to an array.
func resolvedList(v any, data map[string]any) []any {
	return asList(resolveUI(v, data))
}

func resolvedMaps(v any, data map[string]any) []map[string]any {
	return mapsOf(resolveUI(v, data))
}
