package aboard

import (
	"io/fs"
	"regexp"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/exoport/aboard/pkg/aboard/web"
)

// webSources is the shell and every view module, as EMBEDDED — the bytes a host
// actually talks to — keyed by their path in the tree.
func webSources(t *testing.T) map[string]string {
	t.Helper()
	out := map[string]string{}
	shell, err := fs.ReadFile(web.FS, "aboard.html")
	if err != nil {
		t.Fatalf("reading the shell: %v", err)
	}
	out["aboard.html"] = string(shell)
	views, err := fs.Glob(web.FS, "views/*.js")
	if err != nil {
		t.Fatalf("listing views: %v", err)
	}
	for _, name := range views {
		body, err := fs.ReadFile(web.FS, name)
		if err != nil {
			t.Fatalf("reading %s: %v", name, err)
		}
		out[name] = string(body)
	}
	return out
}

// The embed section of the manifest is a promise to a host that reads it
// instead of loading the page to find out, so it is checked against the web
// tree in BOTH directions: everything declared is handled, and everything
// handled is declared. A host that trusts `in` and posts a message the shell
// no longer reads would get silence — the exact ambiguity the `host`
// announcement was added to end.
func TestTheEmbedDeclarationMatchesTheShell(t *testing.T) {
	src := webSources(t)
	shell := src["aboard.html"]

	// In: the shell's one embedder listener dispatches on `msg.__aboard === '…'`,
	// and nothing else in the shell uses that spelling.
	declaredIn := make([]string, 0, len(declaredEmbed.In))
	for _, m := range declaredEmbed.In {
		declaredIn = append(declaredIn, m.Type)
	}
	handled := uniqueMatches(regexp.MustCompile(`msg\.__aboard === '([a-z-]+)'`), shell)
	assertSameSet(t, "messages a host may post (embed.in)", declaredIn, handled,
		"declared in embed.go", "handled by aboard.html's embedder listener")

	// Out: every message to a host goes through postToEmbedder, so its call
	// sites across the whole tree are the complete list.
	declaredOut := make([]string, 0, len(declaredEmbed.Out))
	for _, m := range declaredEmbed.Out {
		declaredOut = append(declaredOut, m.Type)
	}
	posted := make([]string, 0, len(declaredEmbed.Out))
	for _, body := range src {
		posted = append(posted, uniqueMatches(regexp.MustCompile(`postToEmbedder\(\{\s*__aboard: '([a-z-]+)'`), body)...)
	}
	assertSameSet(t, "messages the board posts to its host (embed.out)", declaredOut, dedupe(posted),
		"declared in embed.go", "posted through postToEmbedder")

	// Which is only a complete list if nothing goes around the helper. A direct
	// parent.postMessage is invisible to a top-level host AND to this check.
	for name, body := range src {
		if name == "views/embed.js" {
			continue
		}
		if strings.Contains(body, "parent.postMessage(") {
			t.Errorf("%s posts to its parent directly — send a host message through postToEmbedder in views/embed.js, "+
				"or a host on the top-level channel never hears it", name)
		}
	}

	// Params: each is read from the address somewhere in the tree.
	for _, p := range declaredEmbed.Params {
		read := false
		for _, body := range src {
			if strings.Contains(body, ".get('"+p.Name+"')") {
				read = true
				break
			}
		}
		if !read {
			t.Errorf("embed.go declares ?%s= and nothing in the web tree reads it", p.Name)
		}
	}

	// Channels: `top` is only a channel if embed.js still turns it on.
	embed := src["views/embed.js"]
	if !strings.Contains(embed, ".get('embed') === '"+embedChannelTop+"'") {
		t.Errorf("embed.go declares the %q channel and views/embed.js no longer reads ?embed=%s", embedChannelTop, embedChannelTop)
	}
}

// The one caller outside this package that depends on the shell's TEXT: the VS
// Code extension decides whether a board understands ?chrome= by searching the
// served HTML for `dataset.chrome` (shellSupportsChrome in aboard_vscode's
// src/board.ts), because until the embed section existed there was nothing in
// /capabilities to ask. Renaming that line would tell every installed extension
// that a current board is too old for it.
func TestTheShellStillSaysDatasetChrome(t *testing.T) {
	if !strings.Contains(webSources(t)["aboard.html"], "document.body.dataset.chrome") {
		t.Error("aboard.html no longer contains `document.body.dataset.chrome`, which the VS Code extension " +
			"searches for to decide whether a board understands ?chrome= — keep the spelling, or change the extension first")
	}
}

func uniqueMatches(re *regexp.Regexp, body string) []string {
	found := re.FindAllStringSubmatch(body, -1)
	out := make([]string, 0, len(found))
	for _, m := range found {
		out = append(out, m[1])
	}
	return dedupe(out)
}

func dedupe(list []string) []string {
	out := slices.Clone(list)
	sort.Strings(out)
	return slices.Compact(out)
}

func assertSameSet(t *testing.T, what string, declared, found []string, declaredWhere, foundWhere string) {
	t.Helper()
	for _, name := range dedupe(declared) {
		if !slices.Contains(found, name) {
			t.Errorf("%s: %q is %s but not %s", what, name, declaredWhere, foundWhere)
		}
	}
	for _, name := range found {
		if !slices.Contains(declared, name) {
			t.Errorf("%s: %q is %s but not %s", what, name, foundWhere, declaredWhere)
		}
	}
}
