// A heartbeat strip: is the agent that maintains this tab still on duty?
//
// The honest problem. A read-only tab looks identical whether the agent keeping
// it is working, idle, or dead three hours ago — and the more trustworthy the tab
// looks, the worse that is. But a subagent only EXISTS while it is running a
// task, so a permanently pulsing dot would be a lie.
//
// So this shows two things and never conflates them: what the agent last said it
// was doing, and how long ago it said so. Age is what makes it truthful — a
// "working" heartbeat from twenty minutes ago reads as stale on purpose, because
// that is exactly what a session that died mid-task looks like.
//
//   state.heartbeat = { by, at, phase: 'working' | 'idle', note? }
//
// The agent stamps `working` when it starts and `idle` when it finishes, so the
// pulse is real for the duration and stops on its own afterwards. Nothing here
// polls: the value arrives with the board's own live reload, and the strip only
// re-renders the AGE on a local tick.

const STYLE_ID = 'heartbeat-style';

const FRESH_MS = 90 * 1000;        // a beat this recent means "now"
const RECENT_MS = 10 * 60 * 1000;  // beyond this, say "last seen" and stop implying life

const CSS = `
.hb {
  display: inline-flex;
  align-items: center;
  gap: 7px;
  padding: 2px 8px 2px 7px;
  border: 1px solid var(--line);
  border-radius: 999px;
  background: var(--sunken);
  font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
  font-size: 0.73rem;
  color: var(--dim);
  white-space: nowrap;
}
.hb-dot {
  width: 7px;
  height: 7px;
  border-radius: 50%;
  background: var(--dim);
  flex: 0 0 auto;
}
.hb[data-state="working"] { color: var(--text); border-color: var(--accent-dim); }
.hb[data-state="working"] .hb-dot { background: var(--accent); animation: hb-beat 1.6s ease-in-out infinite; }
.hb[data-state="idle"] { color: var(--muted); }
.hb[data-state="idle"] .hb-dot { background: var(--accent-dim); }
.hb[data-state="stale"] { color: var(--dim); border-style: dashed; }
.hb[data-state="stale"] .hb-dot { background: var(--edge); }
.hb-who { color: inherit; }
.hb-age { color: var(--dim); }

@keyframes hb-beat {
  0%, 100% { box-shadow: 0 0 0 0 color-mix(in srgb, var(--accent) 55%, transparent); }
  50%      { box-shadow: 0 0 0 5px color-mix(in srgb, var(--accent) 0%, transparent); }
}

@media (prefers-reduced-motion: reduce) {
  .hb[data-state="working"] .hb-dot { animation: none; }
}
`;

function injectStyle() {
  if (document.getElementById(STYLE_ID)) return;
  const style = document.createElement('style');
  style.id = STYLE_ID;
  style.textContent = CSS;
  document.head.append(style);
}

function ageLabel(ms) {
  if (!Number.isFinite(ms) || ms < 0) return '';
  const s = Math.round(ms / 1000);
  if (s < 10) return 'just now';
  if (s < 60) return `${s}s ago`;
  const m = Math.round(s / 60);
  if (m < 60) return `${m}m ago`;
  const h = Math.floor(m / 60);
  return `${h}h${String(m % 60).padStart(2, '0')} ago`;
}

/**
 * Attach a heartbeat strip. `read()` returns the current heartbeat object (read
 * lazily, so an agent write flows through without re-mounting).
 * Returns { el, refresh(), destroy() }.
 */
export function attachHeartbeat(read) {
  injectStyle();

  const el = document.createElement('span');
  el.className = 'hb';
  const dot = document.createElement('span');
  dot.className = 'hb-dot';
  const who = document.createElement('span');
  who.className = 'hb-who';
  const age = document.createElement('span');
  age.className = 'hb-age';
  el.append(dot, who, age);

  function refresh() {
    const beat = read();
    if (!beat || typeof beat !== 'object' || !beat.at) {
      el.hidden = true;
      return;
    }
    el.hidden = false;

    const ms = Date.now() - Date.parse(beat.at);
    const phase = beat.phase === 'working' ? 'working' : 'idle';
    // Age wins over the claimed phase. An agent that says "working" and then
    // stops writing is indistinguishable from one that died — so after RECENT_MS
    // the strip stops implying either.
    const state = !Number.isFinite(ms) || ms > RECENT_MS
      ? 'stale'
      : (phase === 'working' && ms < FRESH_MS ? 'working' : 'idle');

    el.dataset.state = state;
    who.textContent = beat.by || 'agent';
    age.textContent = state === 'stale'
      ? `· last seen ${ageLabel(ms)}`
      : `· ${state === 'working' ? 'working' : 'idle'} ${ageLabel(ms)}`;

    const parts = [
      `${beat.by || 'agent'} — ${phase}`,
      beat.note ? beat.note : null,
      beat.at,
      state === 'stale' ? 'No beat in a while: treat this tab as unattended.' : null,
    ].filter(Boolean);
    el.title = parts.join('\n');
  }

  refresh();
  // Local tick only — the age has to keep moving even when nothing writes.
  const timer = setInterval(refresh, 5000);

  return { el, refresh, destroy() { clearInterval(timer); } };
}
