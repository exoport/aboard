package aboard

import (
	"fmt"
	"strings"
)

// embed.go — what a HOST that shows the board inside something of its own may
// rely on, declared.
//
// The shell speaks to a host in `{__aboard: …}` messages and reads a few URL
// parameters a host puts on its address. None of that was in the manifest, so a
// host had one way to learn whether a board understood it: load the page and
// see. The VS Code extension still greps the shell's HTML for `dataset.chrome` for
// exactly that reason, and it wrote down why — "there is no field in
// /capabilities to test".
//
// This is that field. It is DECLARED, like the command table, and checked against
// the web tree by TestTheEmbedDeclarationMatchesTheShell rather than scraped from
// it: a scrape would believe whatever the code says, including a message somebody
// deleted by accident.
//
// Adding a message, a parameter or a channel moves capsHash, which is correct: a
// host that reads this is depending on it.

// embedSpec is the embedding surface as `aboard capabilities` reports it.
type embedSpec struct {
	// Channels are where a host may be. `frame`: the board is in an iframe and
	// the host is its parent. `top`: the board is the host's own top-level page
	// and the host runs script in it, turned on by `?embed=top`. Both carry the
	// same messages under the same rule — a message is accepted when it comes
	// from the host's window — so a host picks a channel, not a feature set.
	Channels []string `json:"channels"`
	// Params are the shell's URL parameters a host may set. Each is per-viewer
	// and read from the address; none is ever written to the board.
	Params []embedParam `json:"params"`
	// In are the messages a host may post to the board; Out are the ones the
	// board posts to its host.
	In  []embedMessage `json:"in"`
	Out []embedMessage `json:"out"`
	// Doc is where the whole contract is written down.
	Doc string `json:"doc"`
}

type embedParam struct {
	Name   string   `json:"name"`
	Values []string `json:"values"`
	Doc    string   `json:"doc"`
}

type embedMessage struct {
	Type string `json:"type"`
	Doc  string `json:"doc"`
}

const (
	embedChannelFrame = "frame"
	embedChannelTop   = "top"
	// The theme is both a URL parameter and a message, spelt the same.
	embedTheme = "theme"
)

// declaredEmbed is the surface. A list in the order a host meets each thing,
// not sorted: the order is part of what a reader of the reference follows.
var declaredEmbed = embedSpec{
	Channels: []string{embedChannelFrame, embedChannelTop},
	Params: []embedParam{
		{
			Name: "chrome", Values: []string{"full", "notabs", "none"},
			Doc: "how much of the board's own head to draw: `notabs` hides the tab strip and its `+` for a host that lists tabs itself",
		},
		{
			Name: "embed", Values: []string{embedChannelTop},
			Doc: "`top`: a host runs this page top level and speaks to it on the same window — without it an unframed page has no host",
		},
		{
			Name: embedTheme, Values: []string{ThemeDark, ThemeLight},
			Doc: "the variant to paint from the first frame, outranking the viewer's stored choice and the project default until the viewer presses the switch; never stored",
		},
	},
	In: []embedMessage{
		{Type: "host", Doc: "what the host can do for the board: `{name, clipboard}`"},
		{Type: embedTheme, Doc: "a palette: `{kind, tokens}`, validated against the theme tokens and written nowhere"},
		{Type: "newtab", Doc: "open the board's own New tab sheet; the human still names the tab and can cancel"},
		{Type: "clipboard-result", Doc: "the answer to a `clipboard-image` request: `{id, ok, error, tool}`"},
	},
	Out: []embedMessage{
		{Type: "active", Doc: "the tab on screen changed: `{tab}`"},
		{Type: "clipboard-image", Doc: "put this PNG on the system clipboard: `{id, dataUrl}`"},
	},
	Doc: "docs/reference/http-api.md#two-channels-framed-and-top-level",
}

// embedMarkdown is the Embedding section of the generated reference.
func embedMarkdown(b *strings.Builder, e embedSpec) {
	if len(e.Channels) == 0 {
		return
	}
	fmt.Fprintf(b, "## Embedding\n\nFor a host that shows the board inside something of its own. "+
		"Channels: `%s` — the same messages on each, accepted only from the host's window. "+
		"The contract is in `%s`.\n\n", strings.Join(e.Channels, "`, `"), e.Doc)
	fmt.Fprintf(b, "| URL parameter | values | meaning |\n|---|---|---|\n")
	for _, p := range e.Params {
		fmt.Fprintf(b, "| `%s` | %s | %s |\n", p.Name, strings.Join(p.Values, ", "), p.Doc)
	}
	fmt.Fprintf(b, "\n| message | direction | meaning |\n|---|---|---|\n")
	for _, msg := range e.In {
		fmt.Fprintf(b, "| `%s` | host → board | %s |\n", msg.Type, msg.Doc)
	}
	for _, msg := range e.Out {
		fmt.Fprintf(b, "| `%s` | board → host | %s |\n", msg.Type, msg.Doc)
	}
	b.WriteString("\n")
}
