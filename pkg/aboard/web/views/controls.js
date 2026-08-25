// controls.js — how a renderer makes a button, and how it declares one.
//
// Two functions, and the difference between them is the whole point:
//
//   controlsFor('dag')('relayout')   a control the RENDERER owns. Its label and
//                                    title come from views/dag.spec.json, so the
//                                    thing on screen and the thing an agent reads
//                                    are one declaration and one edit.
//
//   button('Send', 'title…')         a plain button. For agent-authored content
//                                    (a `ui` button carries a label the agent
//                                    wrote) and for chrome that belongs to no
//                                    renderer — the context menu, the inline
//                                    editor, a dialog's Cancel.
//
// Why a split rather than declaring every button: whether a button is a
// CAPABILITY or merely an affordance is a judgement, and no rule decides it. A
// dialog's Cancel is not something an agent needs told about; a delete-row button
// is. Two calls make that choice visible in review instead of burying it.
//
// The history: the same `button(label, title)` helper had been written three
// times — dag.js, html.js and markup.js — around 42 hand-rolled
// createElement('button') sites that each restated type, className and title in
// their own order. Half set a title, half did not, and nothing connected any of
// them to the `gestures` an agent reads. That is how `table` shipped a delete-row
// button documented nowhere while SKILL.md advertised the feature.
//
// The deeper reason is drift. State fields do not silently disagree with their
// spec, because `aboard apply` READS the declaration — it is load-bearing, so a wrong one
// produces a wrong warning and somebody fixes it. `gestures` had no consumer at
// all, so nothing broke when it went stale. Rendering FROM the declaration gives
// that half a consumer: get an id wrong and the button says so on screen.

import { CONTROLS } from './controls.generated.js';

// Every declared control carries data-gesture. A sweep keys on that, so renaming
// a label never breaks the check and a tooltip never has to be worded to match a
// sentence — the two are written for different readers and may read differently.
export const GESTURE_ATTR = 'data-gesture';

/**
 * A button. `className` defaults to icon-btn, which is what nearly every call
 * site used; pass `primary-btn`, `chip`, or an extra class where it differs.
 */
export function button(label, title, opts = {}) {
  const b = document.createElement('button');
  // 'button' unless asked otherwise: a bare <button> inside a <form> defaults to
  // submit, and several of these live in dialog forms. The one place that wants
  // the submit behaviour asks for it.
  b.type = opts.type || 'button';
  b.className = opts.className || 'icon-btn';
  if (label !== undefined && label !== null && label !== '') b.textContent = String(label);
  if (title) b.title = String(title);
  if (opts.ariaLabel) b.setAttribute('aria-label', String(opts.ariaLabel));
  if (opts.pressed !== undefined) b.setAttribute('aria-pressed', String(!!opts.pressed));
  if (opts.disabled) b.disabled = true;
  if (opts.onClick) b.addEventListener('click', opts.onClick);
  return b;
}

/**
 * controlsFor('dag') returns a maker bound to that renderer's declarations.
 *
 *   const ctl = controlsFor('dag');
 *   ctl('relayout', { onClick: … })
 *   ctl('score', { label: String(i), title: `score ${i}` })   // per-instance
 *
 * An override is for what the declaration cannot know: a label that toggles
 * ("Follow" / "Following"), or a per-item title naming the item. The declaration
 * stays the canonical description of the capability; the override is one
 * instance of it.
 */
export function controlsFor(type) {
  const declared = CONTROLS[type] || {};
  return function control(id, opts = {}) {
    const spec = declared[id];
    // A missing declaration renders as a visible marker, not as a blank button —
    // the same choice `ui` makes for an unknown component type, and for the same
    // reason. Silently falling back to something plausible would let an
    // undeclared control ship looking correct, which is the exact failure this
    // file exists to stop.
    const label = opts.label !== undefined ? opts.label : (spec ? spec.label : `?${id}`);
    const title = opts.title !== undefined ? opts.title
      : (spec ? spec.title : `undeclared control "${id}" — add it to views/${type}.spec.json`);
    const b = button(label, title, opts);
    b.setAttribute(GESTURE_ATTR, id);
    if (!spec) b.dataset.undeclared = 'yes';
    return b;
  };
}
