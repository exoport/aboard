// Table view: typed rows the human edits in place.
//
// The widest hole in the previous nine renderers. Tabular data went into `notes`
// (which loses every bit of structure) or into a bespoke `html` widget (rewritten
// from scratch each time). Findings needing a verdict, files needing an owner,
// test results, costs, a comparison — all the same shape, and all of it came back
// as prose that had to be re-parsed by hand.
//
// Scope line, held deliberately: no formulas, no merged cells, no nested tables.
// This is a form with many rows, not a spreadsheet. Sorting and column width are
// per-viewer; everything else round-trips through state.
//
//   state = {
//     columns: [{ id, label, type, options?, width?, hint? }],   // text|number|select|checkbox|longtext
//     rows:    [{ id, <columnId>: value, ... }],
//     readOnly?: true,          // agent-owned, same posture as the kanban
//     addLabel?: 'Add finding'  // what the button says, since "Add row" is rarely the word
//   }

import { openContextMenu, copyText } from './menu.js';
import { flashSaved } from './inline.js';
import { controlsFor } from './controls.js';

const ctl = controlsFor('table');

const STYLE_ID = 'table-view-style';

const CSS = `
[data-view="table"] .table-wrap {
  border: 1px solid var(--line);
  border-radius: 5px;
  background: var(--sunken);
  overflow-x: auto;
}
[data-view="table"] table { border-collapse: collapse; width: 100%; font-size: 0.86rem; }
[data-view="table"] th {
  position: sticky;
  top: 0;
  z-index: 1;
  text-align: left;
  font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
  font-size: 0.72rem;
  letter-spacing: 0.06em;
  text-transform: uppercase;
  color: var(--muted);
  background: var(--surface);
  border-bottom: 1px solid var(--line-strong);
  padding: 7px 9px;
  white-space: nowrap;
  cursor: pointer;
  user-select: none;
}
[data-view="table"] th[data-sorted="asc"]::after  { content: ' ▲'; color: var(--accent); }
[data-view="table"] th[data-sorted="desc"]::after { content: ' ▼'; color: var(--accent); }
[data-view="table"] th.no-sort { cursor: default; }
[data-view="table"] td {
  border-bottom: 1px solid var(--line);
  padding: 3px 5px;
  vertical-align: top;
}
[data-view="table"] tr:last-child td { border-bottom: 0; }
[data-view="table"] tr[data-flash="yes"] td { background: var(--drop); }
[data-view="table"] .cell-input,
[data-view="table"] .cell-select {
  width: 100%;
  min-width: 6ch;
  font: inherit;
  font-size: 0.86rem;
  color: var(--text);
  background: transparent;
  border: 1px solid transparent;
  border-radius: 3px;
  padding: 4px 5px;
}
[data-view="table"] .cell-input:hover,
[data-view="table"] .cell-select:hover { border-color: var(--line); }
[data-view="table"] .cell-input:focus,
[data-view="table"] .cell-select:focus { border-color: var(--accent); background: var(--bg); outline: none; }
[data-view="table"] .cell-input[data-type="number"] { text-align: right; font-variant-numeric: tabular-nums; }
[data-view="table"] .cell-static { padding: 4px 5px; white-space: pre-wrap; }
[data-view="table"] .row-tools { white-space: nowrap; width: 1%; }
[data-view="table"] .id-cell {
  font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
  font-size: 0.74rem;
  color: var(--dim);
  padding: 6px 8px;
  white-space: nowrap;
}
[data-view="table"] .table-foot {
  display: flex;
  align-items: center;
  gap: 10px;
  margin-top: 10px;
  flex-wrap: wrap;
}
[data-view="table"] .ro-badge {
  font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
  font-size: 0.72rem; letter-spacing: 0.06em; text-transform: uppercase;
  color: var(--agent); border: 1px solid var(--agent); border-radius: 2px; padding: 2px 6px;
}
[data-view="table"] .empty { padding: 18px; color: var(--muted); }
`;

function injectStyle() {
  if (document.getElementById(STYLE_ID)) return;
  const style = document.createElement('style');
  style.id = STYLE_ID;
  style.textContent = CSS;
  document.head.append(style);
}

