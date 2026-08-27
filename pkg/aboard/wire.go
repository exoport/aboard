// wire.go — the words this board puts on a wire, named once each.
//
// Three vocabularies live here and they are kept SEPARATE on purpose, even where
// two of them spell a word identically:
//
//	key*    keys inside the board document (.aboard/aboard.json). Read by every
//	        renderer, by `apply`, by the merge, and by the browser. Changing one
//	        is a schema change.
//	wire*   keys in the JSON the HTTP API itself speaks — the reply envelopes,
//	        the two small request payloads, and the query parameters that carry
//	        the same word. Changing one is an API change.
//	route*  paths in the router. The switch in route() is still the
//	        implementation; declaredRoutes describes it, and both spell the path
//	        from here so a rename cannot move only one of them.
//
// `at`, `by`, `note`, `type` and `reason` therefore appear twice. Collapsing them
// into one constant apiece would be shorter and would say something false: a
// board document and an HTTP reply are two contracts with two audiences and two
// reasons to change, and one constant would let a rename of either silently
// rename the other. The duplication is the boundary, written down.
//
// Only the goconst-flagged repeats plus their immediate siblings live here. A key
// that appears once, at the one place that owns it, stays a literal — a constant
// used once moves the reader further from the value without protecting anything.

package aboard

// Keys of the board document. Five of them — `version`, `nextId`, `rev`,
// `updatedAt` and `lastEditedBy` — are stamped by the server on every accepted
// write and are not the caller's to set (see commitState); `tabs` is the
// document's body; the rest are per-tab or per-object fields the renderers read.
const (
	keyTabs         = "tabs"
	keyVersion      = "version"
	keyNextID       = "nextId"
	keyRev          = "rev"
	keyUpdatedAt    = "updatedAt"
	keyLastEditedBy = "lastEditedBy"

	keyName      = "name"
	keyType      = "type"
	keyNote      = "note"
	keyStateFrom = "stateFrom"
	keyRequests  = "requests"
	keyBy        = "by"
	keyAt        = "at"
	keyReason    = "reason"
)

// Keys of the HTTP API's own JSON: what a reply is made of, the fields of the two
// payloads it accepts (a poke, a mount receipt), and the query parameters that
// carry the same word into a handler.
const (
	wireOK     = "ok"
	wireError  = "error"
	wireReason = "reason"
	wireBytes  = "bytes"
	wireType   = "type"
	wireAt     = "at"
	wireBy     = "by"
	wireNote   = "note"
)

// msgTabPlainID is the refusal three handlers share, because they share the rule:
// a tab id becomes a filename or a map key, so it is validated rather than
// sanitised (logTabRe). One sentence, so the human sees the same words whichever
// endpoint they hit.
const msgTabPlainID = "tab must be a plain id"

// Routes named where more than one place spells them: the router, the declared
// manifest that describes the router, and the client that calls back into it.
const (
	routeState = "/aboard.json"
	routeLog   = "/log"
	routeTheme = "/theme.json"
)
