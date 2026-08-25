// Gate view: an approval queue. The board as a place to say yes, no, or
// "not like that — like this".
//
// This is the one pattern the whole ecosystem converged on separately —
// HumanLayer's require_approval, ACP's session/request_permission, LangChain's
// approve / edit / reject / respond, MCP elicitation's accept / reject / cancel.
// Four verdicts, and the valuable one is EDIT: a veto tells an agent it was
// wrong, an edit tells it what right looks like.
//
//   state = {
//     pending: [{ id, title, detail?, command?, risk?: 'low'|'medium'|'high', asked?, by? }],
//     decided: [{ id, title, verdict, reason?, editedTo?, at, by }]
//   }
//
// Two things make it honest rather than decorative:
//
//   - Nothing here executes. A decision is a record; the agent that asked reads
//     it and acts. A queue that could act on its own would be a permission
//     system, and this server has no auth.
//   - A reason can be added AFTER the verdict. People decide fast — click Allow,
//     move on — and the reason is the half that evaporates and the half that stops
//     the argument recurring later. Freezing the record at the moment of the click
//     meant an unreasoned decision stayed unreasoned forever. A late reason is
//     stamped as late, because one written a week afterwards is reconstructed, not
//     recorded, and it will read with more confidence than it has earned.
//   - It pairs with waiting. `./board -wait -for "answer <tabId>"` blocks the
//     asking session until a human writes here, so an approval is a real
//     question rather than a note left on a shelf. Without that, a stale queue is
//     worse than none: you would think you had gated something that already ran.

import { copyText, openContextMenu } from './menu.js';
import { inlineEditor, flashSaved } from './inline.js';
import { controlsFor } from './controls.js';

const ctl = controlsFor('gate');

const STYLE_ID = 'gate-view-style';

const CSS = `
[data-view="gate"] .gate-list { display: flex; flex-direction: column; gap: 12px; }
[data-view="gate"] .ask {
  border: 1px solid var(--line);
  border-left: 3px solid var(--mark);
  border-radius: 4px;
  background: var(--surface);
  padding: 12px 14px;
}
[data-view="gate"] .ask[data-risk="high"] { border-left-color: var(--danger); }
[data-view="gate"] .ask[data-risk="low"] { border-left-color: var(--accent-dim); }
[data-view="gate"] .ask-head { display: flex; align-items: baseline; gap: 10px; flex-wrap: wrap; }
[data-view="gate"] .ask-title { font-weight: 600; }
[data-view="gate"] .ask-meta {
  font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
  font-size: 0.73rem; color: var(--dim); margin-left: auto;
}
[data-view="gate"] .risk {
  font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
  font-size: 0.7rem; letter-spacing: 0.06em; text-transform: uppercase;
  border: 1px solid currentColor; border-radius: 2px; padding: 1px 5px;
  color: var(--mark);
}
[data-view="gate"] .ask[data-risk="high"] .risk { color: var(--danger); }
[data-view="gate"] .ask[data-risk="low"] .risk { color: var(--accent); }
[data-view="gate"] .ask-detail { margin: 7px 0 0; color: var(--muted); font-size: 0.87rem; }
[data-view="gate"] .ask-command,
[data-view="gate"] .ask-edit {
  display: block; width: 100%; margin: 9px 0 0;
  font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
  font-size: 0.83rem; line-height: 1.5;
  color: var(--text); background: var(--bg);
  border: 1px solid var(--line); border-radius: 3px; padding: 8px 10px;
  white-space: pre-wrap; overflow-x: auto;
}
[data-view="gate"] .ask-edit:focus { border-color: var(--accent); outline: none; }
[data-view="gate"] .ask-actions { display: flex; align-items: center; gap: 8px; margin-top: 11px; flex-wrap: wrap; }
[data-view="gate"] .ask-reason { flex: 1 1 16ch; min-width: 12ch; font-size: 0.84rem; }
[data-view="gate"] .allow-btn { border-color: var(--accent-dim); color: var(--accent); }
[data-view="gate"] .allow-btn:hover:not(:disabled) { background: var(--accent); color: var(--accent-ink); }
[data-view="gate"] .deny-btn { border-color: var(--danger); color: var(--danger); }
[data-view="gate"] .deny-btn:hover:not(:disabled) { background: var(--danger); color: var(--bg); }
[data-view="gate"] .decided { margin-top: 20px; }
[data-view="gate"] .decided h3 {
  font-size: 0.74rem; letter-spacing: 0.09em; text-transform: uppercase;
  color: var(--dim); margin: 0 0 8px; font-weight: 600;
}
[data-view="gate"] .verdict-row {
  display: grid; grid-template-columns: 8ch 1fr auto; gap: 10px;
  padding: 6px 0; border-bottom: 1px solid var(--line); font-size: 0.85rem;
}
[data-view="gate"] .verdict-row:last-child { border-bottom: 0; }
[data-view="gate"] .v-allow { color: var(--accent); }
[data-view="gate"] .v-deny { color: var(--danger); }
[data-view="gate"] .v-edit { color: var(--mark); }
[data-view="gate"] .verdict-when { color: var(--dim); font-family: ui-monospace, SFMono-Regular, Menlo, monospace; font-size: 0.74rem; }
[data-view="gate"] .verdict-right { display: flex; align-items: baseline; gap: 8px; white-space: nowrap; }
[data-view="gate"] .verdict-row[data-undone="yes"] { opacity: 0.55; }
[data-view="gate"] .verdict-row[data-undone="yes"] .v-allow,
[data-view="gate"] .verdict-row[data-undone="yes"] .v-deny,
[data-view="gate"] .verdict-row[data-undone="yes"] .v-edit { text-decoration: line-through; }
[data-view="gate"] .undo-btn {
  font: inherit; font-size: 0.72rem; line-height: 1;
  color: var(--dim); background: none;
  border: 1px solid var(--line); border-radius: 2px; padding: 2px 6px; cursor: pointer;
}
[data-view="gate"] .undo-btn:hover { color: var(--mark); border-color: var(--mark); }
[data-view="gate"] .verdict-reason { color: var(--muted); }
[data-view="gate"] .verdict-late {
  color: var(--dim);
  font-size: 0.75rem;
  font-style: italic;
}
[data-view="gate"] .reason-btn {
  font: inherit; font-size: 0.72rem;
  color: var(--mark); background: none;
  border: 1px dashed var(--mark); border-radius: 2px;
  padding: 2px 6px; cursor: pointer;
}
[data-view="gate"] .reason-btn:hover { background: var(--drop); }
[data-view="gate"] .reason-edit { grid-column: 1 / -1; margin-top: 6px; }
[data-view="gate"] .gate-empty { color: var(--muted); padding: 14px 0; }
`;

