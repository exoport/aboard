// Notes view: the board's escape hatch. When nothing else fits — a decision
// log, a summary, a pasted excerpt — an agent opens a notes tab and the human
// edits it as free text.
//
// `state.markdown: true` renders it instead of showing it raw, with a Read/Edit
// toggle. Opt-in per tab, because plain is right for a paste or a log and wrong
// for a report: most of what an agent writes for a human is prose WITH
// structure, and a wall of unrendered hashes and dashes is the worst of both.
// The toggle is per-viewer state (never aboard.json) and the textarea is
// untouched underneath — this changes how a note reads, not what it is.

import { renderMarkdown, injectMarkdownStyle } from './markdown.js';
import { controlsFor } from './controls.js';

const ctl = controlsFor('notes');

let styleInjected = false;

function injectStyle() {
  if (styleInjected) return;
  styleInjected = true;
  const style = document.createElement('style');
  style.textContent = `
    [data-view="notes"] .notes-head { display: flex; align-items: baseline; justify-content: space-between; gap: 10px; flex-wrap: wrap; }
    [data-view="notes"] .save-note { font-size: 0.79rem; color: var(--accent); opacity: 0; transition: opacity 0.4s ease; }
    [data-view="notes"] .save-note.show { opacity: 1; }
    [data-view="notes"] .save-note.err { color: var(--danger); }
    [data-view="notes"] .copy-note { font-size: 0.79rem; color: var(--muted); }
    [data-view="notes"] .external-note {
      display: none; align-items: center; gap: 10px; margin: 0 0 10px; padding: 8px 12px;
      border: 1px solid var(--line); border-left: 3px solid var(--agent); border-radius: 3px;
      background: var(--sunken); font-size: 0.83rem;
    }
    [data-view="notes"] .external-note[data-visible="yes"] { display: flex; }
    [data-view="notes"] .notes-preview {
      min-height: 55vh;
      padding: 14px 16px;
      background: var(--sunken);
      border: 1px solid var(--line);
      border-radius: 4px;
      overflow-wrap: anywhere;
    }
    [data-view="notes"] [hidden] { display: none !important; }
    [data-view="notes"] textarea.notes-area {
      display: block; width: 100%; min-height: 55vh;
      font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
      font-size: 0.88rem; line-height: 1.65;
    }
  `;
  document.head.append(style);
}

