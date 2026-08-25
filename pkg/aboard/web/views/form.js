// Form view: Claude asks questions by writing `state.form` in aboard.json,
// the human answers here, and every edit writes straight back to disk.

import { controlsFor } from './controls.js';

const ctl = controlsFor('form');

let styleInjected = false;

function injectStyle() {
  if (styleInjected) return;
  styleInjected = true;
  const style = document.createElement('style');
  style.textContent = `
    [data-view="form"] .form-wrap { max-width: 620px; }
    [data-view="form"] .fields { display: flex; flex-direction: column; gap: 16px; margin-top: 12px; }
    [data-view="form"] .field { display: flex; flex-direction: column; gap: 6px; }
    [data-view="form"] .field-head { display: flex; align-items: baseline; gap: 8px; flex-wrap: wrap; }
    [data-view="form"] .field-head label { font-weight: 600; }
    [data-view="form"] .chip {
      font-size: 0.73rem;
      color: var(--muted);
      background: var(--sunken);
      border: 1px solid var(--line);
      border-radius: 2px;
      padding: 1px 5px;
    }
    [data-view="form"] .range-row { display: flex; align-items: center; gap: 10px; }
    [data-view="form"] .range-row input[type="range"] { flex: 1 1 auto; }
    [data-view="form"] textarea { width: 100%; min-height: 4.5em; }
    [data-view="form"] input[type="text"] { width: 100%; }
    [data-view="form"] select { width: 100%; }
    [data-view="form"] .save-note {
      font-size: 0.79rem;
      color: var(--accent);
      opacity: 0;
      transition: opacity 0.4s ease;
    }
    [data-view="form"] .save-note.show { opacity: 1; }
    [data-view="form"] .toolbar { margin: 14px 0 0; }
  `;
  document.head.append(style);
}

// One string per field's (id, type) pair. Unchanged string => same shape,
// so refresh() can update values in place instead of re-rendering.
function fingerprint(fields) {
  return fields.map((f) => `${f.id}:${f.type}`).join('|');
}

function neutralValue(field) {
  switch (field.type) {
    case 'checkbox': return false;
    case 'range': return field.min ?? 0;
    case 'select': return Array.isArray(field.options) && field.options.length ? field.options[0] : '';
    default: return '';
  }
}

