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
	return fmt.Errorf("pick one of the ids or keys above")
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

func typeMarkdown(typ string, st map[string]any) string {
	var b strings.Builder
	switch typ {

	case "gate":
		// Decisions lead with their reasons, because the reason is the half that
		// evaporates and the half that stops the argument recurring.
		decided := mapsOf(st["decided"])
		if len(decided) > 0 {
			b.WriteString("## Decisions\n\n")
			for _, d := range decided {
				verdict := str(d, "verdict")
				if d["undone"] == true {
					verdict += " (reversed)"
				}
				fmt.Fprintf(&b, "- **%s** — %s", str(d, "title"), verdict)
				if at := str(d, "at"); at != "" {
					fmt.Fprintf(&b, ", %s", at[:min(10, len(at))])
				}
				b.WriteString("\n")
				if reason := str(d, "reason"); reason != "" {
					late := ""
					if str(d, "reasonAddedAt") != "" {
						late = " _(reason added after the fact)_"
					}
					fmt.Fprintf(&b, "  - Why: %s%s\n", reason, late)
				} else {
					fmt.Fprintf(&b, "  - Why: _not recorded_ — ask before relying on this\n")
				}
				if edited := str(d, "editedTo"); edited != "" {
					fmt.Fprintf(&b, "  - Changed before allowing:\n\n    ```\n    %s\n    ```\n",
						strings.ReplaceAll(edited, "\n", "\n    "))
				}
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

	case "dag", "kanban":
		nodes := mapsOf(st["nodes"])
		if len(nodes) == 0 {
			return ""
		}
		if typ == "kanban" {
			for _, col := range stringsOf(st["columns"]) {
				items := []map[string]any{}
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
			break
		}
		var walk func(parent string, depth int)
		walk = func(parent string, depth int) {
			for _, n := range nodes {
				p := str(n, "parent")
				if p != parent {
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

	case "form":
		fields := mapsOf(st["fields"])
		if len(fields) == 0 {
			return ""
		}
		if intro := str(st, "intro"); intro != "" {
			fmt.Fprintf(&b, "%s\n\n", intro)
		}
		for _, f := range fields {
			label := str(f, "label")
			if label == "" {
				label = str(f, "id")
			}
			value, _ := json.Marshal(f["value"])
			fmt.Fprintf(&b, "- **%s** — %s\n", label, string(value))
		}

	case "table":
		cols := mapsOf(st["columns"])
		rows := mapsOf(st["rows"])
		if len(cols) == 0 || len(rows) == 0 {
			return ""
		}
		head := make([]string, 0, len(cols))
		for _, c := range cols {
			label := str(c, "label")
			if label == "" {
				label = str(c, "id")
			}
			head = append(head, label)
		}
		fmt.Fprintf(&b, "| %s |\n|%s|\n", strings.Join(head, " | "), strings.Repeat("---|", len(head)))
		for _, r := range rows {
			cells := make([]string, 0, len(cols))
			for _, c := range cols {
				cells = append(cells, cellText(r[str(c, "id")]))
			}
			fmt.Fprintf(&b, "| %s |\n", strings.Join(cells, " | "))
		}

	case "vote":
		options := mapsOf(st["options"])
		if len(options) == 0 {
			return ""
		}
		if q := str(st, "question"); q != "" {
			fmt.Fprintf(&b, "**%s**\n\n", q)
		}
		ballots, _ := st["ballots"].(map[string]any)
		for _, o := range options {
			id := str(o, "id")
			fmt.Fprintf(&b, "- **%s**", firstNonEmpty(str(o, "label"), id))
			scores := []string{}
			actors := make([]string, 0, len(ballots))
			for actor := range ballots {
				actors = append(actors, actor)
			}
			sort.Strings(actors)
			for _, actor := range actors {
				if m, ok := ballots[actor].(map[string]any); ok {
					if v, ok := m[id]; ok {
						scores = append(scores, fmt.Sprintf("%s %v", actor, v))
					}
				}
			}
			if len(scores) > 0 {
				fmt.Fprintf(&b, " — %s", strings.Join(scores, ", "))
			}
			b.WriteString("\n")
			if note := str(o, "note"); note != "" {
				fmt.Fprintf(&b, "  %s\n", note)
			}
			// The comments are the reasoning, which is the point of exporting a
			// vote at all — a bare score promotes nothing.
			if comments, ok := o["comments"].(map[string]any); ok {
				keys := make([]string, 0, len(comments))
				for k := range comments {
					keys = append(keys, k)
				}
				sort.Strings(keys)
				for _, k := range keys {
					if text, ok := comments[k].(string); ok && text != "" {
						fmt.Fprintf(&b, "  - %s: %s\n", k, text)
					}
				}
			}
		}

	case "notes":
		return str(st, "text") + "\n"

	case "chat":
		msgs := mapsOf(st["messages"])
		if len(msgs) == 0 {
			return ""
		}
		for _, m := range msgs {
			fmt.Fprintf(&b, "**%s**", str(m, "by"))
			if at := str(m, "at"); at != "" {
				fmt.Fprintf(&b, " · %s", at)
			}
			fmt.Fprintf(&b, "\n\n%s\n\n", str(m, "text"))
		}

	case "markup":
		images := mapsOf(st["images"])
		if len(images) == 0 {
			return ""
		}
		for _, im := range images {
			fmt.Fprintf(&b, "## %s\n\n![%s](%s)\n\n",
				firstNonEmpty(str(im, "caption"), str(im, "id")), str(im, "caption"), str(im, "src"))
			for _, r := range mapsOf(im["regions"]) {
				fmt.Fprintf(&b, "- `%s` at %s", str(r, "id"), pctBox(r))
				if note := str(r, "note"); note != "" {
					fmt.Fprintf(&b, " — %s", note)
				}
				b.WriteString("\n")
			}
			for _, s := range mapsOf(im["strokes"]) {
				fmt.Fprintf(&b, "- `%s` freehand", str(s, "id"))
				if note := str(s, "note"); note != "" {
					fmt.Fprintf(&b, " — %s", note)
				}
				b.WriteString("\n")
			}
			b.WriteString("\n")
		}

	case "diagram":
		src := str(st, "source")
		if src == "" {
			return ""
		}
		fmt.Fprintf(&b, "```mermaid\n%s\n```\n", strings.TrimSpace(src))

	case "stack":
		blocks := mapsOf(st["blocks"])
		if len(blocks) == 0 {
			return ""
		}
		for _, blk := range blocks {
			inner, _ := blk["state"].(map[string]any)
			text := typeMarkdown(str(blk, "type"), inner)
			if text == "" {
				continue
			}
			fmt.Fprintf(&b, "## %s\n\n%s\n", firstNonEmpty(str(blk, "title"), str(blk, "type")), text)
		}
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
		body, _ := json.Marshal(v)
		return string(body)
	}
}

func pctBox(r map[string]any) string {
	num := func(key string) int {
		if f, ok := r[key].(float64); ok {
			return int(f*100 + 0.5)
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

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
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

	return "", fmt.Errorf("this tab has no rows or nodes to put in a csv — try -format md")
}
