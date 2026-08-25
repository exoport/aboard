# vendor/

Third-party code, committed rather than installed. There is no `package.json`
anywhere in this project and nothing to `npm install`.

## mermaid.min.js

| | |
|---|---|
| version | **11.17.0** |
| source | `https://cdn.jsdelivr.net/npm/mermaid@11/dist/mermaid.min.js` |
| size | 3.5 MB |
| sha256 | `8d8e0eec56d3a83b4b3c87f42050845546dee93ebe1875d2117c12e6947c0cb3` |
| licence | MIT |
| loaded by | `views/diagram.js`, injected as a classic `<script>` on first use |

It is a UMD/IIFE build that sets `globalThis.mermaid` — **not** an ES module, so
`import` does not work on it.

Vendored rather than pulled from a CDN so the Diagram tab renders with no network
and the board keeps working offline. It is also the only reason this directory
exists.

### What it drags in

The bundle is self-contained, with mermaid's own dependency tree already inlined.
Detected inside it:

d3 · dagre · elkjs · cytoscape · DOMPurify 3.4.12 · marked · khroma · dayjs ·
roughjs · lodash helpers · uuid

So the honest count is *one* file to trust, holding a dozen upstream libraries.
Nothing in it is reachable at runtime except what `views/diagram.js` calls.

### Updating

```sh
curl -sL -o vendor/mermaid.min.js https://cdn.jsdelivr.net/npm/mermaid@11/dist/mermaid.min.js
sha256sum vendor/mermaid.min.js          # record the new hash above
./test/smoke.sh                          # views still mount
# then open /test/mermaid-probe.html to confirm which diagram types still render
```

The probe matters on upgrade: diagram types move in and out of beta between
minor releases.
