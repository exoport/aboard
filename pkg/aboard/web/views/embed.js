// embed.js — who this board's HOST is, and the one way to talk to it.
//
// A host is whatever shows the board inside something of its own and wants to
// hear from it: a VS Code panel, a workbench's side view. There are two places
// such a host can be, and they are the two CHANNELS the board speaks:
//
//   frame  the board is in an <iframe> and the host is its parent. Always on,
//          because being framed is already the host saying so.
//   top    the board is the top-level page of a view the host created, and the
//          host runs script in that same page. Only under `?embed=top`: a plain
//          browser tab is top level too, and it has nobody to talk to.
//
// Moonwatcher asked for `top` on 2026-09-13. It creates a native WebKit view,
// injects its own script into the page, and until then had to wrap the board in
// an iframe of its own purely to have a parent to be — which cost it a second copy
// of every per-viewer setting (storage is partitioned by the top-level origin) and
// every html tab (frame-ancestors refused its scheme).
//
// The trust is the SAME rule on both channels: a message is accepted when its
// `e.source` is the host's window, and the host's window is the parent when framed
// and this window itself at top level. A sandboxed `html` tab is an opaque-origin
// frame; it can post to `window.top`, but its messages arrive with ITS frame as the
// source, so it can reach neither. What does pass `e.source === window` is script
// running in this page — the board's own, or the host's injected script — and
// either of those could already press any button on the board.
//
// Every message a renderer sends a host goes through postToEmbedder, and the
// declared list of them in pkg/aboard/embed.go is checked against its call sites.

const TOP = new URLSearchParams(location.search).get('embed') === 'top';

/**
 * The host's window, or null when nobody is hosting this board.
 * @returns {Window|null}
 */
export function embedderWindow() {
  if (window.parent !== window) return window.parent;
  return TOP ? window : null;
}

/**
 * Is this message from the host? The one authentication an embedder message gets.
 * @param {MessageEvent} e
 */
export function fromEmbedder(e) {
  const host = embedderWindow();
  // `host` first: a message with no source (a MessagePort, a service worker) has
  // e.source === null, and so does an unhosted board's embedderWindow().
  return host !== null && e.source === host;
}

/**
 * Send one `{__aboard: …}` message to the host. Returns false when there is no
 * host to send it to, or the post threw.
 * @param {object} msg
 */
export function postToEmbedder(msg) {
  const host = embedderWindow();
  if (!host) return false;
  // '*' because a host's origin is not knowable in advance — a VS Code webview is
  // vscode-webview://<uuid> — and the receiver authenticates by source, not origin.
  try { host.postMessage(msg, '*'); return true; } catch { return false; }
}