export function mountForm(root, ctx) {
  injectStyle();

  let fp = null;                    // fingerprint of the last rendered field set
  let inputs = new Map();           // field.id -> { kind, input, out? }
  let saveNoteEl = null;
  let saveNoteTimer = null;

  const getFields = () => {
    const form = ctx.state && ctx.state;
    return form && Array.isArray(form.fields) ? form.fields : [];
  };
  const getField = (id) => getFields().find((f) => f.id === id) || null;

  function flashSaved(ok) {
    if (!saveNoteEl) return;
    clearTimeout(saveNoteTimer);
    saveNoteEl.textContent = ok ? 'Answers saved' : 'Save failed — check the server';
    saveNoteEl.classList.add('show');
    saveNoteTimer = setTimeout(() => saveNoteEl.classList.remove('show'), 2000);
  }

  function commit(fieldId, value, immediate) {
    const field = getField(fieldId);
    if (!field) return;
    field.value = value;
    const p = immediate ? ctx.save({ immediate: true }) : ctx.save();
    p.then(flashSaved).catch(() => flashSaved(false));
  }

  function buildCheckbox(field, domId) {
    const input = document.createElement('input');
    input.type = 'checkbox';
    input.id = domId;
    input.checked = !!field.value;
    input.addEventListener('change', () => commit(field.id, input.checked, true));
    inputs.set(field.id, { kind: 'checkbox', input });
    return input;
  }

  function buildRange(field, domId) {
    const row = document.createElement('div');
    row.className = 'range-row';
    const min = field.min ?? 0;
    const max = field.max ?? 10;
    const step = field.step ?? 1;
    const value = field.value ?? min;
    const input = document.createElement('input');
    input.type = 'range';
    input.id = domId;
    input.min = String(min);
    input.max = String(max);
    input.step = String(step);
    input.value = String(value);
    const out = document.createElement('span');
    out.className = 'mono';
    out.textContent = String(value);
    input.addEventListener('input', () => {
      out.textContent = input.value;
      commit(field.id, Number(input.value), false);
    });
    inputs.set(field.id, { kind: 'range', input, out });
    row.append(input, out);
    return row;
  }

  function buildSelect(field, domId) {
    const select = document.createElement('select');
    select.id = domId;
    const options = Array.isArray(field.options) ? field.options : [];
    for (const opt of options) {
      const o = document.createElement('option');
      o.value = opt;
      o.textContent = opt;
      select.append(o);
    }
    select.value = field.value ?? (options[0] ?? '');
    select.addEventListener('change', () => commit(field.id, select.value, true));
    inputs.set(field.id, { kind: 'select', input: select });
    return select;
  }

  // Shared by the two plain-text field types; only the tag name differs.
  function buildTextLike(field, domId, kind) {
    const input = document.createElement(kind === 'textarea' ? 'textarea' : 'input');
    if (kind === 'text') input.type = 'text';
    input.id = domId;
    input.value = field.value ?? '';
    if (field.placeholder) input.placeholder = field.placeholder;
    input.addEventListener('input', () => commit(field.id, input.value, false));
    inputs.set(field.id, { kind, input });
    return input;
  }

  function renderField(field, idx) {
    const row = document.createElement('div');
    row.className = 'field';
    const domId = `form-input-${idx}`;

    const head = document.createElement('div');
    head.className = 'field-head';
    const label = document.createElement('label');
    label.setAttribute('for', domId);
    label.textContent = field.label || field.id;
    const chip = document.createElement('span');
    chip.className = 'chip mono';
    chip.textContent = field.id;
    head.append(label, chip);
    row.append(head);

    let control;
    switch (field.type) {
      case 'checkbox': control = buildCheckbox(field, domId); break;
      case 'range': control = buildRange(field, domId); break;
      case 'select': control = buildSelect(field, domId); break;
      case 'text': control = buildTextLike(field, domId, 'text'); break;
      case 'textarea': control = buildTextLike(field, domId, 'textarea'); break;
      default: {
        control = document.createElement('p');
        control.className = 'hint';
        control.textContent = `Unsupported field type: "${field.type}"`;
      }
    }
    row.append(control);

    if (field.hint) {
      const hint = document.createElement('p');
      hint.className = 'hint';
      hint.textContent = field.hint;
      row.append(hint);
    }
    return row;
  }

  function onReset() {
    const fields = getFields();
    if (fields.length === 0) return;
    if (!window.confirm('Reset all answers to their neutral defaults?')) return;
    for (const field of fields) field.value = neutralValue(field);
    syncValues(true);
    ctx.save({ immediate: true }).then(flashSaved).catch(() => flashSaved(false));
  }

  // Pushes current state values into their DOM inputs. Skips inputs that
  // currently have focus unless forced, so an external aboard.json change
  // never clobbers text the human is mid-typing.
  function syncValues(force) {
    for (const field of getFields()) {
      const entry = inputs.get(field.id);
      if (!entry) continue;
      if (document.activeElement === entry.input && !force) continue;
      if (entry.kind === 'checkbox') entry.input.checked = !!field.value;
      else if (entry.kind === 'range') {
        entry.input.value = String(field.value);
        entry.out.textContent = String(field.value);
      } else {
        entry.input.value = field.value ?? '';
      }
    }
  }

  function render() {
    const form = ctx.state && ctx.state;
    const fields = getFields();
    fp = fingerprint(fields);
    inputs = new Map();
    root.textContent = '';

    const panel = document.createElement('div');
    panel.className = 'panel form-wrap';

    if (!form || fields.length === 0) {
      const p = document.createElement('p');
      p.className = 'hint';
      p.textContent = 'No questions right now.';
      panel.append(p);
      root.append(panel);
      return;
    }

    const heading = document.createElement('h2');
    heading.className = 'panel-head';
    heading.textContent = form.title || 'Form';
    panel.append(heading);

    if (form.intro) {
      const intro = document.createElement('p');
      intro.className = 'hint';
      intro.textContent = form.intro;
      panel.append(intro);
    }

    const list = document.createElement('div');
    list.className = 'fields';
    fields.forEach((field, idx) => list.append(renderField(field, idx)));
    panel.append(list);

    const foot = document.createElement('div');
    foot.className = 'toolbar';
    const resetBtn = ctl('reset', { onClick: onReset });
    saveNoteEl = document.createElement('span');
    saveNoteEl.className = 'save-note';
    foot.append(resetBtn, saveNoteEl);
    panel.append(foot);

    root.append(panel);
  }

  function refresh() {
    const newFp = fingerprint(getFields());
    if (newFp !== fp) {
      render();
      return;
    }
    syncValues(false);
  }

  render();
  return { refresh };
}
