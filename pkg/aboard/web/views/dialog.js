// dialog.js — the board's own confirm() and prompt(), because the host's are not
// always there.
//
// A VS Code webview — and any <iframe> sandboxed without `allow-modals` — does
// not merely restyle window.alert/confirm/prompt. It SUPPRESSES them: confirm()
// returns false, prompt() returns null, nothing is drawn, nothing is logged and
// nothing throws. Every gesture guarded by one is dead inside the panel, and the
// only symptom is the one the human reported on 2026-08-26 about the removal
// banner's Remove button: "I clicked it but nothing happens".
//
// So the board never asks the host to ask. It draws the question itself, in the
// same <dialog class="sheet-dialog"> the new-tab sheet, the dag's delete-confirm
// and markup's clear-marks modal already use — one pattern, extended, rather
// than a second one invented beside it. A <dialog> is not affected by
// `allow-modals`; only the three window functions are.
//
// Both buttons go through button() from controls.js rather than
// controlsFor(type): a dialog's OK and Cancel are chrome belonging to no
// renderer, which is exactly the case that file documents for the plain helper.
// There is no capability here for an agent to learn about and nothing to declare
// in a spec — what the agent cares about is the control that OPENED the dialog,
// and that one is declared where it lives.
//
// Keys: Escape cancels, Enter confirms. Escape is handled here as well as by the
// element, because the shell already distrusts the native `cancel` event inside
// a webview (see the Escape guard in aboard.html's keydown handler) and a modal
// nobody can dismiss is the worst thing this file could ship.

import { button } from './controls.js';

const STYLE_ID = 'board-dialog-style';

const CSS = `
/* Its own layout rather than the .sheet-dialog form rule, because this dialog
   deliberately has no form element in it. See the comment on sheet(). */
.board-dialog .dialog-sheet { display: flex; flex-direction: column; gap: 12px; }
.board-dialog p { margin: 0; }
.board-dialog .dialog-head {
  font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
  font-size: 0.72rem;
  letter-spacing: 0.06em;
  text-transform: uppercase;
  color: var(--muted);
}
.board-dialog .dialog-body { display: flex; flex-direction: column; gap: 9px; }
.board-dialog .dialog-body p { line-height: 1.45; }
.board-dialog .dialog-hint {
  font-size: 0.74rem;
  color: var(--dim);
  font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
  margin-right: auto;
}
.board-dialog input[type="text"] { width: 100%; font: inherit; font-size: 0.9rem; }
`;

function injectStyle() {
  if (document.getElementById(STYLE_ID)) return;
  const style = document.createElement('style');
  style.id = STYLE_ID;
  style.textContent = CSS;
  document.head.append(style);
}

/**
 * The shared skeleton. `fill` gets the dialog's body and returns a reader for
 * the value the promise resolves to when the question is ANSWERED; `cancelled`
 * is what it resolves to on Cancel, Escape, or the dialog being closed any other
 * way.
 *
 * **There is no <form> in here, and that is the point of the file.** The obvious
 * shape — a form, a submit button, Enter for free — needs `allow-forms` on the
 * frame, and this dialog exists precisely because the board is embedded in
 * frames whose sandbox we do not control. A host that grants `allow-scripts` and
 * nothing else would have swallowed the submit exactly as it swallows
 * confirm(), and we would have replaced one silent nothing with another. So the
 * OK button is an ordinary button with a click handler, and Enter is a keydown.
 *
 * One dialog element per call, appended to <body> and removed on close. Nothing
 * is kept between calls, so there is no shared pending-handler to leak — the
 * stale-callback bug markup.js guards against cannot happen here.
 */
