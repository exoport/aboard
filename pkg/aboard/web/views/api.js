// api.js — every browser→server URL is built here, and nowhere else.
//
// The board can be served under a prefix (`aboard serve --base-path /aboard`), so
// a root-absolute literal like '/aboard.json' is wrong the moment it is. The
// server injects the prefix into the shell as window.ABOARD_BASE and this is the
// one place that reads it.
//
// The global is ABOARD_BASE. It was spelt that way one commit before the rename
// landed, when everything around it still said `board` — a new global named for
// where the project was going rather than where it came from, so that this file
// needed no edit here at all.
//
// Static things — the stylesheet link, the module imports — stay RELATIVE
// instead, because an `import` specifier cannot be built at runtime. That works
// because the server redirects `<base>` to `<base>/`, so the document URL always
// ends in a slash or in aboard.html, and a relative specifier resolves the same
// either way.

// Normalised once: no trailing slash, so api('/x') never produces '//x'.
export const BASE = (() => {
  const raw = (typeof window !== 'undefined' && typeof window.ABOARD_BASE === 'string')
    ? window.ABOARD_BASE : '';
  return raw.replace(/\/+$/, '');
})();

/**
 * Build a server URL from a root-relative path.
 * @param {string} path e.g. '/aboard.json', '/tab/bb72/html', 'assets/x.svg'
 * @returns {string}
 */
export function api(path) {
  const p = String(path || '');
  return BASE + (p.startsWith('/') ? p : '/' + p);
}
