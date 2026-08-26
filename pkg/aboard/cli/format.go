// format.go — the --output-format convention, matching ape's.
//
// Structured output goes to stdout and diagnostics to stderr, so a consumer can
// pipe `--output-format json` straight into jq without filtering prose out of it.

package cli

import (
	"encoding/json"
	"fmt"
	"io"

	"gopkg.in/yaml.v3"
)

const (
	formatHuman = "human"
	formatJSON  = "json"
	formatYAML  = "yaml"
)

// checkOutputFormat refuses an unknown --output-format BEFORE the command does
// anything.
//
// renderOutput validates too, but it runs LAST, after the side effect: `init
// --output-format nope` created `.aboard/`, wrote the document, appended to
// `.gitignore` and then exited 2 — a status the declared table defines as
// "detected before anything was contacted". The corrected retry then exited 1
// with "already exists", so the user's first two attempts both failed and the
// board they now had was the one the failed run made. A command with a side
// effect calls this first; renderOutput keeps its own check for the commands
// that have none.
func checkOutputFormat(format string) error {
	switch format {
	case formatHuman, formatJSON, formatYAML, "":
		return nil
	default:
		return usageErr(fmt.Errorf("--output-format must be human, json or yaml, got %q", format))
	}
}

// renderOutput writes value in the requested format. human is a function rather
// than a value so the caller decides what "human" looks like, while json and
// yaml always serialise the same value the human form was rendered from — which
// is what stops the two drifting apart.
func renderOutput(w io.Writer, format string, value any, human func() string) error {
	switch format {
	case formatJSON:
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(value)
	case formatYAML:
		enc := yaml.NewEncoder(w)
		enc.SetIndent(2)
		defer func() { _ = enc.Close() }()
		return enc.Encode(value)
	case formatHuman, "":
		_, err := io.WriteString(w, human())
		return err
	default:
		// One sentence, in one place: this branch is only ever reached for a
		// format checkOutputFormat would have refused.
		return checkOutputFormat(format)
	}
}