function injectStyle() {
  if (document.getElementById(STYLE_ID)) return;
  const style = document.createElement('style');
  style.id = STYLE_ID;
  style.textContent = CSS;
  document.head.append(style);
}

export function mountGate(root, ctx) {
  injectStyle();

  const pending = () => (Array.isArray(ctx.state.pending) ? ctx.state.pending : []);
  const decided = () => (Array.isArray(ctx.state.decided) ? ctx.state.decided : []);

  const head = document.createElement('div');
  head.className = 'toolbar';
  const count = document.createElement('span');
  count.className = 'mono hint';
  head.append(count);

  const list = document.createElement('div');
  list.className = 'gate-list';

  const history = document.createElement('div');
  history.className = 'decided';

  root.append(head, list, history);

  // Editing is per-viewer until it is submitted: an abandoned edit must not
  // become a decision, and must not be visible to the asking agent as one.
  const editing = new Map();   // ask id -> current text
  let editingReason = null;    // the decided entry whose reason is being written

  function decide(ask, verdict, reason, editedTo) {
    if (!Array.isArray(ctx.state.decided)) ctx.state.decided = [];
    ctx.state.decided.unshift({
      id: ask.id,
      title: ask.title,
      verdict,
      reason: reason || '',
      ...(editedTo !== undefined && editedTo !== null ? { editedTo } : {}),
      at: new Date().toISOString(),
      by: 'human',
      // The whole request is kept, not just its title. Without this an undo
      // could only put back a name — the detail, the command and the risk would
      // be gone, so "undecide" would quietly destroy what it claimed to restore.
      ask: { ...ask },
    });
    ctx.state.pending = pending().filter((p) => p.id !== ask.id);
    editing.delete(ask.id);
    ctx.save({ immediate: true }).then(render);
  }

  function askCard(ask) {
    const card = document.createElement('article');
    card.className = 'ask';
    card.dataset.risk = ask.risk || 'medium';
    card.dataset.id = ask.id;

    const headRow = document.createElement('div');
    headRow.className = 'ask-head';
    const title = document.createElement('span');
    title.className = 'ask-title';
    title.textContent = ask.title || '(untitled request)';
    const risk = document.createElement('span');
    risk.className = 'risk';
    risk.textContent = ask.risk || 'medium';
    const meta = document.createElement('span');
    meta.className = 'ask-meta';
    meta.textContent = [ask.by, ask.id].filter(Boolean).join(' · ');
    headRow.append(title, risk, meta);
    card.append(headRow);

    if (ask.detail) {
      const detail = document.createElement('p');
      detail.className = 'ask-detail';
      detail.textContent = ask.detail;
      card.append(detail);
    }

    // The command is the thing being approved, so it is shown verbatim — and
    // becomes editable in place, which is the whole point of the edit verdict.
    let editor = null;
    if (ask.command !== undefined && ask.command !== null) {
      if (editing.has(ask.id)) {
        editor = document.createElement('textarea');
        editor.className = 'ask-edit';
        editor.rows = Math.min(8, String(editing.get(ask.id)).split('\n').length + 1);
        editor.value = editing.get(ask.id);
        editor.addEventListener('input', () => editing.set(ask.id, editor.value));
        card.append(editor);
      } else {
        const pre = document.createElement('pre');
        pre.className = 'ask-command';
        pre.textContent = String(ask.command);
        card.append(pre);
      }
    }

    const actions = document.createElement('div');
    actions.className = 'ask-actions';

    const reason = document.createElement('input');
    reason.type = 'text';
    reason.className = 'ask-reason';
    reason.placeholder = 'why (optional, but it is what I learn from)';

    const allow = ctl('allow', { label: editing.has(ask.id) ? 'Allow as edited' : 'Allow',
      className: 'icon-btn allow-btn' });
    allow.addEventListener('click', () => {
      const edited = editing.has(ask.id) ? editing.get(ask.id) : null;
      const changed = edited !== null && edited !== String(ask.command ?? '');
      decide(ask, changed ? 'edit' : 'allow', reason.value, changed ? edited : undefined);
    });

    const deny = ctl('deny', { className: 'icon-btn deny-btn',
      onClick: () => decide(ask, 'deny', reason.value) });

    actions.append(allow, deny);

    if (ask.command !== undefined && ask.command !== null) {
      const edit = ctl('edit', { label: editing.has(ask.id) ? 'Cancel edit' : 'Edit' });
      edit.addEventListener('click', () => {
        if (editing.has(ask.id)) editing.delete(ask.id);
        else editing.set(ask.id, String(ask.command ?? ''));
        render();
      });
      actions.append(edit);
    }

    actions.append(reason);
    card.append(actions);

    card.addEventListener('contextmenu', (e) => {
      if (e.shiftKey) return;
      openContextMenu(e, [
        { head: ask.id },
        { label: 'Copy id', hint: ask.id, run: (ev) => copyText(ask.id, ev) },
        ask.command && { label: 'Copy command', run: (ev) => copyText(String(ask.command), ev) },
      ]);
    });

    return card;
  }

  // A decision you cannot take back is a trap: the wrong button is one keystroke
  // away and the consequence used to be permanent. Undo puts the request back at
  // the top of the queue exactly as it arrived.
  //
  // It does NOT pretend the decision never happened. The asking agent may already
  // have acted on it, so the entry is not deleted — it is marked `undone`, stays
  // in the record with the time it was reversed, and the agent can see both.
  function undecide(entry) {
    const restored = entry.ask && typeof entry.ask === 'object'
      ? { ...entry.ask }
      : { id: entry.id, title: entry.title };
    if (!Array.isArray(ctx.state.pending)) ctx.state.pending = [];
    // Guard against the same request sitting in both lists.
    if (!pending().some((p) => p.id === restored.id)) ctx.state.pending.unshift(restored);
    ctx.state.decided = decided().map((d) => (d === entry
      ? { ...d, undone: true, undoneAt: new Date().toISOString() }
      : d));
    ctx.save({ immediate: true }).then(render);
  }

  // Adding or changing a reason after the verdict. Stamped, so a reader can weigh
  // it: `reasonAddedAt` present means it was not written at the moment of the
  // decision.
  function setReason(entry, text) {
    const trimmed = (text || '').trim();
    ctx.state.decided = decided().map((d) => {
      if (d !== entry) return d;
      const next = { ...d, reason: trimmed };
      if (trimmed) next.reasonAddedAt = new Date().toISOString();
      else delete next.reasonAddedAt;
      return next;
    });
    editingReason = null;
    ctx.save({ immediate: true }).then(render);
  }

  function verdictRow(entry) {
    const row = document.createElement('div');
    row.className = 'verdict-row';
    const verdict = document.createElement('span');
    verdict.className = 'mono v-' + (entry.verdict || 'allow');
    verdict.textContent = entry.verdict || 'allow';
    const what = document.createElement('span');
    what.textContent = entry.title || entry.id;
    if (entry.reason) {
      const why = document.createElement('span');
      why.className = 'verdict-reason';
      why.textContent = ' — ' + entry.reason;
      what.append(why);
      if (entry.reasonAddedAt) {
        const late = document.createElement('span');
        late.className = 'verdict-late';
        late.textContent = '  (reason added later)';
        late.title = 'Decided ' + (entry.at || '?') + '\nReason written ' + entry.reasonAddedAt +
          '\nA reason written afterwards is reconstructed, not recorded — weigh it accordingly.';
        what.append(late);
      }
    }
    if (entry.editedTo) {
      const edited = document.createElement('pre');
      edited.className = 'ask-command';
      edited.textContent = entry.editedTo;
      what.append(edited);
    }
    const right = document.createElement('span');
    right.className = 'verdict-right';
    const when = document.createElement('span');
    when.className = 'verdict-when';
    when.textContent = entry.at ? new Date(entry.at).toLocaleTimeString() : '';
    right.append(when);

    if (entry.undone) {
      row.dataset.undone = 'yes';
      verdict.textContent = entry.verdict + ' (undone)';
      const note = document.createElement('span');
      note.className = 'verdict-when';
      note.textContent = ' · reversed ' + (entry.undoneAt ? new Date(entry.undoneAt).toLocaleTimeString() : '');
      right.append(note);
    } else {
      // No reason recorded is the common case — people click Allow and move on —
      // and it is the one that costs later, so the invitation is visible rather
      // than hidden behind a right-click.
      const reasonBtn = ctl('reason', {
        label: entry.reason ? 'edit why' : 'add why',
        title: entry.reason
          ? 'Change the reason. It will be marked as written after the fact.'
          : 'Say why, now or whenever you are asked. It will be marked as written after the fact.',
        className: 'reason-btn', onClick: () => { editingReason = entry; render(); } });
      right.append(reasonBtn);

      const undo = ctl('undo', { className: 'msg-tool undo-btn', onClick: () => undecide(entry) });
      right.append(undo);
    }

    if (editingReason === entry) {
      const editor = inlineEditor({
        value: entry.reason || '',
        multiline: false,
        placeholder: 'why — the half that stops this being argued again',
        saveLabel: 'Save why',
        onSave: (text) => { setReason(entry, text); },
        onCancel: () => { editingReason = null; render(); },
      });
      editor.el.classList.add('reason-edit');
      row.append(editor.el);
      requestAnimationFrame(() => editor.focus());
    }

    row.append(verdict, what, right);
    return row;
  }

  function render() {
    const waiting = pending();
    count.textContent = waiting.length
      ? `${waiting.length} waiting on you`
      : 'nothing waiting';

    list.replaceChildren();
    if (!waiting.length) {
      const empty = document.createElement('p');
      empty.className = 'gate-empty';
      empty.textContent = 'Nothing to approve. An agent adds to state.pending and waits with: ./board -wait -for "answer ' +
        String((ctx.tab && ctx.tab.id) || '<tab>').split('/')[0] + '"';
      list.append(empty);
    } else {
      for (const ask of waiting) list.append(askCard(ask));
    }

    history.replaceChildren();
    const past = decided();
    if (past.length) {
      const h = document.createElement('h3');
      h.textContent = `decided (${past.length})`;
      history.append(h);
      for (const entry of past.slice(0, 30)) history.append(verdictRow(entry));
    }
  }

  render();
  return {
    refresh() {
      // Never interrupt a decision being typed or an edit in progress.
      if (root.contains(document.activeElement) &&
          /INPUT|TEXTAREA/.test(document.activeElement.tagName)) return;
      render();
    },
  };
}