function sheet({ head, fill, ok, cancel, danger, cancelled, focus }) {
  injectStyle();

  const dlg = document.createElement('dialog');
  dlg.className = 'sheet-dialog board-dialog';
  dlg.setAttribute('data-dialog', 'board');

  const sheetEl = document.createElement('div');
  sheetEl.className = 'dialog-sheet';

  if (head) {
    const h = document.createElement('p');
    h.className = 'dialog-head';
    h.textContent = head;
    sheetEl.append(h);
  }

  const body = document.createElement('div');
  body.className = 'dialog-body';
  sheetEl.append(body);
  const read = fill(body);

  const actions = document.createElement('div');
  actions.className = 'dialog-actions';

  const hint = document.createElement('span');
  hint.className = 'dialog-hint';
  hint.textContent = 'Enter to confirm · Esc to cancel';
  actions.append(hint);

  const cancelBtn = button(cancel || 'Cancel', 'Close without doing anything');
  const okBtn = button(ok || 'OK', '', {
    className: danger ? 'icon-btn icon-btn--danger' : 'primary-btn',
  });
  actions.append(cancelBtn, okBtn);
  sheetEl.append(actions);
  dlg.append(sheetEl);
  document.body.append(dlg);

  // Whatever had the keyboard before the question was asked. showModal() makes
  // the rest of the document inert, which is the focus TRAP; putting the caret
  // back afterwards is ours, because a dialog we close from our own Escape
  // handler is not a close the browser restores focus for.
  const returnFocus = document.activeElement;

  return new Promise((resolve) => {
    let settled = false;
    const finish = (value) => {
      if (settled) return;
      settled = true;
      // Order matters: close() first so the top layer is gone, then focus, then
      // remove the element — refocusing while the modal is still up is ignored.
      if (dlg.open) dlg.close();
      dlg.remove();
      if (returnFocus && typeof returnFocus.focus === 'function' && returnFocus.isConnected) {
        returnFocus.focus();
      }
      resolve(value);
    };

    cancelBtn.addEventListener('click', () => finish(cancelled));
    okBtn.addEventListener('click', () => finish(read()));
    // Escape via the element's own cancel event, and again from keydown. Either
    // one alone is enough in a plain browser; neither is something to bet a
    // trap-free modal on inside a webview, and a modal nobody can dismiss is the
    // worst thing this file could ship.
    dlg.addEventListener('cancel', (e) => { e.preventDefault(); finish(cancelled); });
    dlg.addEventListener('close', () => finish(cancelled));
    dlg.addEventListener('keydown', (e) => {
      // The modal owns the keyboard while it is up, which is the sentence the
      // shell's own help panel already writes for itself (`if ($help.open)
      // return`). It has to be said HERE because window.confirm took the
      // keyboard away from the page entirely and a <dialog> does not: without
      // this line every key pressed over the question still bubbles to the
      // shell's document-level handler, so `]` and `1`-`9` switch the tab
      // behind the modal, `?` stacks the help panel on top of it, and every
      // other key reaches the active renderer's onKey. showModal() makes the
      // rest of the document inert for POINTERS and focus; it does nothing
      // about a listener bound to document.
      e.stopPropagation();
      if (e.key === 'Escape') { e.preventDefault(); finish(cancelled); return; }
      if (e.key !== 'Enter') return;
      // A focused button already answers Enter by clicking itself; confirming
      // here as well would run BOTH, which on the removal dialog means Cancel
      // and then Remove.
      if (e.target instanceof HTMLButtonElement) return;
      e.preventDefault();
      finish(read());
    });

    dlg.showModal();
    const target = focus ? focus() : okBtn;
    if (target && typeof target.focus === 'function') target.focus();
  });
}

/**
 * askConfirm(message, { ok, cancel, head, danger }) → Promise<boolean>
 *
 * The replacement for window.confirm. A message with blank-line-separated
 * paragraphs keeps them: the removal question is two sentences and the second
 * one names the tabs about to be emptied, which a native confirm ran together.
 *
 * Enter confirms, matching window.confirm's own default — including for a
 * destructive question, where the safer-looking choice (default to Cancel) would
 * mean the same keystroke did opposite things in two dialogs that look alike.
 */
export function askConfirm(message, opts = {}) {
  return sheet({
    head: opts.head,
    ok: opts.ok || 'OK',
    cancel: opts.cancel || 'Cancel',
    danger: !!opts.danger,
    cancelled: false,
    fill: (body) => {
      for (const para of String(message).split('\n')) {
        if (!para.trim()) continue;
        const p = document.createElement('p');
        p.textContent = para.trim();
        body.append(p);
      }
      return () => true;
    },
  });
}

/**
 * askPrompt(label, initial, { ok, cancel, head, placeholder, maxLength })
 *   → Promise<string|null>
 *
 * The replacement for window.prompt, and it keeps prompt's contract exactly:
 * null means cancelled, a string means answered — including the empty string.
 * Callers already distinguish the two (renaming a tab to "" is refused, but
 * cancelling must not be mistaken for it), so collapsing them would be a silent
 * behaviour change dressed as a refactor.
 */
export function askPrompt(label, initial = '', opts = {}) {
  let input = null;
  return sheet({
    head: opts.head,
    ok: opts.ok || 'Save',
    cancel: opts.cancel || 'Cancel',
    cancelled: null,
    // The caret goes in the box with the current value SELECTED, which is what
    // window.prompt did: the common case is replacing the name, not appending to
    // it. Returns null because it has already focused what it wanted.
    focus: () => { input.focus(); input.select(); return null; },
    fill: (body) => {
      const field = document.createElement('label');
      field.className = 'field';
      const caption = document.createElement('span');
      caption.textContent = label;
      input = document.createElement('input');
      input.type = 'text';
      input.value = initial == null ? '' : String(initial);
      if (opts.placeholder) input.placeholder = String(opts.placeholder);
      if (opts.maxLength) input.maxLength = Number(opts.maxLength);
      field.append(caption, input);
      body.append(field);
      return () => input.value;
    },
  });
}
