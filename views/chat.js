// Chat view: two or more agents (and the human) coordinating in one channel.
// Not a chatbot UI — every row is a participant's turn, and the human can
// jump in at any point. Messages are append-only: nobody edits or deletes
// another participant's line here.
//
// One exception, and it is the human's own: a message YOU wrote can be reworded
// or withdrawn until a session has read it. The gate is the ack (`ackBy`), not a
// timer — "has anyone acted on this yet" is the question that actually matters,
// and a half-formed question sitting on the record is how an agent ends up
// answering the wrong thing. Once acked, the controls disappear and a lock
// explains why; the server carries acks forward so an agent cannot reopen the
// window by dropping one.

import { controlsFor } from './controls.js';

const ctl = controlsFor('chat');

const NEAR_BOTTOM_PX = 56;         // how close to the bottom still counts as "reading live"
const AUTHOR_PALETTE = ['--accent', '--agent', '--mark', '--focus', '--edge'];

let styleInjected = false;

function injectStyle() {
  if (styleInjected) return;
  styleInjected = true;
  const style = document.createElement('style');
  style.textContent = `
    [data-view="chat"] .chat-panel { display: flex; flex-direction: column; gap: 10px; }
    [data-view="chat"] .chat-head { display: flex; align-items: baseline; justify-content: space-between; gap: 8px; }
    [data-view="chat"] .chat-head .panel-head { margin: 0; }
    [data-view="chat"] .chat-count { color: var(--muted); font-size: 0.75rem; }
    [data-view="chat"] .chat-scroll {
      /* Height is the agent's call, not the renderer's: set state.height to any
         CSS length. This fallback fills the viewport below the composer. */
      height: var(--chat-height, calc(100vh - 300px));
      min-height: 220px;
      overflow-y: auto;
      display: flex;
      flex-direction: column;
      background: var(--sunken);
      border: 1px solid var(--line);
      border-radius: 4px;
      padding: 12px;
    }
    [data-view="chat"] .chat-empty { margin: auto; text-align: center; }
    [data-view="chat"] .messages { display: flex; flex-direction: column; gap: 10px; }
    [data-view="chat"] .day-divider {
      display: flex;
      align-items: center;
      gap: 10px;
      color: var(--dim);
      font-size: 0.72rem;
      margin: 2px 0;
    }
    [data-view="chat"] .day-divider::before,
    [data-view="chat"] .day-divider::after { content: ''; flex: 1 1 auto; border-top: 1px solid var(--line); }
    [data-view="chat"] .msg {
      max-width: min(82%, 72ch);   /* keep the line length readable, not the panel narrow */
      padding: 7px 10px 8px;
      border: 1px solid var(--line);
      border-left: 3px solid var(--author-color, var(--line-strong));
      border-radius: 6px;
      background: var(--surface);
      align-self: flex-start;
    }
    [data-view="chat"] .msg-human {
      align-self: flex-end;
      border-left-color: var(--line);
      border-right: 3px solid var(--author-color, var(--accent));
    }
    [data-view="chat"] .msg-head { display: flex; align-items: center; gap: 8px; margin-bottom: 4px; flex-wrap: wrap; }
    [data-view="chat"] .author-chip {
      color: var(--author-color, var(--muted));
      border: 1px solid var(--author-color, var(--line));
      border-radius: 2px;
      padding: 1px 6px;
      font-size: 0.72rem;
      letter-spacing: 0.02em;
    }
    [data-view="chat"] .msg-time { color: var(--dim); font-size: 0.72rem; }
    [data-view="chat"] .msg-text { white-space: pre-wrap; overflow-wrap: anywhere; line-height: 1.45; font-size: 0.88rem; }
    [data-view="chat"] .mention {
      color: var(--author-color, var(--accent));
      background: color-mix(in srgb, var(--author-color, var(--accent)) 12%, transparent);
      border-radius: 3px; padding: 0 3px; font-weight: 550;
    }
    [data-view="chat"] .msg-tools { display: flex; gap: 4px; margin-left: auto; }
    [data-view="chat"] .msg-tool {
      font: inherit; font-size: 0.72rem; line-height: 1;
      color: var(--dim); background: none;
      border: 1px solid var(--line); border-radius: 2px;
      padding: 2px 5px; cursor: pointer;
    }
    [data-view="chat"] .msg-tool:hover { color: var(--text); border-color: var(--line-strong); }
    [data-view="chat"] .msg-tool--danger:hover { color: var(--danger); border-color: var(--danger); }
    [data-view="chat"] .msg-lock { font-size: 0.7rem; color: var(--dim); margin-left: auto; }
    [data-view="chat"] .msg-edited { font-size: 0.7rem; color: var(--dim); }
    [data-view="chat"] .msg-edit { width: 100%; min-height: 4em; font-size: 0.88rem; }
    [data-view="chat"] .composer { display: flex; gap: 8px; align-items: flex-end; }
    [data-view="chat"] .composer-input { flex: 1 1 auto; min-height: 2.6em; max-height: 8em; }
    [data-view="chat"] .composer-hint { margin-top: -2px; }
    @media (prefers-reduced-motion: no-preference) {
      [data-view="chat"] .chat-scroll { scroll-behavior: smooth; }
    }
  `;
  document.head.append(style);
}

