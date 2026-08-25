// Vote view: several participants score the same options, visibly.
//
// Built for the case where two sessions propose different things. Today that
// disagreement gets resolved by whichever agent writes the summary — which is
// exactly the wrong way round. Here each participant's score sits in the open
// next to the others, the spread is visible, and the human breaks the tie.
//
//   state = {
//     question: 'Which cutover?',
//     scale: 5,                              // 1..scale, default 5
//     options: [{ id, label, note?, comments?: { <actor>: '…' } }],
//     ballots: { 'agent-1': { <optionId>: 3 }, human: { … } },
//     closed?: true                          // stop collecting, keep showing
//   }
//
// A score with no reason is a vote you cannot argue with, so every option takes
// per-participant notes: `option.comments[actor]`. The human writes theirs in
// place, agents write their own key, and both are shown against the option — which
// is the point, since the interesting case is two participants scoring the same
// thing differently for reasons neither had said out loud.
//
// The human votes as `human`; an agent writes its own key. Nobody can overwrite
// another participant's ballot through this view — the columns are per actor, and
// only the human's column is editable here. An agent doing it through aboard.json
// is a different question, and one the journal now answers.

import { inlineEditor, flashSaved } from './inline.js';
import { controlsFor } from './controls.js';

const ctl = controlsFor('vote');

const STYLE_ID = 'vote-view-style';

const ACTOR_COLORS = ['--accent', '--agent', '--mark', '--focus', '--edge'];

const CSS = `
[data-view="vote"] .vote-q { font-size: 1rem; font-weight: 600; margin: 0 0 4px; }
[data-view="vote"] .vote-sub { color: var(--muted); font-size: 0.85rem; margin: 0 0 14px; }
[data-view="vote"] table { border-collapse: collapse; width: 100%; }
[data-view="vote"] th, [data-view="vote"] td {
  text-align: left; padding: 8px 10px; border-bottom: 1px solid var(--line); vertical-align: top;
}
[data-view="vote"] th {
  font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
  font-size: 0.72rem; letter-spacing: 0.06em; text-transform: uppercase;
  color: var(--actor-color, var(--muted));
}
[data-view="vote"] .opt-label { font-weight: 550; }
[data-view="vote"] .opt-note { color: var(--muted); font-size: 0.83rem; margin-top: 3px; }
[data-view="vote"] .comments { margin-top: 7px; display: flex; flex-direction: column; gap: 5px; }
[data-view="vote"] .comment {
  font-size: 0.82rem; line-height: 1.45;
  border-left: 2px solid var(--actor-color, var(--line-strong));
  padding-left: 8px; color: var(--text);
}
[data-view="vote"] .comment b {
  font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
  font-size: 0.72rem; font-weight: 500; color: var(--actor-color, var(--muted));
  display: block;
}
[data-view="vote"] .note-btn {
  font: inherit; font-size: 0.74rem; color: var(--dim);
  background: none; border: 1px solid var(--line); border-radius: 2px;
  padding: 1px 6px; cursor: pointer; margin-top: 6px;
}
[data-view="vote"] .note-btn:hover { color: var(--text); border-color: var(--line-strong); }
[data-view="vote"] .note-edit { width: 100%; min-height: 3.2em; margin-top: 6px; font-size: 0.83rem; }
[data-view="vote"] .score { font-variant-numeric: tabular-nums; text-align: center; width: 7ch; color: var(--actor-color, var(--text)); }
[data-view="vote"] .score-empty { color: var(--dim); }
[data-view="vote"] .picker { display: flex; gap: 3px; justify-content: center; }
[data-view="vote"] .pip {
  width: 20px; height: 22px; padding: 0; font: inherit; font-size: 0.74rem;
  color: var(--dim); background: transparent;
  border: 1px solid var(--line); border-radius: 3px; cursor: pointer;
}
[data-view="vote"] .pip:hover { border-color: var(--line-strong); color: var(--text); }
[data-view="vote"] .pip[aria-pressed="true"] { background: var(--accent); border-color: var(--accent); color: var(--accent-ink); }
[data-view="vote"] .total { font-variant-numeric: tabular-nums; text-align: right; width: 10ch; }
[data-view="vote"] .bar {
  height: 5px; border-radius: 3px; background: var(--accent); margin-top: 5px;
}
[data-view="vote"] .spread { color: var(--mark); font-size: 0.78rem; }
[data-view="vote"] .lead { color: var(--accent); }
[data-view="vote"] .vote-empty { color: var(--muted); }
`;

