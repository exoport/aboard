// A markdown subset, rendered to DOM nodes.
//
// Not a markdown engine and deliberately not vendored: a library would be
// another 50-200 KB inside the binary for the one thing this board actually
// needs — an agent's report reading as prose instead of as a wall of plain text.
// Headings, lists, code, quotes, rules, links, emphasis. That is the whole set.
//
// Everything here builds ELEMENTS. Nothing is ever assembled as an HTML string
// and assigned to innerHTML, because every input is agent- or human-authored
// state: with innerHTML, a note containing markup would execute as markup. Link
// hrefs are scheme-checked for the same reason — a `javascript:` URL in a note
// must render as text, not as a live link.
//
// ONE exception, and it is not this file's to make: a ```mermaid fence is handed
// to diagram.js, which assigns mermaid's own svg output as markup under
// `securityLevel: 'strict'`. That is the same trust the diagram TAB has always
// placed in the vendored bundle; the fence does not widen it, and re-parsing the
// bundle's output here would not narrow it either.
//
// Anything unrecognised falls through as literal text, which is the right
// failure for a subset: a note is never worse off than the plain textarea it
// replaced.

import { renderMermaidInto } from './diagram.js';

const SAFE_HREF = /^(https?:\/\/|mailto:|\/|\.\/|#)/i;

/** Inline spans: `code`, **strong**, *em*, _em_, [text](href). */
function inlineInto(parent, text) {
  // One pass, longest-delimiter-first, so **bold** is not read as two *em*.
  const pattern = /(`[^`\n]+`)|(\*\*[^*\n]+\*\*)|(\*[^*\n]+\*)|(_[^_\n]+_)|(\[[^\]\n]+\]\([^)\s]+\))/;
  let rest = String(text);

  while (rest) {
    const hit = pattern.exec(rest);
    if (!hit) { parent.append(document.createTextNode(rest)); return; }

    if (hit.index > 0) parent.append(document.createTextNode(rest.slice(0, hit.index)));
    const token = hit[0];

    if (token.startsWith('`')) {
      const code = document.createElement('code');
      code.textContent = token.slice(1, -1);
      parent.append(code);
    } else if (token.startsWith('**')) {
      const strong = document.createElement('strong');
      strong.textContent = token.slice(2, -2);
      parent.append(strong);
    } else if (token.startsWith('*') || token.startsWith('_')) {
      const em = document.createElement('em');
      em.textContent = token.slice(1, -1);
      parent.append(em);
    } else {
      const split = token.indexOf('](');
      const label = token.slice(1, split);
      const href = token.slice(split + 2, -1);
      if (SAFE_HREF.test(href)) {
        const a = document.createElement('a');
        a.href = href;
        a.textContent = label;
        a.rel = 'noreferrer noopener';
        if (/^https?:/i.test(href)) a.target = '_blank';
        parent.append(a);
      } else {
        // Not a scheme we will make clickable: show what it said, plainly.
        parent.append(document.createTextNode(token));
      }
    }
    rest = rest.slice(hit.index + token.length);
  }
}