export function mountTable(root, ctx) {
  injectStyle();

  // Per-viewer, never persisted: how YOU are looking at the rows is not a
  // property of the data, and writing it would reorder everyone else's view.
  let sortBy = null;
  let sortDir = 'asc';

  const readOnly = () => ctx.state.readOnly === true;
  const columns = () => (Array.isArray(ctx.state.columns) ? ctx.state.columns : []).filter((c) => c && c.id);
  const rows = () => (Array.isArray(ctx.state.rows) ? ctx.state.rows : []);

  const toolbar = document.createElement('div');
  toolbar.className = 'toolbar';
  const addBtn = ctl('add', { className: 'primary-btn' });
  const badge = document.createElement('span');
  badge.className = 'ro-badge';
  badge.textContent = 'read-only';
  const hint = document.createElement('span');
  hint.className = 'hint';
  toolbar.append(addBtn, badge, hint);

  const wrap = document.createElement('div');
  wrap.className = 'table-wrap';

  const foot = document.createElement('div');
  foot.className = 'table-foot';
  const count = document.createElement('span');
  count.className = 'mono hint';
  const copyCsv = ctl('copy-csv');
  const copyMd = ctl('copy-md');
  foot.append(count, copyCsv, copyMd);

  root.append(toolbar, wrap, foot);

  function nextId() {
    if (typeof ctx.nextId === 'function') return ctx.nextId();
    let max = 0;
    for (const r of rows()) {
      const hit = /^[a-z]*(\d+)$/.exec(r && r.id);
      if (hit) max = Math.max(max, Number(hit[1]));
    }
    return 'bb' + (max + 1);
  }

  const save = () => ctx.save();

  function sorted() {
    const list = [...rows()];
    if (!sortBy) return list;
    const col = columns().find((c) => c.id === sortBy);
    const dir = sortDir === 'asc' ? 1 : -1;
    return list.sort((a, b) => {
      const x = a[sortBy], y = b[sortBy];
      if (col && col.type === 'number') return ((Number(x) || 0) - (Number(y) || 0)) * dir;
      if (col && col.type === 'checkbox') return ((x ? 1 : 0) - (y ? 1 : 0)) * dir;
      return String(x ?? '').localeCompare(String(y ?? ''), undefined, { numeric: true }) * dir;
    });
  }

  function cellFor(row, col) {
    const td = document.createElement('td');
    if (col.width) td.style.width = col.width;
    const value = row[col.id];

    if (readOnly()) {
      const span = document.createElement('div');
      span.className = 'cell-static';
      span.textContent = col.type === 'checkbox' ? (value ? '✓' : '·') : String(value ?? '');
      td.append(span);
      return td;
    }

    if (col.type === 'checkbox') {
      const box = document.createElement('input');
      box.type = 'checkbox';
      box.checked = !!value;
      box.addEventListener('change', () => { row[col.id] = box.checked; save(); });
      td.append(box);
      return td;
    }

    if (col.type === 'select') {
      const sel = document.createElement('select');
      sel.className = 'cell-select';
      const options = Array.isArray(col.options) ? col.options : [];
      // An unknown stored value must still be visible, or an agent-written value
      // outside the option list would silently vanish on the next render.
      const all = options.includes(value) || value === undefined || value === ''
        ? options
        : [...options, String(value)];
      for (const opt of ['', ...all]) {
        if (opt === '' && options.length) continue;
        const o = document.createElement('option');
        o.value = opt;
        o.textContent = opt || '—';
        sel.append(o);
      }
      sel.value = value ?? '';
      sel.addEventListener('change', () => { row[col.id] = sel.value; save(); });
      td.append(sel);
      return td;
    }

    const input = document.createElement(col.type === 'longtext' ? 'textarea' : 'input');
    input.className = 'cell-input';
    input.dataset.type = col.type || 'text';
    if (col.type === 'number') input.type = 'number';
    if (col.type === 'longtext') input.rows = 2;
    input.value = value ?? '';
    if (col.hint) input.placeholder = col.hint;
    input.addEventListener('input', () => {
      row[col.id] = col.type === 'number'
        ? (input.value === '' ? '' : Number(input.value))
        : input.value;
      save();
    });
    // Cells save as you type, which is invisible: an edit that persisted and one
    // that was lost look identical without this.
    input.addEventListener('blur', () => {
      save().then((ok) => { if (ok !== false) flashSaved(td); });
    });
    td.append(input);
    return td;
  }

  function render() {
    const ro = readOnly();
    const cols = columns();
    addBtn.hidden = ro;
    addBtn.textContent = ctx.state.addLabel || 'Add row';
    badge.hidden = !ro;
    hint.textContent = ro
      ? 'An agent maintains this table — it is yours to read.'
      : 'Click a header to sort · edits save as you type';

    wrap.replaceChildren();
    if (!cols.length) {
      const empty = document.createElement('p');
      empty.className = 'empty';
      empty.textContent = 'No columns defined yet — an agent sets state.columns.';
      wrap.append(empty);
      count.textContent = '';
      return;
    }

    const table = document.createElement('table');
    const thead = document.createElement('thead');
    const headRow = document.createElement('tr');

    const idHead = document.createElement('th');
    idHead.className = 'no-sort';
    idHead.textContent = 'id';
    headRow.append(idHead);

    for (const col of cols) {
      const th = document.createElement('th');
      th.textContent = col.label || col.id;
      if (col.id === sortBy) th.dataset.sorted = sortDir;
      th.addEventListener('click', () => {
        if (sortBy === col.id) sortDir = sortDir === 'asc' ? 'desc' : 'asc';
        else { sortBy = col.id; sortDir = 'asc'; }
        render();
      });
      headRow.append(th);
    }
    if (!ro) {
      const tools = document.createElement('th');
      tools.className = 'no-sort row-tools';
      headRow.append(tools);
    }
    thead.append(headRow);
    table.append(thead);

    const tbody = document.createElement('tbody');
    for (const row of sorted()) {
      const tr = document.createElement('tr');
      tr.dataset.id = row.id;

      const idCell = document.createElement('td');
      idCell.className = 'id-cell';
      idCell.textContent = row.id;
      tr.append(idCell);

      for (const col of cols) tr.append(cellFor(row, col));

      if (!ro) {
        const tools = document.createElement('td');
        tools.className = 'row-tools';
        const del = ctl('delete-row');
        del.addEventListener('click', () => {
          ctx.state.rows = rows().filter((r) => r.id !== row.id);
          save().then(render);
        });
        tools.append(del);
        tr.append(tools);
      }

      tr.addEventListener('contextmenu', (e) => {
        if (e.shiftKey) return;
        openContextMenu(e, [
          { head: row.id },
          { label: 'Copy id', hint: row.id, run: (ev) => copyText(row.id, ev) },
          { label: 'Copy row as markdown', run: (ev) => copyText(rowMarkdown(row, cols), ev) },
          !ro && 'separator',
          !ro && { label: 'Duplicate row', run: () => {
            const copy = { ...row, id: nextId() };
            rows().push(copy);
            save().then(render);
          } },
          !ro && { label: 'Delete row', danger: true, run: () => {
            ctx.state.rows = rows().filter((r) => r.id !== row.id);
            save().then(render);
          } },
        ]);
      });

      tbody.append(tr);
    }
    table.append(tbody);
    wrap.append(table);

    const n = rows().length;
    count.textContent = `${n} row${n === 1 ? '' : 's'}` + (sortBy ? ` · sorted by ${sortBy}` : '');
  }

  function rowMarkdown(row, cols) {
    return '| ' + [row.id, ...cols.map((c) => String(row[c.id] ?? ''))].join(' | ') + ' |';
  }

  function asCSV() {
    const cols = columns();
    const cell = (v) => {
      const s = v === undefined || v === null ? '' : String(v);
      return /[",\n]/.test(s) ? '"' + s.replace(/"/g, '""') + '"' : s;
    };
    const out = [['id', ...cols.map((c) => c.label || c.id)].map(cell).join(',')];
    for (const row of sorted()) out.push([row.id, ...cols.map((c) => row[c.id])].map(cell).join(','));
    return out.join('\n') + '\n';
  }

  function asMarkdown() {
    const cols = columns();
    const head = '| id | ' + cols.map((c) => c.label || c.id).join(' | ') + ' |';
    const rule = '|---|' + cols.map(() => '---|').join('');
    const body = sorted().map((r) => rowMarkdown(r, cols));
    return [head, rule, ...body].join('\n') + '\n';
  }

  copyCsv.addEventListener('click', (ev) => copyText(asCSV(), ev));
  copyMd.addEventListener('click', (ev) => copyText(asMarkdown(), ev));

  addBtn.addEventListener('click', () => {
    const row = { id: nextId() };
    for (const col of columns()) {
      row[col.id] = col.type === 'checkbox' ? false : '';
    }
    if (!Array.isArray(ctx.state.rows)) ctx.state.rows = [];
    ctx.state.rows.push(row);
    save().then(() => {
      render();
      const first = wrap.querySelector(`tr[data-id="${CSS.escape(row.id)}"] .cell-input, tr[data-id="${CSS.escape(row.id)}"] .cell-select`);
      first?.focus();
    });
  });

  function focus(id) {
    const tr = wrap.querySelector(`tr[data-id="${CSS.escape(String(id))}"]`);
    if (!tr) return false;
    tr.scrollIntoView({ block: 'center' });
    tr.dataset.flash = 'yes';
    setTimeout(() => { delete tr.dataset.flash; }, 2400);
    return true;
  }

  render();
  return {
    refresh() {
      // Never clobber the cell someone is typing in: the live reload fires on
      // every write, including this page's own.
      if (root.contains(document.activeElement) &&
          /INPUT|TEXTAREA|SELECT/.test(document.activeElement.tagName)) return;
      render();
    },
    focus,
  };
}
