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
// Anything unrecognised falls through as literal text, which is the right
// failure for a subset: a note is never worse off than the plain textarea it
// replaced.

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
    if (/^```/.test(line)) {
      flushParagraph(para);
      const body = [];
      i += 1;
      while (i < lines.length && !/^```/.test(lines[i])) { body.push(lines[i]); i += 1; }
      i += 1;                                     // consume the closing fence
      const pre = document.createElement('pre');
      const code = document.createElement('code');
      code.textContent = body.join('\n');
      pre.append(code);
      frag.append(pre);
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
    .md a { color: var(--focus); }
    .md strong { color: var(--text); font-weight: 650; }
  `;
  document.head.append(style);
}