function heading(line) {
  const hit = /^(#{1,4})\s+(.*)$/.exec(line);
  if (!hit) return null;
  const el = document.createElement('h' + Math.min(6, hit[1].length + 1));
  inlineInto(el, hit[2]);
  return el;
}

function codeBlock(body) {
  const pre = document.createElement('pre');
  const code = document.createElement('code');
  code.textContent = body;
  pre.append(code);
  return pre;
}

/**
 * A ```mermaid fence, drawn as a figure.
 *
 * The board vendors mermaid and, until now, a diagram could only be a whole tab
 * — so a write-up with one figure in it had to be two tabs, and the figure could
 * not travel with the prose being promoted into a document.
 *
 * Rendered through diagram.js's loader and theme, never a copy of either: two
 * theme maps drift, which is exactly how the html tab ended up with a palette
 * that had lost five tokens.
 *
 * The SOURCE is what the element holds until a render succeeds, so the failure
 * state is the mermaid text verbatim — never an empty box, and never a stack
 * trace. That covers a syntax error and a bundle that would not load, which are
 * the same thing from the reader's side: no picture, so show the words.
 */
function mermaidBlock(source) {
  const figure = document.createElement('div');
  figure.className = 'md-mermaid';
  figure.dataset.mermaid = source;
  figure.append(codeBlock(source));
  if (source.trim()) {
    renderMermaidInto(figure, source).catch(() => { /* the verbatim source stays */ });
  }
  return figure;
}

// A fence renders to an SVG with the token values baked into it, so a theme
// switch cannot reach it by changing a custom property — the same problem the
// diagram TAB has, in a place with no mount and no handle for the shell to call.
//
// Hence one document-level listener for the whole module rather than one per
// figure: a notes tab can hold a dozen fences, they come and go on every
// re-render of the markdown, and a listener per figure would be a leak per
// figure. It walks what is ON SCREEN when the theme changes, which is the only
// set that matters — the source is kept on the element, so nothing has to be
// remembered anywhere else.
document.addEventListener('aboard:theme', () => {
  for (const figure of document.querySelectorAll('.md-mermaid[data-mermaid]')) {
    const source = figure.dataset.mermaid || '';
    if (!source.trim()) continue;
    renderMermaidInto(figure, source).catch(() => { /* leave the last good render */ });
  }
});

/** Build a fragment for `text`. Block-level scan, line by line. */
export function renderMarkdown(text) {
  const frag = document.createDocumentFragment();
  const lines = String(text == null ? '' : text).split('\n');
  let i = 0;

  const flushParagraph = (buf) => {
    if (!buf.length) return;
    const p = document.createElement('p');
    inlineInto(p, buf.join('\n'));
    frag.append(p);
    buf.length = 0;
  };

  const para = [];

  while (i < lines.length) {
    const line = lines[i];

    // Fenced code: verbatim until the closing fence or the end of the text.
    // The INFO STRING is kept rather than thrown away, because one value of it
    // means something here: ```mermaid renders as a diagram.
    const fence = /^```(.*)$/.exec(line);
    if (fence) {
      flushParagraph(para);
      const info = fence[1].trim().toLowerCase();
      const body = [];
      i += 1;
      while (i < lines.length && !/^```/.test(lines[i])) { body.push(lines[i]); i += 1; }
      i += 1;                                     // consume the closing fence
      frag.append(info === 'mermaid' ? mermaidBlock(body.join('\n')) : codeBlock(body.join('\n')));
      continue;
    }

    if (!line.trim()) { flushParagraph(para); i += 1; continue; }

    if (/^(-{3,}|\*{3,}|_{3,})\s*$/.test(line)) {
      flushParagraph(para);
      frag.append(document.createElement('hr'));
      i += 1;
      continue;
    }

    const head = heading(line);
    if (head) { flushParagraph(para); frag.append(head); i += 1; continue; }

    // Lists: a run of adjacent item lines, either kind. Nesting is out of scope
    // — two spaces of indent reads as part of the same item.
    const bullet = /^\s*[-*+]\s+(.*)$/;
    const number = /^\s*\d+[.)]\s+(.*)$/;
    if (bullet.test(line) || number.test(line)) {
      flushParagraph(para);
      const ordered = !bullet.test(line);
      const list = document.createElement(ordered ? 'ol' : 'ul');
      while (i < lines.length) {
        const item = (ordered ? number : bullet).exec(lines[i]);
        if (!item) break;
        const li = document.createElement('li');
        inlineInto(li, item[1]);
        list.append(li);
        i += 1;
        // A continuation line (indented, not a new item) joins the same item.
        while (i < lines.length && /^\s{2,}\S/.test(lines[i]) &&
               !bullet.test(lines[i]) && !number.test(lines[i])) {
          li.append(document.createTextNode(' '));
          inlineInto(li, lines[i].trim());
          i += 1;
        }
      }
      frag.append(list);
      continue;
    }

    if (/^>\s?/.test(line)) {
      flushParagraph(para);
      const quote = document.createElement('blockquote');
      const buf = [];
      while (i < lines.length && /^>\s?/.test(lines[i])) { buf.push(lines[i].replace(/^>\s?/, '')); i += 1; }
      inlineInto(quote, buf.join('\n'));
      frag.append(quote);
      continue;
    }

    para.push(line);
    i += 1;
  }

  flushParagraph(para);
  return frag;
}

/** Shared styling for rendered markdown, injected once. */
export function injectMarkdownStyle() {
  const ID = 'markdown-style';
  if (document.getElementById(ID)) return;
  const style = document.createElement('style');
  style.id = ID;
  style.textContent = `
    .md { line-height: 1.6; }
    .md > :first-child { margin-top: 0; }
    .md > :last-child { margin-bottom: 0; }
    .md h2, .md h3, .md h4, .md h5 { margin: 1.3em 0 0.4em; line-height: 1.3; color: var(--text); }
    .md h2 { font-size: 1.08rem; }
    .md h3 { font-size: 0.98rem; }
    .md h4, .md h5 { font-size: 0.9rem; color: var(--muted); text-transform: uppercase; letter-spacing: 0.06em; }
    .md p { margin: 0 0 0.75em; }
    .md ul, .md ol { margin: 0 0 0.8em; padding-left: 1.4em; }
    .md li { margin-bottom: 0.3em; }
    .md li::marker { color: var(--accent); }
    .md code {
      font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
      font-size: 0.86em;
      background: var(--sunken);
      border: 1px solid var(--line);
      border-radius: 3px;
      padding: 0.5px 4px;
    }
    .md pre {
      margin: 0 0 0.9em;
      padding: 10px 12px;
      background: var(--sunken);
      border: 1px solid var(--line);
      border-radius: 4px;
      overflow-x: auto;
    }
    .md pre code { border: 0; background: none; padding: 0; font-size: 0.84rem; line-height: 1.5; }
    .md blockquote {
      margin: 0 0 0.9em;
      padding: 2px 0 2px 12px;
      border-left: 2px solid var(--agent);
      color: var(--muted);
    }
    .md hr { border: 0; border-top: 1px solid var(--line); margin: 1.2em 0; }
    /* A rendered fence. Same well as a code block, because that is what it was
       before it rendered and what it goes back to if mermaid cannot read it. */
    .md .md-mermaid {
      margin: 0 0 0.9em;
      padding: 10px 12px;
      background: var(--sunken);
      border: 1px solid var(--line);
      border-radius: 4px;
      overflow-x: auto;
    }
    .md .md-mermaid pre { margin: 0; padding: 0; border: 0; background: none; }
    .md .md-mermaid svg { display: block; max-width: 100%; height: auto; }
    .md a { color: var(--focus); }
    .md strong { color: var(--text); font-weight: 650; }
  `;
  document.head.append(style);
}