function injectStyle() {
  if (document.getElementById(STYLE_ID)) return;
  const style = document.createElement('style');
  style.id = STYLE_ID;
  style.textContent = CSS;
  document.head.append(style);
}

export function mountVote(root, ctx) {
  injectStyle();

  const options = () => (Array.isArray(ctx.state.options) ? ctx.state.options : []).filter((o) => o && o.id);
  const ballots = () => (ctx.state.ballots && typeof ctx.state.ballots === 'object' ? ctx.state.ballots : {});
  const scale = () => {
    const n = Number(ctx.state.scale);
    return Number.isFinite(n) && n >= 2 && n <= 10 ? Math.floor(n) : 5;
  };
  const closed = () => ctx.state.closed === true;

  const question = document.createElement('p');
  question.className = 'vote-q';
  const sub = document.createElement('p');
  sub.className = 'vote-sub';
  const table = document.createElement('table');
  root.append(question, sub, table);

  function actors() {
    const keys = Object.keys(ballots());
    if (!keys.includes('human')) keys.push('human');
    // Humans last: their column is the editable one, and it reads as the
    // deciding column when it sits at the end.
    return keys.sort((a, b) => (a === 'human' ? 1 : b === 'human' ? -1 : a.localeCompare(b)));
  }

  const colorFor = (actor) => `var(${ACTOR_COLORS[Math.max(0, actors().indexOf(actor)) % ACTOR_COLORS.length]})`;

  function scoreOf(actor, optionId) {
    const b = ballots()[actor];
    const v = b && b[optionId];
    return Number.isFinite(Number(v)) && v !== '' ? Number(v) : null;
  }

  function setHumanScore(optionId, value) {
    if (!ctx.state.ballots || typeof ctx.state.ballots !== 'object') ctx.state.ballots = {};
    if (!ctx.state.ballots.human || typeof ctx.state.ballots.human !== 'object') ctx.state.ballots.human = {};
    const current = ctx.state.ballots.human[optionId];
    // Clicking the same pip again clears it — "I have no view on this" has to be
    // expressible, or every option gets a number whether it deserves one or not.
    if (Number(current) === value) delete ctx.state.ballots.human[optionId];
    else ctx.state.ballots.human[optionId] = value;
    ctx.save().then(render);
  }

  function stats(optionId) {
    const values = actors().map((a) => scoreOf(a, optionId)).filter((v) => v !== null);
    if (!values.length) return { n: 0, total: 0, mean: 0, spread: 0 };
    const total = values.reduce((a, b) => a + b, 0);
    return {
      n: values.length,
      total,
      mean: total / values.length,
      spread: Math.max(...values) - Math.min(...values),
    };
  }

  // Per-viewer: which note you have open is not everyone's business.
  const openNotes = new Set();

  function commentsOf(opt) {
    return opt.comments && typeof opt.comments === 'object' ? opt.comments : null;
  }

  function saveComment(opt, text) {
    const target = options().find((o) => o.id === opt.id);
    if (!target) return;
    if (!target.comments || typeof target.comments !== 'object') target.comments = {};
    const trimmed = text.trim();
    if (trimmed) target.comments.human = trimmed;
    else delete target.comments.human;
    ctx.save().then(render);
  }

  function commentsInto(cell, opt) {
    const all = commentsOf(opt) || {};
    const box = document.createElement('div');
    box.className = 'comments';
    for (const [actor, text] of Object.entries(all)) {
      if (actor === 'human' && openNotes.has(opt.id)) continue;   // being edited below
      const c = document.createElement('div');
      c.className = 'comment';
      c.style.setProperty('--actor-color', colorFor(actor));
      const who = document.createElement('b');
      who.textContent = actor;
      const body = document.createElement('span');
      body.textContent = text;
      c.append(who, body);
      box.append(c);
    }
    cell.append(box);

    if (closed()) return;

    if (openNotes.has(opt.id)) {
      const editor = inlineEditor({
        value: all.human || '',
        placeholder: 'why this score — or what would change it',
        saveLabel: 'Save note',
        onSave: (text) => {
          openNotes.delete(opt.id);
          saveComment(opt, text);
          const back = table.querySelector('.opt-label');
          if (back) flashSaved(back.parentElement);
        },
        onCancel: () => { openNotes.delete(opt.id); render(); },
      });
      cell.append(editor.el);
      // Focus after the row is in the document, or the caret goes nowhere.
      requestAnimationFrame(() => editor.focus());
      return;
    }

    const btn = ctl('note', { label: all.human ? 'edit your note' : 'add a note',
      className: 'note-btn', onClick: () => { openNotes.add(opt.id); render(); } });
    cell.append(btn);
  }

  function render() {
    question.textContent = ctx.state.question || 'Score the options';
    const list = options();
    const people = actors();

    const best = list
      .map((o) => ({ id: o.id, mean: stats(o.id).mean }))
      .sort((a, b) => b.mean - a.mean)[0];

    sub.textContent = closed()
      ? 'Closed — showing the result.'
      : `Click a number to score. ${people.filter((p) => Object.keys(ballots()[p] || {}).length).length} of ${people.length} have voted.`;

    table.replaceChildren();
    if (!list.length) {
      const p = document.createElement('p');
      p.className = 'vote-empty';
      p.textContent = 'No options yet — an agent sets state.options.';
      table.append(p);
      return;
    }

    const thead = document.createElement('thead');
    const hr = document.createElement('tr');
    const optHead = document.createElement('th');
    optHead.textContent = 'option';
    hr.append(optHead);
    for (const actor of people) {
      const th = document.createElement('th');
      th.textContent = actor;
      th.style.setProperty('--actor-color', colorFor(actor));
      th.style.textAlign = 'center';
      hr.append(th);
    }
    const meanHead = document.createElement('th');
    meanHead.textContent = 'mean';
    meanHead.style.textAlign = 'right';
    hr.append(meanHead);
    thead.append(hr);
    table.append(thead);

    const tbody = document.createElement('tbody');
    for (const opt of list) {
      const tr = document.createElement('tr');

      const label = document.createElement('td');
      const strong = document.createElement('div');
      strong.className = 'opt-label';
      strong.textContent = opt.label || opt.id;
      label.append(strong);
      if (opt.note) {
        const note = document.createElement('div');
        note.className = 'opt-note';
        note.textContent = opt.note;
        label.append(note);
      }
      commentsInto(label, opt);
      tr.append(label);

      for (const actor of people) {
        const td = document.createElement('td');
        td.className = 'score';
        td.style.setProperty('--actor-color', colorFor(actor));
        const mine = actor === 'human' && !closed();
        const value = scoreOf(actor, opt.id);

        if (mine) {
          const picker = document.createElement('div');
          picker.className = 'picker';
          for (let i = 1; i <= scale(); i++) {
            const pip = ctl('score', { label: String(i), title: value === i ? 'click again to clear' : `score ${i}`,
              className: 'pip', pressed: value === i, onClick: () => setHumanScore(opt.id, i) });
            picker.append(pip);
          }
          td.append(picker);
        } else {
          td.textContent = value === null ? '·' : String(value);
          if (value === null) td.classList.add('score-empty');
        }
        tr.append(td);
      }

      const s = stats(opt.id);
      const meanCell = document.createElement('td');
      meanCell.className = 'total';
      const num = document.createElement('div');
      num.textContent = s.n ? s.mean.toFixed(1) : '—';
      if (best && best.id === opt.id && s.n) num.className = 'lead';
      meanCell.append(num);
      if (s.n) {
        const bar = document.createElement('div');
        bar.className = 'bar';
        bar.style.width = Math.max(4, (s.mean / scale()) * 100) + '%';
        bar.style.marginLeft = 'auto';
        meanCell.append(bar);
        if (s.spread >= Math.ceil(scale() / 2)) {
          const dis = document.createElement('div');
          dis.className = 'spread';
          // The disagreement is the interesting part, so it is called out rather
          // than averaged away.
          dis.textContent = `split by ${s.spread}`;
          dis.title = 'Participants disagree sharply on this option';
          meanCell.append(dis);
        }
      }
      tr.append(meanCell);
      tbody.append(tr);
    }
    table.append(tbody);
  }

  render();
  return { refresh: render };
}