export function mountNotes(root, ctx) {
  injectStyle();

  const getText = () => {
    const t = ctx.state && ctx.state.text;
    return typeof t === 'string' ? t : '';
  };

  const panel = document.createElement('div');
  panel.className = 'panel';

  const head = document.createElement('div');
  head.className = 'notes-head';
  const heading = document.createElement('h2');
  heading.className = 'panel-head';
  heading.textContent = (ctx.tab && ctx.tab.name) || 'Notes';
  const saveNote = document.createElement('span');
  saveNote.className = 'save-note';
  head.append(heading, saveNote);

  const externalNotice = document.createElement('div');
  externalNotice.className = 'external-note';
  const externalText = document.createElement('span');
  externalText.textContent = 'The agent updated this note while you were typing.';
  const reloadBtn = ctl('reload');
  externalNotice.append(externalText, reloadBtn);

  // Words / lines / chars, built identically save for the label.
  const toolbar = document.createElement('div');
  toolbar.className = 'toolbar';
  const stats = {};
  for (const key of ['words', 'lines', 'chars']) {
    const l = document.createElement('span');
    l.className = 'toolbar-label';
    l.textContent = key;
    const c = document.createElement('span');
    c.className = 'mono';
    stats[key] = c;
    toolbar.append(l, c);
  }
  const copyBtn = ctl('copy');
  const copyNote = document.createElement('span');
  copyNote.className = 'copy-note';
  toolbar.append(copyBtn, copyNote);

  // Rendered when state.markdown is on. Read at render time, so an agent can
  // turn it on under an open page and the live reload picks it up.
  const asMarkdown = () => ctx.state && ctx.state.markdown === true;
  injectMarkdownStyle();

  const preview = document.createElement('div');
  preview.className = 'md notes-preview';

  const modeBtn = ctl('mode');
  toolbar.append(modeBtn);

  // Per-viewer, never persisted: which pane you are looking at is yours, and
  // writing it to aboard.json would flip it for everyone else.
  let editing = false;

  const textarea = document.createElement('textarea');
  textarea.className = 'notes-area';
  textarea.setAttribute('aria-label', ((ctx.tab && ctx.tab.name) || 'Notes') + ' text');
  textarea.placeholder = 'Anything that does not fit another tab — a decision log, a summary, a paste.';
  textarea.value = getText();

  panel.append(head, externalNotice, toolbar, preview, textarea);
  root.append(panel);

  function paintMode() {
    const md = asMarkdown();
    modeBtn.hidden = !md;
    modeBtn.textContent = editing ? 'Read' : 'Edit';
    modeBtn.title = editing ? 'Show the rendered note' : 'Edit the markdown source';
    const showPreview = md && !editing;
    preview.hidden = !showPreview;
    textarea.hidden = showPreview;
    if (showPreview) preview.replaceChildren(renderMarkdown(textarea.value));
  }

  modeBtn.addEventListener('click', () => {
    editing = !editing;
    paintMode();
    if (editing) textarea.focus();
  });

  let saveTimer = null;
  let copyTimer = null;
  let pendingExternal = null; // external text queued behind the "reload text" affordance

  function updateCounts(value) {
    stats.words.textContent = String(value.trim() ? value.trim().split(/\s+/).length : 0);
    stats.lines.textContent = String(value ? value.split('\n').length : 0);
    stats.chars.textContent = String(value.length);
  }

  function flashSaved(ok) {
    clearTimeout(saveTimer);
    saveNote.textContent = ok ? 'Saved' : 'Save failed — check the server';
    saveNote.classList.toggle('err', !ok);
    saveNote.classList.add('show');
    saveTimer = setTimeout(() => saveNote.classList.remove('show'), 2000);
  }

  function commit() {
    ctx.state.text = textarea.value;
    updateCounts(textarea.value);
    if (asMarkdown() && !editing) paintMode();
    ctx.save().then(flashSaved).catch(() => flashSaved(false));
  }

  textarea.addEventListener('input', commit);

  // Tab indents (this is a writing surface, not a form) — but a lone Escape
  // arms one Tab press to leave normally, so a keyboard user is never stuck.
  let escapeArmed = false;
  textarea.addEventListener('keydown', (e) => {
    if (e.key === 'Escape') { escapeArmed = true; return; }
    if (e.key === 'Tab') {
      if (escapeArmed) { escapeArmed = false; return; }
      e.preventDefault();
      const { selectionStart: start, selectionEnd: end, value } = textarea;
      textarea.value = value.slice(0, start) + '  ' + value.slice(end);
      textarea.selectionStart = textarea.selectionEnd = start + 2;
      commit();
      return;
    }
    escapeArmed = false;
  });

  copyBtn.addEventListener('click', async () => {
    clearTimeout(copyTimer);
    try {
      await navigator.clipboard.writeText(textarea.value);
      copyNote.textContent = 'Copied';
    } catch {
      copyNote.textContent = 'Copy failed — select the text manually';
    }
    copyTimer = setTimeout(() => { copyNote.textContent = ''; }, 2000);
  });

  reloadBtn.addEventListener('click', () => {
    if (pendingExternal === null) return;
    const scrollTop = textarea.scrollTop;
    textarea.value = pendingExternal;
    updateCounts(pendingExternal);
    pendingExternal = null;
    externalNotice.dataset.visible = 'no';
    textarea.scrollTop = scrollTop;
    paintMode();
    textarea.focus();
  });

  // A rewrite from an agent must never eat what the human is mid-sentence on.
  function refresh() {
    const incoming = getText();
    if (incoming === textarea.value) {
      pendingExternal = null;
      externalNotice.dataset.visible = 'no';
      paintMode();          // state.markdown may have been flipped on its own
      return;
    }
    if (document.activeElement === textarea) {
      pendingExternal = incoming;
      externalNotice.dataset.visible = 'yes';
      return;
    }
    const scrollTop = textarea.scrollTop;
    textarea.value = incoming;
    updateCounts(incoming);
    textarea.scrollTop = scrollTop;
    pendingExternal = null;
    externalNotice.dataset.visible = 'no';
    paintMode();
  }

  updateCounts(textarea.value);
  paintMode();
  return { refresh };
}