export function mountChat(root, ctx) {
  injectStyle();

  let firstRender = true;   // first paint always snaps to the bottom, empty or not

  const panel = document.createElement('div');
  panel.className = 'panel chat-panel';

  const head = document.createElement('div');
  head.className = 'chat-head';
  const heading = document.createElement('h2');
  heading.className = 'panel-head';
  heading.textContent = (ctx.tab && ctx.tab.name) || 'Chat';
  const countEl = document.createElement('span');
  countEl.className = 'mono chat-count';
  head.append(heading, countEl);

  const scrollEl = document.createElement('div');
  // state.height: any CSS length ("50vh", "480px"), or a number read as px.
  function applyHeight() {
    const h = ctx.state.height;
    if (h === undefined || h === null || h === '') {
      scrollEl.style.removeProperty('--chat-height');
      return;
    }
    scrollEl.style.setProperty('--chat-height', typeof h === 'number' ? `${h}px` : String(h));
  }

  scrollEl.className = 'chat-scroll';
  applyHeight();

  const emptyEl = document.createElement('p');
  emptyEl.className = 'hint chat-empty';
  emptyEl.textContent = 'No messages yet — say something below to start the channel.';
  scrollEl.append(emptyEl);

  const listEl = document.createElement('div');
  listEl.className = 'messages';
  scrollEl.append(listEl);

  const composer = document.createElement('div');
  composer.className = 'composer';
  const textarea = document.createElement('textarea');
  textarea.className = 'composer-input';
  textarea.rows = 2;
  textarea.placeholder = 'Message the board…';
  textarea.setAttribute('aria-label', 'Message');
  const sendBtn = ctl('send', { className: 'primary-btn' });
  composer.append(textarea, sendBtn);

  const composerHint = document.createElement('p');
  composerHint.className = 'hint composer-hint';
  composerHint.textContent = 'Enter to send · Shift, Alt or Ctrl+Enter for a newline';

  panel.append(head, scrollEl, composer, composerHint);
  root.append(panel);

  function getMessages() {
    return Array.isArray(ctx.state.messages) ? ctx.state.messages : [];
  }

  // Same "highest existing n<number> + 1" pattern used for nodes and marks
  // elsewhere in this project, so ids never collide with another session's.
  // Board-wide monotonic id from the shell, so a deleted message's id is never
  // handed out again. Falls back to local max+1 only if mounted without it.
  function nextId() {
    if (typeof ctx.nextId === 'function') return ctx.nextId();
    let max = 0;
    for (const m of ctx.state.messages || []) {
      const hit = /^[a-z]*(\d+)$/.exec(m && m.id);
      if (hit) max = Math.max(max, Number(hit[1]));
    }
    return 'm' + (max + 1);
  }

  function isNearBottom() {
    return scrollEl.scrollHeight - scrollEl.scrollTop - scrollEl.clientHeight < NEAR_BOTTOM_PX;
  }

  function buildRow(msg) {
    const by = typeof msg.by === 'string' && msg.by ? msg.by : 'unknown';
    const row = document.createElement('div');
    row.className = 'msg ' + (by === 'human' ? 'msg-human' : 'msg-agent');
    row.style.setProperty('--author-color', colorVarFor(by));

    const rowHead = document.createElement('div');
    rowHead.className = 'msg-head';
    const chip = document.createElement('span');
    chip.className = 'mono author-chip';
    chip.textContent = by;
    const time = document.createElement('span');
    time.className = 'mono msg-time';
    time.textContent = formatTime(msg.at);
    if (typeof msg.at === 'string') time.title = msg.at;
    rowHead.append(chip, time);

    if (msg.edited) {
      const edited = document.createElement('span');
      edited.className = 'msg-edited';
      edited.textContent = '(edited)';
      rowHead.append(edited);
    }

    // Your own message, not yet read by anyone: reword it or take it back.
    if (by === 'human' && !msg.ackBy) {
      const tools = document.createElement('div');
      tools.className = 'msg-tools';

      const editBtn = ctl('edit-message', { className: 'msg-tool', onClick: () => startEdit(row, msg) });

      const delBtn = ctl('delete-message', { className: 'msg-tool msg-tool--danger' });
      delBtn.addEventListener('click', () => {
        ctx.state.messages = getMessages().filter((m) => m.id !== msg.id);
        render();
        ctx.save({ immediate: true });
      });

      tools.append(editBtn, delBtn);
      rowHead.append(tools);
    } else if (msg.ackBy) {
      const lock = document.createElement('span');
      lock.className = 'msg-lock';
      lock.textContent = `read by ${msg.ackBy}`;
      lock.title = 'Read by a session — no longer editable';
      rowHead.append(lock);
    }

    const text = document.createElement('div');
    text.className = 'msg-text';
    mentionsInto(text, typeof msg.text === 'string' ? msg.text : '');

    row.append(rowHead, text);
    return row;
  }

  // @mentions. In a channel with two agents and a human, "who is this for" was
  // carried entirely by prose. A mention that matches a participant in this
  // channel is lit; one that does not is left plain rather than promising an
  // address that nobody answers.
  function mentionsInto(parent, text) {
    const known = new Set(getMessages().map((m) => m && m.by).filter(Boolean));
    known.add('human');
    const pattern = /@([a-zA-Z][\w-]{0,31})/g;
    let at = 0;
    let hit;
    while ((hit = pattern.exec(text)) !== null) {
      if (hit.index > at) parent.append(document.createTextNode(text.slice(at, hit.index)));
      const name = hit[1];
      if (known.has(name)) {
        const chip = document.createElement('span');
        chip.className = 'mention';
        chip.textContent = '@' + name;
        chip.style.setProperty('--author-color', colorVarFor(name));
        chip.title = 'addressed to ' + name;
        parent.append(chip);
      } else {
        parent.append(document.createTextNode(hit[0]));
      }
      at = hit.index + hit[0].length;
    }
    if (at < text.length) parent.append(document.createTextNode(text.slice(at)));
  }

  // Edit in place: the bubble becomes a textarea, Enter saves, Escape abandons.
  // Nothing is written until you save, so an abandoned edit costs nothing.
  function startEdit(row, msg) {
    const original = typeof msg.text === 'string' ? msg.text : '';
    const area = document.createElement('textarea');
    area.className = 'msg-edit';
    area.value = original;
    const existing = row.querySelector('.msg-text');
    if (!existing) return;
    existing.replaceWith(area);
    area.focus();
    area.setSelectionRange(area.value.length, area.value.length);

    const finish = (save) => {
      const next = area.value.trim();
      if (save && next && next !== original) {
        msg.text = next;
        msg.edited = true;
        render();
        ctx.save({ immediate: true });
        return;
      }
      render();
    };

    area.addEventListener('keydown', (e) => {
      if (e.key === 'Escape') { e.preventDefault(); finish(false); return; }
      if (e.key === 'Enter' && !e.shiftKey && !e.altKey && !e.ctrlKey && !e.metaKey) {
        e.preventDefault();
        finish(true);
      }
    });
    area.addEventListener('blur', () => finish(true), { once: true });
  }

  function buildDivider(at) {
    const div = document.createElement('div');
    div.className = 'day-divider';
    const span = document.createElement('span');
    span.textContent = formatDay(at);
    div.append(span);
    return div;
  }

  // Rebuilds the list from state and restores whatever the viewer was doing:
  // pinned to the bottom if they already were, or exactly where they scrolled
  // to otherwise — never yanked around by a message another agent appended.
  function render() {
    const messages = getMessages();
    const shouldStickToBottom = firstRender || isNearBottom();
    const prevScrollTop = scrollEl.scrollTop;

    countEl.textContent = messages.length ? `${messages.length} message${messages.length === 1 ? '' : 's'}` : '';
    emptyEl.hidden = messages.length !== 0;

    const frag = document.createDocumentFragment();
    let lastDay = null;
    for (const msg of messages) {
      const day = dayKey(msg.at);
      if (lastDay !== null && day !== null && day !== lastDay) frag.append(buildDivider(msg.at));
      if (day !== null) lastDay = day;
      frag.append(buildRow(msg));
    }
    listEl.replaceChildren(frag);

    if (shouldStickToBottom) scrollEl.scrollTop = scrollEl.scrollHeight;
    else scrollEl.scrollTop = prevScrollTop;
    firstRender = false;
  }

  function send() {
    const text = textarea.value.trim();
    if (!text) return;
    if (!Array.isArray(ctx.state.messages)) ctx.state.messages = [];
    ctx.state.messages.push({ id: nextId(), at: new Date().toISOString(), by: 'human', text });
    textarea.value = '';
    render();
    ctx.save({ immediate: true });
  }

  sendBtn.addEventListener('click', send);
  textarea.addEventListener('keydown', (e) => {
    if (e.key !== 'Enter' || e.isComposing) return;
    // Any modifier means "newline", not "send". Shift is the convention most
    // chat apps use, but nobody should have to learn which one this app picked —
    // Alt, Ctrl and Cmd all do the obvious thing. Bare Enter still sends.
    if (e.shiftKey || e.altKey || e.ctrlKey || e.metaKey) return;
    e.preventDefault();
    send();
  });

  render();

  return {
    refresh() {
      applyHeight();   // an agent may change state.height live
      render();
    },
  };
}

