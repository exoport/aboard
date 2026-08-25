// One way to finish an inline edit, everywhere.
//
// The complaint that produced this file: "the only way to finish entering a note
// is clicking outside". Blur-to-save works, but it is invisible — nothing on
// screen says the edit is live, nothing says how to commit it, and nothing says
// Escape would have thrown it away. Every editor on the board had invented its
// own version of that silence.
//
// So an inline editor here always shows its exits:
//
//   - a Save button, because a visible affordance beats a remembered gesture;
//   - a Cancel button, so abandoning is as easy as committing;
//   - Enter to save (Shift+Enter for a newline in a multi-line field), Escape to
//     abandon, both stated in a hint beside the buttons;
//   - clicking outside still saves, since that is what people already did.
//
// It also flashes "saved" afterwards. Without it, an edit that persists correctly
// and an edit that vanished look exactly alike — which is the thing that makes
// people distrust a UI they cannot see the inside of.

import { button } from './controls.js';

const STYLE_ID = 'inline-edit-style';

const CSS = `
.inline-edit {
  display: flex;
  flex-direction: column;
  gap: 6px;
  /* Fills whatever it is dropped into. As a flex child (the tab-note strip) it
     would otherwise size to its content and leave most of the row empty, which is
     exactly what it did. */
  width: 100%;
  flex: 1 1 auto;
  min-width: 0;
}
.inline-edit textarea,
.inline-edit input {
  width: 100%;
  font: inherit;
  font-size: 0.86rem;
}
.inline-edit textarea { min-height: 3.4em; resize: vertical; }
.inline-actions { display: flex; align-items: center; gap: 7px; flex-wrap: wrap; }
.inline-actions .inline-hint {
  font-size: 0.74rem;
  color: var(--dim);
  font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
}
.inline-save { border-color: var(--accent-dim); color: var(--accent); }
.inline-save:hover:not(:disabled) { background: var(--accent); color: var(--accent-ink); }
.inline-flash {
  font-size: 0.74rem;
  color: var(--accent);
  opacity: 0;
  transition: opacity 0.25s ease;
}
.inline-flash[data-show="yes"] { opacity: 1; }
`;

function injectStyle() {
  if (document.getElementById(STYLE_ID)) return;
  const style = document.createElement('style');
  style.id = STYLE_ID;
  style.textContent = CSS;
  document.head.append(style);
}

/**
 * Build an inline editor.
 *
 *   value      starting text
 *   multiline  textarea (default) or single-line input
 *   placeholder
 *   saveLabel  what the commit button says ("Save" by default)
 *   onSave(text)   called with the trimmed text; may return a promise
 *   onCancel()     called when abandoned
 *
 * Returns { el, focus() }. Mount `el` where the value was displayed.
 */
export function inlineEditor({
  value = '',
  multiline = true,
  placeholder = '',
  saveLabel = 'Save',
  onSave,
  onCancel,
} = {}) {
  injectStyle();

  const wrap = document.createElement('div');
  wrap.className = 'inline-edit';

  const field = document.createElement(multiline ? 'textarea' : 'input');
  if (!multiline) field.type = 'text';
  field.value = value;
  field.placeholder = placeholder;
  if (multiline) field.rows = Math.min(8, String(value).split('\n').length + 1);

  const actions = document.createElement('div');
  actions.className = 'inline-actions';

  const save = button(saveLabel, '', { className: 'icon-btn inline-save' });
  const cancel = button('Cancel');

  const hint = document.createElement('span');
  hint.className = 'inline-hint';
  hint.textContent = multiline ? 'Enter saves · Shift+Enter newline · Esc cancels' : 'Enter saves · Esc cancels';

  actions.append(save, cancel, hint);
  wrap.append(field, actions);

  // One-shot: a blur that lands after Save has already run must not save twice,
  // and Cancel must win over the blur it causes.
  let done = false;
  const finish = async (commit) => {
    if (done) return;
    done = true;
    if (commit) await onSave?.(field.value.trim());
    else onCancel?.();
  };

  save.addEventListener('click', () => finish(true));
  // pointerdown, not click: the field's blur fires first otherwise, and Cancel
  // would save the very edit it was pressed to discard.
  cancel.addEventListener('pointerdown', (e) => { e.preventDefault(); finish(false); });

  field.addEventListener('keydown', (e) => {
    if (e.key === 'Escape') { e.preventDefault(); finish(false); return; }
    if (e.key === 'Enter' && !e.shiftKey && !e.altKey) { e.preventDefault(); finish(true); }
  });

  field.addEventListener('blur', () => {
    // Let a click on our own buttons land first; only a click genuinely outside
    // should commit through the blur path.
    setTimeout(() => {
      if (wrap.contains(document.activeElement)) return;
      finish(true);
    }, 120);
  });

  return {
    el: wrap,
    focus() {
      field.focus();
      if (field.setSelectionRange) field.setSelectionRange(field.value.length, field.value.length);
    },
  };
}

/** "saved" for a moment, so a change that persisted does not look like one that vanished. */
export function flashSaved(host, text = 'saved') {
  injectStyle();
  const el = document.createElement('span');
  el.className = 'inline-flash';
  el.textContent = text;
  host.append(el);
  requestAnimationFrame(() => { el.dataset.show = 'yes'; });
  setTimeout(() => {
    el.dataset.show = 'no';
    setTimeout(() => el.remove(), 300);
  }, 1400);
}