/* ---------- pure helpers ---------- */

function hashString(str) {
  let hash = 5381;
  for (let i = 0; i < str.length; i++) hash = ((hash << 5) + hash + str.charCodeAt(i)) | 0;
  return Math.abs(hash);
}

// human and a bare agent name get fixed, always-recognizable colours; every
// other name (agent-1, agent-research, claude:planner, …) hashes into the rest of
// the palette so distinct agents stay visually distinct from each other.
//
// "claude" is matched as well as "agent" because this reads HISTORY: transcripts
// written before the rename are stamped by:"claude", and they should keep their
// colour. It is not an endorsement of the name — CLAUDE.md still says use
// agent-1, never claude.
function colorVarFor(by) {
  if (by === 'human') return 'var(--accent)';
  if (by === 'agent' || by === 'claude') return 'var(--agent)';
  return `var(${AUTHOR_PALETTE[hashString(by) % AUTHOR_PALETTE.length]})`;
}

function dayKey(at) {
  const d = new Date(at);
  return Number.isNaN(d.getTime()) ? null : d.toDateString();
}

function formatDay(at) {
  const d = new Date(at);
  return d.toLocaleDateString(undefined, { month: 'short', day: 'numeric', year: 'numeric' });
}

function formatTime(at) {
  const d = new Date(at);
  return Number.isNaN(d.getTime()) ? '' : d.toLocaleTimeString([], { hour: 'numeric', minute: '2-digit' });
}
