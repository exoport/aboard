// Markup view: one or more images with a drawn-on-top layer of regions and
// pen strokes. Coordinates are normalized 0..1 relative to each image's own
// box, never pixels, so a mark stays meaningful at any zoom level and Claude
// can read "top-left corner" straight out of the numbers.

import { openContextMenu, copyText, referenceFor } from './menu.js';
import { button, controlsFor } from './controls.js';
import { PALETTES } from './controls.generated.js';
import { api } from './api.js';

const ctl = controlsFor('markup');

const SVG_NS = 'http://www.w3.org/2000/svg';
const MIN_REGION_SIZE = 0.01;   // ignore a drag this small — treat it as a stray click
const MIN_POINT_DIST = 0.005;   // thin pen points closer together than this

// A pen stroke is stored as one "x,y x,y" string, not an array of pairs.
// JSON.stringify with indentation puts every single number on its own line, so
// one scribble turned 75% of aboard.json into coordinate noise — and this file
// exists to be read. Three decimals is sub-pixel on a 900px-wide image.
const STROKE_DECIMALS = 3;

// The only colours a mark may take. Stored as the token name (never a hex),
// so the whole UI stays retheme-safe: `--mark` is the implicit default for a
// mark with no `color` field at all, so every board written before this
// feature existed keeps rendering exactly as it did.
// The mark palette, from views/markup.spec.json. Not restated here for the same
// reason ui's tones are not: an agent writing `color` is writing from that
// declaration, and colorVar() will happily build var(--anything), so a private
// second list here would silently accept names the docs never offered.
const COLOR_TOKENS = PALETTES.markup || [];
const DEFAULT_COLOR = 'mark';
const ALL_SCOPE = '__all__';

// The eight resize-handle positions as fractional offsets into a mark's own
// x/y/w/h box: [handle name, x-fraction, y-fraction].
const RESIZE_HANDLES = [
  ['nw', 0, 0], ['n', 0.5, 0], ['ne', 1, 0],
  ['w', 0, 0.5], ['e', 1, 0.5],
  ['sw', 0, 1], ['s', 0.5, 1], ['se', 1, 1],
];

export function mountMarkup(root, ctx) {
  ensureStyles();

  let tool = 'region';          // 'region' | 'ellipse' | 'pen' | 'move' | 'resize'
  let newMarkColor = DEFAULT_COLOR; // colour token newly-drawn marks get — per-viewer, never saved
  let selectedKey = null;       // compound "<imageId>::<markId>"
  let hoverKey = null;
  let drag = null;               // in-progress gesture: { type: 'region'|'pen'|'move'|'resize', pointerId, imageId, ... }
  let previewEl = null;          // live SVG preview shown while dragging
  let bulkPanelOpen = false;     // bulk-recolour picker under the marks-list header — view-only
  const rowEls = new Map();      // mark key -> list row element, kept across renders
  const imageRecords = new Map(); // image id -> persistent DOM record
  const hiddenOverlay = new Map(); // image id -> marks hidden? (view-only, never saved)

  const panel = document.createElement('div');
  panel.className = 'panel';
  root.append(panel);

  const head = document.createElement('p');
  head.className = 'panel-head';
  head.textContent = 'Markup';
  panel.append(head);

  // Paste or drop an image onto the tab. Until now markup only worked on images
  // an AGENT had put in assets/, which made "look at this" something only one
  // side could say. A pasted screenshot with two circles on it is the fastest
  // bug report there is.
  const intakeEl = document.createElement('div');
  intakeEl.className = 'markup-intake';
  const intakeHint = document.createElement('span');
  intakeHint.className = 'hint';
  intakeHint.textContent = 'Paste a screenshot (Ctrl+V) or drop an image here to add it';
  const intakeStatus = document.createElement('span');
  intakeStatus.className = 'markup-intake-status mono';
  intakeEl.append(intakeHint, intakeStatus);
  panel.append(intakeEl);

  const toolbarEl = document.createElement('div');
  toolbarEl.className = 'toolbar';
  panel.append(toolbarEl);

  const toolLabel = document.createElement('span');
  toolLabel.className = 'toolbar-label';
  toolLabel.textContent = 'Tool';
  toolbarEl.append(toolLabel);

  const regionBtn = ctl('region');
  regionBtn.setAttribute('aria-pressed', 'true');
  const ellipseBtn = ctl('ellipse');
  ellipseBtn.setAttribute('aria-pressed', 'false');
  const penBtn = ctl('pen');
  penBtn.setAttribute('aria-pressed', 'false');
  const moveBtn = ctl('move');
  moveBtn.setAttribute('aria-pressed', 'false');
  const resizeBtn = ctl('resize');
  resizeBtn.setAttribute('aria-pressed', 'false');
  toolbarEl.append(regionBtn, ellipseBtn, penBtn, moveBtn, resizeBtn);

  const resizeHintEl = document.createElement('p');
  resizeHintEl.className = 'hint markup-resize-hint';
  resizeHintEl.textContent = 'Pen strokes cannot be resized — switch to the move tool to reposition one.';
  resizeHintEl.hidden = true;
  panel.append(resizeHintEl);

  const colorToolbarEl = document.createElement('div');
  colorToolbarEl.className = 'toolbar';
  panel.append(colorToolbarEl);

  const colorLabel = document.createElement('span');
  colorLabel.className = 'toolbar-label';
  colorLabel.textContent = 'New mark colour';
  colorToolbarEl.append(colorLabel);

  const swatchWrap = document.createElement('span');
  swatchWrap.className = 'markup-swatch-group';
  colorToolbarEl.append(swatchWrap);
  for (const token of COLOR_TOKENS) {
    const btn = makeSwatchBtn(token);
    btn.title = 'New marks will be drawn in "' + token + '"';
    btn.setAttribute('aria-pressed', String(token === newMarkColor));
    btn.addEventListener('click', () => {
      newMarkColor = token;
      for (const sib of swatchWrap.children) {
        sib.setAttribute('aria-pressed', String(sib.dataset.token === newMarkColor));
      }
    });
    swatchWrap.append(btn);
  }

  const emptyEl = document.createElement('p');
  emptyEl.className = 'hint markup-empty';
  panel.append(emptyEl);

  const imagesWrapEl = document.createElement('div');
  imagesWrapEl.className = 'markup-images';
  panel.append(imagesWrapEl);

  const listHead = document.createElement('p');
  listHead.className = 'panel-head';
  listHead.textContent = 'Marks';
  panel.append(listHead);

  // A table-like header above the list. Its colour cell is the bulk-recolour
  // control (see #5): everything else is a plain column label, styled from
  // the very same chip/badge classes the data rows use so the columns line up.
  const listColHead = document.createElement('div');
  listColHead.className = 'markup-row markup-row-head';
  // Appended to listEl below, not to the panel. The header and the rows have to
  // be siblings in ONE grid or their columns are only coincidentally the same
  // width — see the subgrid note in the stylesheet.
  

  const imgHeadCell = document.createElement('span');
  imgHeadCell.className = 'mono markup-row-image';
  imgHeadCell.textContent = 'Image';
  listColHead.append(imgHeadCell);

  const idxHeadCell = document.createElement('span');
  idxHeadCell.className = 'markup-index';
  idxHeadCell.textContent = '#';
  listColHead.append(idxHeadCell);

  const chipHeadCell = document.createElement('span');
  chipHeadCell.className = 'mono markup-chip';
  chipHeadCell.textContent = 'Id';
  listColHead.append(chipHeadCell);

  const summaryHeadCell = document.createElement('span');
  summaryHeadCell.className = 'markup-summary';
  summaryHeadCell.textContent = 'Mark';
  listColHead.append(summaryHeadCell);

  const colorHeadBtn = ctl('bulk-colour', { className: 'markup-bulk-color-btn', onClick: toggleBulkColorPanel });
  listColHead.append(colorHeadBtn);

  const noteHeadCell = document.createElement('span');
  noteHeadCell.className = 'markup-note';
  noteHeadCell.textContent = 'Note';
  listColHead.append(noteHeadCell);

  const delHeadCell = document.createElement('span');
  delHeadCell.className = 'markup-row-head-spacer';
  listColHead.append(delHeadCell);

  // The bulk-recolour picker: scope first (one image, or explicitly "all
  // images" — never a bare default), then a colour, which opens the modal
  // confirmation before anything actually changes.
  const bulkPanelEl = document.createElement('div');
  bulkPanelEl.className = 'markup-bulk-panel';
  bulkPanelEl.hidden = true;

  const bulkScopeLabel = document.createElement('span');
  bulkScopeLabel.className = 'toolbar-label';
  bulkScopeLabel.textContent = 'Apply to';
  bulkPanelEl.append(bulkScopeLabel);

  const bulkScopeSelect = document.createElement('select');
  bulkScopeSelect.setAttribute('aria-label', 'Which marks the bulk colour change applies to');
  bulkPanelEl.append(bulkScopeSelect);

  const bulkSwatchWrap = document.createElement('span');
  bulkSwatchWrap.className = 'markup-swatch-group';
  for (const token of COLOR_TOKENS) {
    const btn = makeSwatchBtn(token);
    btn.title = 'Set every mark in scope to "' + token + '"';
    btn.addEventListener('click', () => confirmBulkColor(token));
    bulkSwatchWrap.append(btn);
  }
  bulkPanelEl.append(bulkSwatchWrap);

  const bulkCancelBtn = button('Cancel', '', { onClick: closeBulkColorPanel });
  bulkPanelEl.append(bulkCancelBtn);

  const listEmptyEl = document.createElement('p');
  listEmptyEl.className = 'hint';
  listEmptyEl.textContent = 'No marks yet — draw a region or a pen stroke on an image above.';
  panel.append(listEmptyEl);

  const listEl = document.createElement('div');
  listEl.className = 'markup-list';
  panel.append(listEl);
  // Order preserved from when these were three siblings of the panel: the column
  // header, then the bulk-recolour panel it opens, then the rows.
  listEl.append(listColHead);
  listEl.append(bulkPanelEl);

  /* ---------- shared modal confirm (bulk recolour / clear marks) ---------- */

  const confirmDialogEl = document.createElement('dialog');
  confirmDialogEl.className = 'sheet-dialog markup-dialog';
  panel.append(confirmDialogEl);

  const confirmMsgEl = document.createElement('p');
  confirmDialogEl.append(confirmMsgEl);

  const confirmActionsEl = document.createElement('div');
  confirmActionsEl.className = 'dialog-actions';
  confirmDialogEl.append(confirmActionsEl);

  const confirmCancelBtn = button('Cancel', '', { onClick: () => confirmDialogEl.close() });
  confirmActionsEl.append(confirmCancelBtn);

  const confirmOkBtn = button('', '', { className: 'primary-btn' });
  confirmActionsEl.append(confirmOkBtn);

  let pendingConfirm = null;
  confirmOkBtn.addEventListener('click', () => {
    const run = pendingConfirm;
    pendingConfirm = null;
    confirmDialogEl.close();
    if (run) run();
  });
  // Escape fires the dialog's native "cancel" event, which closes it without
  // ever reaching confirmOkBtn, so Escape is a no-op by construction. This
  // just guards against a stray pendingConfirm surviving that path.
  confirmDialogEl.addEventListener('close', () => { pendingConfirm = null; });

  function showConfirm(message, confirmLabel, onConfirm) {
    confirmMsgEl.textContent = message;
    confirmOkBtn.textContent = confirmLabel;
    pendingConfirm = onConfirm;
    confirmDialogEl.showModal();
  }

  /* ---------- toolbar ---------- */

  // A declaration, not a const: it is called from the toolbar setup above this
  // point, and hoisting is what makes that legal.
  function makeIconBtn(label, title) {
    return button(label, title);
  }

  function makeSwatchBtn(token) {
    const btn = ctl('swatch', { label: '', className: 'markup-swatch', ariaLabel: 'Colour: ' + token });
    btn.style.background = colorVar(token);
    btn.dataset.token = token;
    return btn;
  }

  regionBtn.addEventListener('click', () => setTool('region'));
  ellipseBtn.addEventListener('click', () => setTool('ellipse'));
  penBtn.addEventListener('click', () => setTool('pen'));
  moveBtn.addEventListener('click', () => setTool('move'));
  resizeBtn.addEventListener('click', () => setTool('resize'));

  function setTool(next) {
    tool = next;
    regionBtn.setAttribute('aria-pressed', String(tool === 'region'));
    ellipseBtn.setAttribute('aria-pressed', String(tool === 'ellipse'));
    penBtn.setAttribute('aria-pressed', String(tool === 'pen'));
    moveBtn.setAttribute('aria-pressed', String(tool === 'move'));
    resizeBtn.setAttribute('aria-pressed', String(tool === 'resize'));
    render(); // handle visibility, the pen-not-resizable hint and the svg cursor all key off the active tool
  }

  /* ---------- state shape: migration + read/write helpers ---------- */

  // Runs once at mount. The old shape was a single image's fields sitting
  // directly on state; the new shape is `images[]` + `layout`. Migrate in
  // memory and persist immediately so the marks a human already drew are
  // never at risk of being read as "no images".
  function migrateLegacyShape() {
    const s = ctx.state;
    if (!s || typeof s !== 'object' || Array.isArray(s)) return;
    if (Array.isArray(s.images)) return; // already the new shape
    if (typeof s.image !== 'string' || !s.image) return; // nothing to migrate
    // Mutate in place: ctx.state is a getter onto this tab's slice of the
    // document, so reassigning it throws and would detach us from the document.
    const migrated = [{
      id: 'i1',
      src: s.image,
      caption: typeof s.caption === 'string' ? s.caption : '',
      annotatable: true,
      regions: Array.isArray(s.regions) ? s.regions : [],
      strokes: Array.isArray(s.strokes) ? s.strokes : [],
    }];
    delete s.image;
    delete s.caption;
    delete s.regions;
    delete s.strokes;
    s.images = migrated;
    s.layout = 'stacked';
    ctx.save({ immediate: true });
  }

  // Read-only, fully-defaulted view — never mutates ctx.state, so rendering
  // alone can't invent an "images" key that wasn't there. Also understands
  // the pre-migration shape, in case migrateLegacyShape ran before a save
  // round-trip landed, or an older write comes back over refresh().
  function readState() {
    const s = ctx.state && typeof ctx.state === 'object' ? ctx.state : {};
    let rawImages = Array.isArray(s.images) ? s.images : null;
    if (!rawImages) {
      rawImages = (typeof s.image === 'string' && s.image)
        ? [{
          id: 'i1', src: s.image,
          caption: typeof s.caption === 'string' ? s.caption : '',
          annotatable: true,
          regions: Array.isArray(s.regions) ? s.regions : [],
          strokes: Array.isArray(s.strokes) ? s.strokes : [],
        }]
        : [];
    }
    const images = rawImages.map(normalizeImage);
    const layout = s.layout === 'stacked' || s.layout === 'side-by-side'
      ? s.layout
      : (images.length >= 2 ? 'side-by-side' : 'stacked');
    return { images, layout };
  }

  function normalizeImage(raw, i) {
    const im = raw && typeof raw === 'object' ? raw : {};
    return {
      id: typeof im.id === 'string' && im.id ? im.id : ('i' + (i + 1)),
      src: typeof im.src === 'string' ? im.src : '',
      caption: typeof im.caption === 'string' ? im.caption : '',
      annotatable: im.annotatable !== false, // absent -> drawable, by default
      regions: Array.isArray(im.regions) ? im.regions : [],
      strokes: Array.isArray(im.strokes) ? im.strokes : [],
    };
  }

  // Mutating accessors, used only right before a write.
  function ensureState() {
    if (!ctx.state || typeof ctx.state !== 'object' || Array.isArray(ctx.state)) {
      // Same reason as above: fill the existing object, never replace it.
      ctx.state.images = [];
      ctx.state.layout = 'stacked';
    }
    if (!Array.isArray(ctx.state.images)) ctx.state.images = [];
    return ctx.state;
  }

  function ensureImage(imageId) {
    const s = ensureState();
    const im = s.images.find((x) => x && x.id === imageId);
    if (!im) return null; // the image list itself is agent/human authored, not invented here
    if (!Array.isArray(im.regions)) im.regions = [];
    if (!Array.isArray(im.strokes)) im.strokes = [];
    return im;
  }

  function findMark(imageId, id) {
    const im = ensureImage(imageId);
    if (!im) return null;
    return im.regions.find((r) => r.id === id) || im.strokes.find((s) => s.id === id) || null;
  }

  function deleteMark(imageId, id) {
    const im = ensureImage(imageId);
    if (!im) return;
    im.regions = im.regions.filter((r) => r.id !== id);
    im.strokes = im.strokes.filter((s) => s.id !== id);
    const key = markKey(imageId, id);
    if (selectedKey === key) selectedKey = null;
    if (hoverKey === key) hoverKey = null;
    ctx.save({ immediate: true });
    render();
  }

  // Renaming: the caption is how a mark is identified in the list and in
  // conversation ("the one on image.png"), and three pasted screenshots all
  // called "image.png" are indistinguishable. Edited in place, Enter commits,
  // Escape abandons.
  function startRenameImage(imageId) {
    const rec = imageRecords.get(imageId);
    const state = readState();
    const im = state.images.find((x) => x.id === imageId);
    if (!rec || !im) return;

    const input = document.createElement('input');
    input.type = 'text';
    input.className = 'markup-caption-input';
    input.value = im.caption || '';
    input.placeholder = 'name this image';
    input.maxLength = 120;
    rec.capEl.replaceWith(input);
    input.focus();
    input.select();

    let done = false;
    const finish = (commit) => {
      if (done) return;
      done = true;
      if (commit) {
        const target = ensureImage(imageId);
        if (target) {
          const next = input.value.trim();
          if (next) target.caption = next;
          else delete target.caption;
          ctx.save({ immediate: true });
        }
      }
      input.replaceWith(rec.capEl);
      render();
    };
    input.addEventListener('keydown', (e) => {
      if (e.key === 'Enter') { e.preventDefault(); finish(true); }
      if (e.key === 'Escape') { e.preventDefault(); finish(false); }
    });
    input.addEventListener('blur', () => finish(true));
  }

  // Deleting an image takes its marks with it — they are coordinates ON it and
  // mean nothing once it is gone. Said out loud in the confirmation, with the
  // count, because "delete" hiding a second deletion is how people lose work.
  function deleteImage(imageId) {
    const state = readState();
    const im = state.images.find((x) => x.id === imageId);
    if (!im) return;
    const marks = (im.regions || []).length + (im.strokes || []).length;
    const label = '"' + (im.caption || labelForImage(im)) + '"';
    const tail = marks
      ? ` Its ${marks} mark${marks === 1 ? '' : 's'} ${marks === 1 ? 'goes' : 'go'} with it — a mark is a position on this image and means nothing without it.`
      : '';
    showConfirm(
      `Remove ${label} from this tab?${tail} The file itself stays on disk.`,
      marks ? `Remove image and ${marks} mark${marks === 1 ? '' : 's'}` : 'Remove image',
      () => {
        const live = ensureState();
        live.images = (live.images || []).filter((x) => x && x.id !== imageId);
        if (selectedKey && selectedKey.startsWith(imageId + '::')) selectedKey = null;
        if (hoverKey && hoverKey.startsWith(imageId + '::')) hoverKey = null;
        hiddenOverlay.delete(imageId);
        // Drop the DOM record too, or the figure lingers until the next reload.
        const rec = imageRecords.get(imageId);
        if (rec && rec.figureEl) rec.figureEl.remove();
        imageRecords.delete(imageId);
        for (const [key, row] of rowEls) {
          if (key.startsWith(imageId + '::')) { row.remove(); rowEls.delete(key); }
        }
        ctx.save({ immediate: true });
        render();
      },
    );
  }

  function clearImageMarks(imageId) {
    const im = ensureImage(imageId);
    if (!im) return;
    const total = im.regions.length + im.strokes.length;
    if (total === 0) return;
    const state = readState();
    const found = state.images.find((x) => x.id === imageId);
    const label = found ? ('"' + (found.caption || labelForImage(found)) + '"') : 'this image';
    showConfirm(
      `Delete all ${total} mark${total === 1 ? '' : 's'} on ${label}? This cannot be undone.`,
      'Delete marks',
      () => {
        const target = ensureImage(imageId);
        if (!target) return;
        target.regions = [];
        target.strokes = [];
        if (selectedKey && selectedKey.startsWith(imageId + '::')) selectedKey = null;
        if (hoverKey && hoverKey.startsWith(imageId + '::')) hoverKey = null;
        ctx.save({ immediate: true });
        render();
      },
    );
  }

  function toggleBulkColorPanel() {
    bulkPanelOpen = !bulkPanelOpen;
    bulkPanelEl.hidden = !bulkPanelOpen;
    if (bulkPanelOpen) renderBulkColorScope(readState());
  }

  function closeBulkColorPanel() {
    bulkPanelOpen = false;
    bulkPanelEl.hidden = true;
  }

  // Same per-image-or-all scope choice the old toolbar dropdown offered,
  // just anchored to the marks list's colour header instead.
  function renderBulkColorScope(state) {
    const annotatables = state.images.filter((im) => im.annotatable);
    const prev = bulkScopeSelect.value;
    bulkScopeSelect.replaceChildren();
    for (const im of annotatables) {
      const opt = document.createElement('option');
      opt.value = im.id;
      opt.textContent = im.caption || labelForImage(im);
      bulkScopeSelect.append(opt);
    }
    if (annotatables.length > 1) {
      const opt = document.createElement('option');
      opt.value = ALL_SCOPE;
      opt.textContent = 'All images';
      bulkScopeSelect.append(opt);
    }
    const values = Array.from(bulkScopeSelect.options, (o) => o.value);
    bulkScopeSelect.value = values.includes(prev) ? prev : values[0];
  }

  function describeScope(scope, state) {
    if (scope === ALL_SCOPE) {
      const count = state.images
        .filter((im) => im.annotatable)
        .reduce((n, im) => n + im.regions.length + im.strokes.length, 0);
      return { count, label: 'all images' };
    }
    const im = state.images.find((x) => x.id === scope);
    const count = im ? im.regions.length + im.strokes.length : 0;
    const label = im ? ('"' + (im.caption || labelForImage(im)) + '"') : 'this image';
    return { count, label };
  }

  // Clicking a swatch in the bulk panel never applies anything by itself —
  // it only ever opens the shared modal, stating exactly what is about to
  // change, so "all images" is never one accidental click away.
  function confirmBulkColor(token) {
    const state = readState();
    const scope = bulkScopeSelect.value;
    const { count, label } = describeScope(scope, state);
    closeBulkColorPanel();
    if (count === 0) return;
    showConfirm(
      `Set all ${count} mark${count === 1 ? '' : 's'} on ${label} to "${token}"?`,
      'Set colour',
      () => applyBulkColorScoped(scope, token),
    );
  }

  function applyBulkColorScoped(scope, token) {
    const s = ensureState();
    const targets = scope === ALL_SCOPE
      ? s.images.filter((im) => im && im.annotatable !== false)
      : s.images.filter((im) => im && im.id === scope);
    let changed = false;
    for (const im of targets) {
      if (!Array.isArray(im.regions)) im.regions = [];
      if (!Array.isArray(im.strokes)) im.strokes = [];
      for (const r of im.regions) { r.color = token; changed = true; }
      for (const st of im.strokes) { st.color = token; changed = true; }
    }
    if (!changed) return;
    ctx.save({ immediate: true });
    render();
  }

  // Ids follow the same pattern as the rest of the board (r1, r2… / s1, s2…):
  // the next free number after the highest existing suffix, scoped to one
  // image's own arrays, so a fresh mark never collides with one Claude added
  // to the same image while we weren't looking. Ellipses share the "r" prefix
  // with rectangles — both live in the same `regions` array.
  // Board-wide monotonic id from the shell. Clearing every mark and drawing new
  // ones must not restart at r1 — ids are how instructions reference marks.
  function nextId(list, prefix) {
    if (typeof ctx.nextId === 'function') return ctx.nextId();
    let max = 0;
    for (const item of list || []) {
      const hit = /^[a-z]*(\d+)$/.exec(item && item.id);
      if (hit) max = Math.max(max, Number(hit[1]));
    }
    return prefix + (max + 1);
  }

  /* ---------- intake: a pasted or dropped image ---------- */

  function intakeSay(text, bad) {
    intakeStatus.textContent = text;
    intakeStatus.dataset.bad = bad ? 'yes' : 'no';
    if (text) setTimeout(() => { if (intakeStatus.textContent === text) intakeStatus.textContent = ''; }, 4000);
  }

  async function addImageFile(file) {
    if (!file || !/^image\//.test(file.type || '')) {
      intakeSay('that is not an image', true);
      return;
    }
    intakeSay('uploading…');
    let payload;
    try {
      const res = await fetch(api('/upload?name=' + encodeURIComponent(file.name || 'pasted')), {
        method: 'POST',
        headers: { 'Content-Type': file.type || 'application/octet-stream' },
        body: file,
      });
      payload = await res.json().catch(() => ({}));
      if (!res.ok) { intakeSay(payload.error || 'upload refused', true); return; }
    } catch {
      intakeSay('upload failed — is the server running?', true);
      return;
    }

    const s = ensureState();
    s.images.push({
      id: nextId(s.images, 'i'),
      src: payload.url,
      caption: file.name || 'pasted image',
      annotatable: true,
      regions: [],
      strokes: [],
    });
    if (s.images.length > 1 && !s.layout) s.layout = 'side-by-side';
    intakeSay('added — draw on it to point at something');
    ctx.save({ immediate: true }).then(render);
  }

  // Paste only counts while this tab is the one on screen: a Ctrl+V meant for a
  // textarea elsewhere must not become an upload.
  function isVisible() {
    return root.isConnected && root.closest('[data-active="yes"]') !== null;
  }

  document.addEventListener('paste', (e) => {
    if (!isVisible()) return;
    const items = [...(e.clipboardData?.items || [])];
    const image = items.find((i) => i.kind === 'file' && /^image\//.test(i.type));
    if (!image) return;
    e.preventDefault();
    addImageFile(image.getAsFile());
  });

  for (const type of ['dragenter', 'dragover']) {
    root.addEventListener(type, (e) => {
      if (![...(e.dataTransfer?.types || [])].includes('Files')) return;
      e.preventDefault();
      intakeEl.dataset.over = 'yes';
    });
  }
  root.addEventListener('dragleave', (e) => {
    if (e.target === root || !root.contains(e.relatedTarget)) delete intakeEl.dataset.over;
  });
  root.addEventListener('drop', (e) => {
    const file = e.dataTransfer?.files?.[0];
    if (!file) return;
    e.preventDefault();
    delete intakeEl.dataset.over;
    addImageFile(file);
  });

  /* ---------- drawing ---------- */

  function pointerToNorm(svgEl, evt) {
    const rect = svgEl.getBoundingClientRect();
    const x = rect.width ? (evt.clientX - rect.left) / rect.width : 0;
    const y = rect.height ? (evt.clientY - rect.top) / rect.height : 0;
    return [clamp01(x), clamp01(y)];
  }

  function onPointerDown(imageId, svgEl, evt) {
    if (drag) return; // ignore a second touch while a gesture is already in flight
    const key = evt.target && evt.target.dataset ? evt.target.dataset.markKey : null;
    if (key) {
      selectMark(key);
      if (tool === 'move') startMoveDrag(imageId, key.slice(imageId.length + 2), svgEl, evt);
      return;
    }
    if (tool !== 'region' && tool !== 'ellipse' && tool !== 'pen') return; // move/resize only ever act on an existing mark
    evt.preventDefault();
    svgEl.setPointerCapture(evt.pointerId);
    const [x, y] = pointerToNorm(svgEl, evt);
    drag = tool === 'pen'
      ? { type: 'pen', pointerId: evt.pointerId, imageId, points: [[x, y]] }
      : { type: 'region', shape: tool === 'ellipse' ? 'ellipse' : 'rect', pointerId: evt.pointerId, imageId, x0: x, y0: y, x1: x, y1: y };
    updateDragPreview(svgEl);
  }

  function startMoveDrag(imageId, id, svgEl, evt) {
    const mark = findMark(imageId, id);
    if (!mark) return;
    evt.preventDefault();
    svgEl.setPointerCapture(evt.pointerId);
    const [x, y] = pointerToNorm(svgEl, evt);
    if (mark.points !== undefined) {
      drag = {
        type: 'move', kind: 'pen', pointerId: evt.pointerId, imageId, id,
        startX: x, startY: y, curX: x, curY: y,
        origPoints: decodePoints(mark.points),
      };
    } else {
      drag = {
        type: 'move', kind: 'box', pointerId: evt.pointerId, imageId, id,
        startX: x, startY: y, curX: x, curY: y,
        orig: { x: mark.x, y: mark.y, w: mark.w, h: mark.h },
        shape: mark.shape === 'ellipse' ? 'ellipse' : 'rect',
      };
    }
    updateDragPreview(svgEl);
  }

  // A resize handle lives outside the <svg> (see buildFigure), so it wires up
  // its own short-lived pointermove/up/cancel listeners on itself rather than
  // going through onPointerMove/onPointerUp, which only ever see events that
  // bubble from inside the svg.
  function onHandlePointerDown(imageId, id, handle, rec, evt) {
    if (drag) return;
    const mark = findMark(imageId, id);
    if (!mark || mark.points !== undefined) return; // only rect/ellipse marks resize
    evt.preventDefault();
    evt.stopPropagation();
    const target = evt.currentTarget;
    target.setPointerCapture(evt.pointerId);
    const [x, y] = pointerToNorm(rec.svgEl, evt);
    drag = {
      type: 'resize', pointerId: evt.pointerId, imageId, id, handle,
      orig: { x: mark.x, y: mark.y, w: mark.w, h: mark.h },
      shape: mark.shape === 'ellipse' ? 'ellipse' : 'rect',
      curX: x, curY: y,
    };
    updateDragPreview(rec.svgEl);

    const move = (e2) => {
      if (!drag || e2.pointerId !== drag.pointerId) return;
      const [x2, y2] = pointerToNorm(rec.svgEl, e2);
      drag.curX = x2;
      drag.curY = y2;
      updateDragPreview(rec.svgEl);
    };
    const finish = (commit) => (e2) => {
      if (!drag || e2.pointerId !== drag.pointerId) return;
      target.removeEventListener('pointermove', move);
      target.removeEventListener('pointerup', up);
      target.removeEventListener('pointercancel', cancel);
      if (target.hasPointerCapture && target.hasPointerCapture(e2.pointerId)) target.releasePointerCapture(e2.pointerId);
      if (commit) commitDrag();
      drag = null;
      clearDragPreview();
      render();
    };
    const up = finish(true);
    const cancel = finish(false);
    target.addEventListener('pointermove', move);
    target.addEventListener('pointerup', up);
    target.addEventListener('pointercancel', cancel);
  }

  function onPointerMove(svgEl, evt) {
    if (!drag || evt.pointerId !== drag.pointerId) return;
    const [x, y] = pointerToNorm(svgEl, evt);
    if (drag.type === 'region') {
      drag.x1 = x;
      drag.y1 = y;
    } else if (drag.type === 'pen') {
      const last = drag.points[drag.points.length - 1];
      if (Math.hypot(x - last[0], y - last[1]) >= MIN_POINT_DIST) drag.points.push([x, y]);
    } else if (drag.type === 'move') {
      drag.curX = x;
      drag.curY = y;
    }
    updateDragPreview(svgEl);
  }

  function onPointerUp(svgEl, evt) {
    if (!drag || evt.pointerId !== drag.pointerId) return;
    svgEl.releasePointerCapture(evt.pointerId);
    commitDrag();
    drag = null;
    clearDragPreview();
    render();
  }

  function onPointerCancel(evt) {
    if (!drag || evt.pointerId !== drag.pointerId) return;
    drag = null;
    clearDragPreview();
    render();
  }

  function commitDrag() {
    const im = ensureImage(drag.imageId);
    if (!im) return;
    if (drag.type === 'region') {
      const x = Math.min(drag.x0, drag.x1);
      const y = Math.min(drag.y0, drag.y1);
      const w = Math.abs(drag.x1 - drag.x0);
      const h = Math.abs(drag.y1 - drag.y0);
      if (w < MIN_REGION_SIZE || h < MIN_REGION_SIZE) return; // too small — treat as a stray click
      const id = nextId(im.regions, 'r');
      const mark = { id, x: round4(x), y: round4(y), w: round4(w), h: round4(h), note: '' };
      if (drag.shape === 'ellipse') mark.shape = 'ellipse';
      if (newMarkColor !== DEFAULT_COLOR) mark.color = newMarkColor;
      im.regions.push(mark);
      selectedKey = markKey(drag.imageId, id);
      ctx.save();
    } else if (drag.type === 'pen') {
      if (drag.points.length < 2) return;
      const id = nextId(im.strokes, 's');
      const mark = { id, points: encodePoints(drag.points), note: '' };
      if (newMarkColor !== DEFAULT_COLOR) mark.color = newMarkColor;
      im.strokes.push(mark);
      selectedKey = markKey(drag.imageId, id);
      ctx.save();
    } else if (drag.type === 'move') {
      if (drag.kind === 'pen') {
        const mark = im.strokes.find((s) => s.id === drag.id);
        if (!mark) return;
        const bbox = pointsBBox(drag.origPoints);
        const dx = clampAxisDelta(bbox.minX, bbox.maxX, drag.curX - drag.startX);
        const dy = clampAxisDelta(bbox.minY, bbox.maxY, drag.curY - drag.startY);
        mark.points = encodePoints(drag.origPoints.map(([px, py]) => [px + dx, py + dy]));
      } else {
        const mark = im.regions.find((r) => r.id === drag.id);
        if (!mark) return;
        const dx = clampAxisDelta(drag.orig.x, drag.orig.x + drag.orig.w, drag.curX - drag.startX);
        const dy = clampAxisDelta(drag.orig.y, drag.orig.y + drag.orig.h, drag.curY - drag.startY);
        mark.x = round4(drag.orig.x + dx);
        mark.y = round4(drag.orig.y + dy);
      }
      ctx.save();
    } else if (drag.type === 'resize') {
      const mark = im.regions.find((r) => r.id === drag.id);
      if (!mark) return;
      const box = computeResizedBox(drag.handle, drag.orig, drag.curX, drag.curY);
      mark.x = round4(box.x);
      mark.y = round4(box.y);
      mark.w = round4(box.w);
      mark.h = round4(box.h);
      ctx.save();
    }
  }

  function updateDragPreview(svgEl) {
    const tag = dragPreviewTag();
    if (!previewEl || previewEl.tagName.toLowerCase() !== tag) {
      clearDragPreview();
      previewEl = document.createElementNS(SVG_NS, tag);
      previewEl.setAttribute('class', 'markup-preview');
      previewEl.setAttribute('vector-effect', 'non-scaling-stroke');
      if (tag === 'polyline') previewEl.setAttribute('fill', 'none');
      svgEl.append(previewEl);
    }
    if (tag === 'polyline') {
      const pts = dragPreviewPoints();
      previewEl.setAttribute('points', pts.map((p) => p[0] + ',' + p[1]).join(' '));
      return;
    }
    const box = dragPreviewBox();
    if (tag === 'ellipse') {
      previewEl.setAttribute('cx', String(box.x + box.w / 2));
      previewEl.setAttribute('cy', String(box.y + box.h / 2));
      previewEl.setAttribute('rx', String(box.w / 2));
      previewEl.setAttribute('ry', String(box.h / 2));
    } else {
      previewEl.setAttribute('x', String(box.x));
      previewEl.setAttribute('y', String(box.y));
      previewEl.setAttribute('width', String(box.w));
      previewEl.setAttribute('height', String(box.h));
    }
  }

  function dragPreviewTag() {
    if (drag.type === 'pen' || (drag.type === 'move' && drag.kind === 'pen')) return 'polyline';
    return drag.shape === 'ellipse' ? 'ellipse' : 'rect';
  }

  function dragPreviewPoints() {
    if (drag.type === 'pen') return drag.points;
    const bbox = pointsBBox(drag.origPoints);
    const dx = clampAxisDelta(bbox.minX, bbox.maxX, drag.curX - drag.startX);
    const dy = clampAxisDelta(bbox.minY, bbox.maxY, drag.curY - drag.startY);
    return drag.origPoints.map(([x, y]) => [clamp01(x + dx), clamp01(y + dy)]);
  }

  function dragPreviewBox() {
    if (drag.type === 'region') {
      return {
        x: Math.min(drag.x0, drag.x1), y: Math.min(drag.y0, drag.y1),
        w: Math.abs(drag.x1 - drag.x0), h: Math.abs(drag.y1 - drag.y0),
      };
    }
    if (drag.type === 'resize') return computeResizedBox(drag.handle, drag.orig, drag.curX, drag.curY);
    // drag.type === 'move' && drag.kind === 'box'
    const dx = clampAxisDelta(drag.orig.x, drag.orig.x + drag.orig.w, drag.curX - drag.startX);
    const dy = clampAxisDelta(drag.orig.y, drag.orig.y + drag.orig.h, drag.curY - drag.startY);
    return { x: drag.orig.x + dx, y: drag.orig.y + dy, w: drag.orig.w, h: drag.orig.h };
  }

  function clearDragPreview() {
    if (previewEl) previewEl.remove();
    previewEl = null;
  }

  /* ---------- selection / hover ---------- */

  function markKey(imageId, id) {
    return imageId + '::' + id;
  }

  function selectMark(key) {
    selectedKey = key;
    applyHighlightClasses();
    if (tool === 'resize') render(); // handles track the selection; keep them in sync on every reselect
    const row = rowEls.get(key);
    if (row) row.scrollIntoView({ block: 'nearest', behavior: reducedMotion() ? 'auto' : 'smooth' });
  }

  function applyHighlightClasses() {
    for (const rec of imageRecords.values()) {
      if (!rec.svgEl) continue;
      for (const el of rec.svgEl.querySelectorAll('[data-mark-key]')) {
        const key = el.dataset.markKey;
        el.classList.toggle('is-selected', key === selectedKey);
        el.classList.toggle('is-hover', key !== selectedKey && key === hoverKey);
      }
      for (const el of rec.labelsEl.children) {
        const key = el.dataset.markKey;
        el.classList.toggle('is-selected', key === selectedKey);
        el.classList.toggle('is-hover', key !== selectedKey && key === hoverKey);
      }
    }
    for (const [key, row] of rowEls) {
      row.classList.toggle('is-selected', key === selectedKey);
      row.classList.toggle('is-hover', key !== selectedKey && key === hoverKey);
    }
  }

  /* ---------- rendering: images ---------- */

  function render() {
    if (drag) return; // never redraw out from under a live gesture
    const state = readState();
    const hasAnnotatable = state.images.some((im) => im.annotatable);
    toolbarEl.hidden = !hasAnnotatable;
    colorToolbarEl.hidden = !hasAnnotatable;
    renderImages(state);
    for (const rec of imageRecords.values()) {
      if (rec.svgEl) rec.svgEl.dataset.tool = tool; // cursor (grab/resize/crosshair) keys off this
    }
    renderBulkColorScope(state);
    const totalMarks = state.images
      .filter((im) => im.annotatable)
      .reduce((n, im) => n + im.regions.length + im.strokes.length, 0);
    colorHeadBtn.disabled = totalMarks === 0;
    renderList(state);
    updateResizeHint(state);
    applyHighlightClasses();
  }

  function updateResizeHint(state) {
    let showHint = false;
    if (tool === 'resize' && selectedKey) {
      for (const im of state.images) {
        if (!selectedKey.startsWith(im.id + '::')) continue;
        const id = selectedKey.slice(im.id.length + 2);
        if (im.strokes.some((s) => s.id === id)) showHint = true;
        break;
      }
    }
    resizeHintEl.hidden = !showHint;
  }

  function renderImages(state) {
    const hasImages = state.images.length > 0;
    emptyEl.hidden = hasImages;
    imagesWrapEl.hidden = !hasImages;
    if (!hasImages) {
      emptyEl.textContent = 'No image set. Add an "images" array (each with a "src" path relative to the board folder) to aboard.json.';
      return;
    }
    imagesWrapEl.dataset.layout = state.layout;
    imagesWrapEl.style.setProperty('--markup-cols', String(state.images.length));

    const seen = new Set();
    for (const im of state.images) {
      seen.add(im.id);
      let rec = imageRecords.get(im.id);
      if (!rec || rec.annotatable !== im.annotatable) {
        if (rec) rec.figureEl.remove();
        rec = buildFigure(im.id, im.annotatable);
        imageRecords.set(im.id, rec);
      }
      updateFigure(rec, im);
      if (rec.svgEl) {
        renderShapesForImage(im, rec);
        renderHandlesForImage(im, rec);
      }
      imagesWrapEl.append(rec.figureEl); // re-appending an existing node moves it, keeping order in sync
    }
    for (const [id, rec] of imageRecords) {
      if (!seen.has(id)) {
        rec.figureEl.remove();
        imageRecords.delete(id);
        hiddenOverlay.delete(id);
        if (selectedKey && selectedKey.startsWith(id + '::')) selectedKey = null;
        if (hoverKey && hoverKey.startsWith(id + '::')) hoverKey = null;
      }
    }
  }

  function buildFigure(imageId, annotatable) {
    const figureEl = document.createElement('div');
    figureEl.className = 'markup-figure';
    figureEl.dataset.imageId = imageId;

    const headRow = document.createElement('div');
    headRow.className = 'markup-figure-head';
    figureEl.append(headRow);

    const capEl = document.createElement('span');
    capEl.className = 'hint markup-figure-caption';
    headRow.append(capEl);

    const spacer = document.createElement('span');
    spacer.className = 'spacer';
    headRow.append(spacer);

    const renameBtn = ctl('rename-image');
    renameBtn.addEventListener('click', () => startRenameImage(imageId));
    headRow.append(renameBtn);

    let hideBtn = null;
    let clearBtn = null;
    if (annotatable) {
      hideBtn = ctl('hide-marks');
      hideBtn.setAttribute('aria-pressed', 'false');
      hideBtn.addEventListener('click', () => {
        hiddenOverlay.set(imageId, !hiddenOverlay.get(imageId));
        render();
      });
      headRow.append(hideBtn);

      clearBtn = ctl('clear-marks');
      clearBtn.addEventListener('click', () => clearImageMarks(imageId));
      headRow.append(clearBtn);
    }

    const removeBtn = ctl('remove-image');
    removeBtn.classList.add('icon-btn--danger');
    removeBtn.addEventListener('click', () => deleteImage(imageId));
    headRow.append(removeBtn);

    const stageEl = document.createElement('div');
    stageEl.className = 'markup-stage';
    figureEl.append(stageEl);

    const imgEl = document.createElement('img');
    imgEl.className = 'markup-img';
    imgEl.addEventListener('error', () => {
      const rec = imageRecords.get(imageId);
      if (!rec || rec.failed) return;
      rec.failed = true;
      render();
    });
    imgEl.addEventListener('load', () => {
      const rec = imageRecords.get(imageId);
      if (!rec || !rec.failed) return;
      rec.failed = false;
      render();
    });
    stageEl.append(imgEl);

    const failEl = document.createElement('p');
    failEl.className = 'hint markup-fail';
    failEl.hidden = true;
    stageEl.append(failEl);

    let svgEl = null;
    let labelsEl = null;
    let handlesEl = null;
    if (annotatable) {
      svgEl = document.createElementNS(SVG_NS, 'svg');
      svgEl.setAttribute('class', 'markup-svg');
      svgEl.setAttribute('viewBox', '0 0 1 1');
      svgEl.setAttribute('preserveAspectRatio', 'none');
      stageEl.append(svgEl);

      labelsEl = document.createElement('div');
      labelsEl.className = 'markup-labels';
      stageEl.append(labelsEl);

      // A plain, unscaled HTML layer for resize handles: the svg above uses
      // viewBox="0 0 1 1" with preserveAspectRatio="none", so any shape drawn
      // inside it stretches to the image's own aspect ratio. A fixed-px div
      // positioned by percentage — the same trick markup-labels already uses
      // for its circular numbers — stays a true, constant-size square instead.
      handlesEl = document.createElement('div');
      handlesEl.className = 'markup-handles';
      stageEl.append(handlesEl);

      svgEl.addEventListener('pointerdown', (evt) => onPointerDown(imageId, svgEl, evt));
      svgEl.addEventListener('pointermove', (evt) => onPointerMove(svgEl, evt));
      svgEl.addEventListener('pointerup', (evt) => onPointerUp(svgEl, evt));
      svgEl.addEventListener('pointercancel', onPointerCancel);
    }

    return {
      id: imageId, figureEl, capEl, hideBtn, clearBtn,
      stageEl, imgEl, failEl, svgEl, labelsEl, handlesEl,
      annotatable, failed: false,
    };
  }

  function updateFigure(rec, im) {
    rec.capEl.textContent = im.caption || '(unnamed)';
    rec.capEl.hidden = false;
    rec.capEl.title = 'Rename this image';
    rec.capEl.style.cursor = 'text';
    if (!rec.capBound) {
      rec.capBound = true;
      rec.capEl.addEventListener('click', () => startRenameImage(im.id));
    }
    if (rec.clearBtn) rec.clearBtn.disabled = im.regions.length + im.strokes.length === 0;

    const hasSrc = im.src !== '';
    rec.stageEl.hidden = !hasSrc;
    if (!hasSrc) return;

    if (rec.imgEl.dataset.src !== im.src) {
      rec.imgEl.dataset.src = im.src;
      rec.failed = false;
      rec.imgEl.alt = im.caption || 'Image';
      rec.imgEl.src = api(im.src);
    }
    rec.imgEl.hidden = rec.failed;
    rec.failEl.hidden = !rec.failed;
    if (rec.failed) rec.failEl.textContent = `Image failed to load: "${im.src}".`;

    applyOverlayVisibility(im.id);
  }

  // Hiding is view-only (never written to ctx.state) and never blocks a
  // later refresh() from rebuilding the shapes underneath it — it only ever
  // toggles a display class on top of whatever renderShapesForImage drew.
  function applyOverlayVisibility(imageId) {
    const rec = imageRecords.get(imageId);
    if (!rec || !rec.svgEl) return;
    const userHidden = !!hiddenOverlay.get(imageId);
    const hidden = userHidden || rec.failed;
    rec.svgEl.classList.toggle('is-marks-hidden', hidden);
    rec.labelsEl.classList.toggle('is-marks-hidden', hidden);
    rec.handlesEl.classList.toggle('is-marks-hidden', hidden);
    rec.hideBtn.disabled = rec.failed;
    rec.hideBtn.setAttribute('aria-pressed', String(userHidden));
    rec.hideBtn.textContent = userHidden ? '👁 Show marks' : '👁 Hide marks';
  }

  function renderShapesForImage(im, rec) {
    rec.svgEl.replaceChildren();
    rec.labelsEl.replaceChildren();
    let index = 0;
    for (const r of im.regions) {
      index += 1;
      rec.svgEl.append(buildRegionShape(im.id, r));
      rec.labelsEl.append(buildLabel(im.id, r.x, r.y, index, r.id, colorToken(r)));
    }
    for (const s of im.strokes) {
      index += 1;
      const pts = decodePoints(s.points);
      rec.svgEl.append(buildStrokeShape(im.id, s));
      if (pts.length) rec.labelsEl.append(buildLabel(im.id, pts[0][0], pts[0][1], index, s.id, colorToken(s)));
    }
  }

  // Resize handles only ever decorate the single selected rect/ellipse, and
  // only while the resize tool is active — everything else leaves this layer
  // empty.
  function renderHandlesForImage(im, rec) {
    rec.handlesEl.replaceChildren();
    if (tool !== 'resize' || !selectedKey || !selectedKey.startsWith(im.id + '::')) return;
    const id = selectedKey.slice(im.id.length + 2);
    const mark = im.regions.find((r) => r.id === id); // pen strokes never get handles
    if (!mark) return;
    const w = Math.max(0, Number(mark.w) || 0);
    const h = Math.max(0, Number(mark.h) || 0);
    const baseX = clamp01(mark.x);
    const baseY = clamp01(mark.y);
    for (const [handle, fx, fy] of RESIZE_HANDLES) {
      const el = document.createElement('div');
      el.className = 'markup-handle';
      el.dataset.handle = handle;
      el.title = 'Drag to resize';
      el.style.left = ((baseX + fx * w) * 100) + '%';
      el.style.top = ((baseY + fy * h) * 100) + '%';
      el.addEventListener('pointerdown', (evt) => onHandlePointerDown(im.id, id, handle, rec, evt));
      rec.handlesEl.append(el);
    }
  }

  // Handles both bbox shapes stored in `regions`: an absent/"rect" `shape`
  // draws a <rect>, "ellipse" draws an <ellipse> from that same x/y/w/h box.
  function buildRegionShape(imageId, r) {
    const isEllipse = r.shape === 'ellipse';
    const el = document.createElementNS(SVG_NS, isEllipse ? 'ellipse' : 'rect');
    const x = clamp01(r.x);
    const y = clamp01(r.y);
    const w = Math.max(0, Number(r.w) || 0);
    const h = Math.max(0, Number(r.h) || 0);
    if (isEllipse) {
      el.setAttribute('cx', String(x + w / 2));
      el.setAttribute('cy', String(y + h / 2));
      el.setAttribute('rx', String(w / 2));
      el.setAttribute('ry', String(h / 2));
    } else {
      el.setAttribute('x', String(x));
      el.setAttribute('y', String(y));
      el.setAttribute('width', String(w));
      el.setAttribute('height', String(h));
    }
    el.setAttribute('vector-effect', 'non-scaling-stroke');
    el.setAttribute('class', 'markup-shape markup-shape--region');
    const colorV = colorVar(colorToken(r));
    el.style.stroke = colorV;
    el.style.fill = colorV;
    el.dataset.markKey = markKey(imageId, r.id);
    return el;
  }

  function buildStrokeShape(imageId, s) {
    const pts = decodePoints(s.points);
    const attr = pts.map((p) => clamp01(p[0]) + ',' + clamp01(p[1])).join(' ');
    const el = document.createElementNS(SVG_NS, 'polyline');
    el.setAttribute('points', attr);
    el.setAttribute('fill', 'none');
    el.setAttribute('vector-effect', 'non-scaling-stroke');
    el.setAttribute('class', 'markup-shape markup-shape--pen');
    el.style.stroke = colorVar(colorToken(s));
    el.dataset.markKey = markKey(imageId, s.id);
    return el;
  }

  function buildLabel(imageId, x, y, index, id, token) {
    const span = document.createElement('span');
    span.className = 'markup-label';
    span.style.left = (clamp01(x) * 100) + '%';
    span.style.top = (clamp01(y) * 100) + '%';
    span.style.background = colorVar(token);
    // The ID, not a counter. Counters were per image, so every image had a "1"
    // and none of them agreed with the list — and an id is what you would type
    // into a message anyway. One identifier on the image, in the table, and in a
    // sentence.
    span.textContent = String(id);
    span.title = String(id);
    span.dataset.markKey = markKey(imageId, id);
    return span;
  }

  function labelForImage(im) {
    return im.id;
  }

  // A stack block's ctx.tab.id is "<tab>/<block>"; a link needs the tab.
  function tabIdOf() {
    return String((ctx.tab && ctx.tab.id) || '').split('/')[0];
  }

  // What a mark is, in one line you can paste into a message: which image, where
  // on it, and what the human said about it.
  function markMarkdown(imageId, id) {
    const state = readState();
    const im = state.images.find((x) => x.id === imageId);
    const mark = findMark(imageId, id);
    if (!im || !mark) return id;
    const where = mark.points
      ? 'stroke'
      : `${Math.round((mark.x || 0) * 100)}%,${Math.round((mark.y || 0) * 100)}%` +
        ` ${Math.round((mark.w || 0) * 100)}×${Math.round((mark.h || 0) * 100)}%`;
    const note = mark.note ? ` — ${mark.note}` : '';
    return `- \`${id}\` on ${im.caption || labelForImage(im)} at ${where}${note}`;
  }

  /* ---------- rendering: marks list ---------- */

  function renderList(state) {
    const showImageTag = state.images.length > 1;
    // EMPTIED, never hidden — here and in the rows. This was `hidden` on both,
    // which is `display: none`, and a display:none grid item is not placed in
    // the grid at all: with a single image every cell slid one column left into
    // a track sized for something else. The id landed in the 22px mark-number
    // track and rendered as "bb"; the delete button landed in the note track and
    // became a full-width box with an ✕ adrift in it. Reported 2026-08-27 as
    // "the marks table columns are odd", which was exactly right.
    //
    // The column is 0px when there is one image, and `.markup-row-image:empty`
    // drops its own padding and border, so the cell holds its place and occupies
    // no space.
    listEl.style.setProperty('--markup-img-col', showImageTag ? 'minmax(6ch, 18ch)' : '0px');
    imgHeadCell.textContent = showImageTag ? 'Image' : '';
    const entries = [];
    // Numbered across the whole tab, not per image. Restarting at 1 for every
    // image meant "mark 1" named two different things as soon as a second image
    // arrived — and pointing at things is the entire purpose of this view.
    let idx = 0;
    for (const im of state.images) {
      if (!im.annotatable) continue;
      const imageLabel = im.caption || labelForImage(im);
      for (const r of im.regions) {
        idx += 1;
        entries.push({ imageId: im.id, imageLabel, id: r.id, type: r.shape === 'ellipse' ? 'ellipse' : 'region', obj: r, index: idx });
      }
      for (const s of im.strokes) {
        idx += 1;
        entries.push({ imageId: im.id, imageLabel, id: s.id, type: 'pen', obj: s, index: idx });
      }
    }

    const present = new Set();
    for (const entry of entries) {
      const key = markKey(entry.imageId, entry.id);
      present.add(key);
      let row = rowEls.get(key);
      if (!row) {
        row = buildRow(entry.imageId, entry.id);
        rowEls.set(key, row);
      }
      updateRow(row, entry, showImageTag);
      listEl.append(row); // re-appending an existing node moves it, keeping list order in sync
    }
    for (const [key, row] of rowEls) {
      if (!present.has(key)) {
        row.remove();
        rowEls.delete(key);
      }
    }
    listEmptyEl.hidden = entries.length !== 0;
  }

  function buildRow(imageId, id) {
    const key = markKey(imageId, id);
    const row = document.createElement('div');
    row.className = 'markup-row';
    row.dataset.markKey = key;

    const imgTag = document.createElement('span');
    imgTag.className = 'mono markup-row-image';
    row.append(imgTag);

    const index = document.createElement('span');
    index.className = 'markup-index';
    row.append(index);

    const chip = document.createElement('span');
    chip.className = 'mono markup-chip';
    row.append(chip);

    const summary = document.createElement('span');
    summary.className = 'markup-summary';
    row.append(summary);

    // Per-mark recolour stays an instant, un-confirmed action — only the
    // header's bulk control (#5) is gated behind the modal.
    const swatchGroup = document.createElement('span');
    swatchGroup.className = 'markup-swatch-group';
    for (const token of COLOR_TOKENS) {
      const btn = makeSwatchBtn(token);
      btn.title = 'Recolour this mark to "' + token + '"';
      btn.addEventListener('click', () => {
        const mark = findMark(imageId, id);
        if (!mark) return;
        mark.color = token;
        ctx.save({ immediate: true });
        render();
      });
      swatchGroup.append(btn);
    }
    row.append(swatchGroup);

    const note = document.createElement('input');
    note.type = 'text';
    note.className = 'markup-note';
    note.placeholder = 'Note for the agent…';
    note.addEventListener('input', () => {
      const mark = findMark(imageId, id);
      if (!mark) return;
      mark.note = note.value;
      ctx.save();
    });
    note.addEventListener('blur', () => ctx.save({ immediate: true }));
    row.append(note);

    const del = ctl('delete-mark', { onClick: () => deleteMark(imageId, id) });
    row.append(del);

    // Right-click: the id, which is the thing you actually want out of this row —
    // it is how you name this mark to an agent, and reading it off the screen and
    // typing it back was the only way to get it.
    row.addEventListener('contextmenu', (e) => {
      if (e.shiftKey) return;
      const state = readState();
      const im = state.images.find((x) => x.id === imageId);
      const mark = findMark(imageId, id);
      const label = im ? (im.caption || labelForImage(im)) : imageId;
      openContextMenu(e, [
        { head: id },
        { label: 'Copy mark id', hint: id, run: (ev) => copyText(id, ev) },
        { label: 'Copy link to this mark', hint: 'tab + mark',
          run: (ev) => copyText(referenceFor(tabIdOf(), id), ev) },
        { label: 'Copy image name', hint: label.length > 18 ? label.slice(0, 18) + '…' : label,
          run: (ev) => copyText(label, ev) },
        mark && mark.note && { label: 'Copy the note', run: (ev) => copyText(String(mark.note), ev) },
        { label: 'Copy as markdown', run: (ev) => copyText(markMarkdown(imageId, id), ev) },
        'separator',
        { label: 'Delete this mark', danger: true, run: () => deleteMark(imageId, id) },
      ]);
    });

    row.addEventListener('mouseenter', () => { hoverKey = key; applyHighlightClasses(); });
    row.addEventListener('mouseleave', () => { hoverKey = null; applyHighlightClasses(); });
    row.addEventListener('click', (evt) => {
      if (evt.target === note || evt.target === del || evt.target.closest('.markup-swatch')) return;
      selectMark(key);
    });

    return row;
  }

  function updateRow(row, entry, showImageTag) {
    const imgTag = row.querySelector('.markup-row-image');
    // EMPTIED, never hidden. `hidden` is `display: none`, and a display:none grid
    // item is not placed in the grid at all — so with a single image every cell
    // in this row slid one column left while the header's own image cell stayed
    // put. The id landed in the 22px mark-number track and rendered as "bb"; the
    // delete button landed in the note track and became a full-width box with an
    // ✕ floating in the middle of it. Reported 2026-08-27 as "the marks table
    // columns are odd", which was exactly right.
    //
    // The column is 0px when there is one image, so an empty cell costs nothing
    // and keeps the row seven cells wide whatever is on screen.
    imgTag.textContent = showImageTag ? entry.imageLabel : '';
    // The column is capped and ellipsised, so the full name has to be reachable
    // somewhere — three screenshots all called "image.png" are otherwise
    // indistinguishable in the list.
    imgTag.title = showImageTag ? entry.imageLabel : '';
    row.querySelector('.markup-index').textContent = String(entry.index);
    row.querySelector('.markup-chip').textContent = entry.id;
    row.querySelector('.markup-summary').textContent = summarize(entry);
    updateRowSwatches(row.querySelector('.markup-swatch-group'), colorToken(entry.obj));
    row.classList.toggle('is-image-hidden', !!hiddenOverlay.get(entry.imageId));
    const note = row.querySelector('.markup-note');
    note.setAttribute('aria-label', 'Note for mark ' + entry.id);
    // Never stomp on text the human is mid-typing when an external refresh lands.
    if (document.activeElement !== note) note.value = entry.obj.note || '';
  }

  function updateRowSwatches(wrap, currentToken) {
    for (const btn of wrap.children) {
      btn.setAttribute('aria-pressed', String(btn.dataset.token === currentToken));
    }
  }

  function summarize(entry) {
    if (entry.type === 'pen') {
      const n = decodePoints(entry.obj.points).length;
      return `pen · ${n} point${n === 1 ? '' : 's'}`;
    }
    const r = entry.obj;
    return `${entry.type} · x ${pct(r.x)}% y ${pct(r.y)}% · ${pct(r.w)}%×${pct(r.h)}%`;
  }

  function pct(v) {
    return Math.round(clamp01(v) * 100);
  }

  /* ---------- colour token resolution ---------- */

  function colorToken(mark) {
    const c = mark && mark.color;
    return COLOR_TOKENS.indexOf(c) !== -1 ? c : DEFAULT_COLOR;
  }

  function colorVar(token) {
    return 'var(--' + token + ')';
  }

  migrateLegacyShape();
  render();

  return {
    refresh() {
      render();
    },
  };
}

/* ---------- small numeric helpers ---------- */

function clamp01(v) {
  const n = Number(v);
  if (!Number.isFinite(n)) return 0;
  return Math.min(1, Math.max(0, n));
}

function encodePoints(points) {
  const f = (v) => Number(clamp01(v).toFixed(STROKE_DECIMALS));
  return points.map(([x, y]) => `${f(x)},${f(y)}`).join(' ');
}

// Accepts the compact string form and the older array-of-pairs form, so a
// aboard.json written before this change still renders.
function decodePoints(raw) {
  if (typeof raw === 'string') {
    return raw.trim().split(/\s+/).reduce((out, pair) => {
      const [x, y] = pair.split(',').map(Number);
      if (Number.isFinite(x) && Number.isFinite(y)) out.push([x, y]);
      return out;
    }, []);
  }
  if (!Array.isArray(raw)) return [];
  return raw.filter((p) => Array.isArray(p) && Number.isFinite(p[0]) && Number.isFinite(p[1]));
}

function round4(v) {
  return Math.round(v * 10000) / 10000;
}

// Resizes a rect/ellipse bounding box from one handle, keeping the opposite
// edge(s) fixed and normalising so a drag past the far edge still yields a
// positive width/height, then enforces the same minimum size a fresh draw
// does (shrinking from the anchor side, so the fixed edge stays put).
function computeResizedBox(handle, orig, px, py) {
  let x = orig.x, y = orig.y, w = orig.w, h = orig.h;
  const affectsX = handle !== 'n' && handle !== 's';
  const affectsY = handle !== 'e' && handle !== 'w';
  if (affectsX) {
    const west = handle === 'nw' || handle === 'sw' || handle === 'w';
    const anchorX = west ? orig.x + orig.w : orig.x;
    x = Math.min(anchorX, px);
    w = Math.abs(px - anchorX);
    if (w < MIN_REGION_SIZE) {
      w = MIN_REGION_SIZE;
      x = west ? anchorX - w : anchorX;
    }
  }
  if (affectsY) {
    const north = handle === 'nw' || handle === 'ne' || handle === 'n';
    const anchorY = north ? orig.y + orig.h : orig.y;
    y = Math.min(anchorY, py);
    h = Math.abs(py - anchorY);
    if (h < MIN_REGION_SIZE) {
      h = MIN_REGION_SIZE;
      y = north ? anchorY - h : anchorY;
    }
  }
  x = Math.min(Math.max(x, 0), Math.max(0, 1 - w));
  y = Math.min(Math.max(y, 0), Math.max(0, 1 - h));
  return { x, y, w, h };
}

// Clamps a translation delta so [min+delta, max+delta] stays inside 0..1 —
// shared by whole-mark move (bbox = the mark itself) and pen-stroke move
// (bbox = the stroke's own extent), so a drag never pushes a mark off-image.
function clampAxisDelta(min, max, delta) {
  const lower = -min;
  const upper = 1 - max;
  if (lower > upper) return 0; // the mark already spans the full 0..1 axis — nowhere to go
  return Math.min(Math.max(delta, lower), upper);
}

function pointsBBox(points) {
  let minX = 1, minY = 1, maxX = 0, maxY = 0;
  for (const [x, y] of points) {
    if (x < minX) minX = x;
    if (y < minY) minY = y;
    if (x > maxX) maxX = x;
    if (y > maxY) maxY = y;
  }
  return { minX, minY, maxX, maxY };
}

function reducedMotion() {
  return typeof window.matchMedia === 'function'
    && window.matchMedia('(prefers-reduced-motion: reduce)').matches;
}

/* ---------- styling ---------- */

function ensureStyles() {
  if (document.getElementById('markup-view-style')) return;
  const style = document.createElement('style');
  style.id = 'markup-view-style';
  style.textContent = `
[data-view="markup"] .markup-intake {
  display: flex;
  align-items: center;
  gap: 10px;
  margin: 0 0 12px;
  padding: 7px 11px;
  border: 1px dashed var(--line-strong);
  border-radius: 4px;
  background: var(--sunken);
}
[data-view="markup"] .markup-intake[data-over="yes"] {
  border-color: var(--accent);
  background: var(--drop);
}
[data-view="markup"] .markup-intake-status { font-size: 0.78rem; color: var(--accent); }
[data-view="markup"] .markup-intake-status[data-bad="yes"] { color: var(--danger); }
[data-view="markup"] .markup-images {
  margin: 0 0 16px;
}
[data-view="markup"] .markup-images[data-layout="side-by-side"] {
  display: grid;
  grid-template-columns: repeat(var(--markup-cols, 2), minmax(0, 1fr));
  gap: 16px;
  align-items: start;
}
[data-view="markup"] .markup-images[data-layout="stacked"] {
  display: flex;
  flex-direction: column;
  gap: 16px;
}
@media (max-width: 760px) {
  [data-view="markup"] .markup-images[data-layout="side-by-side"] {
    grid-template-columns: minmax(0, 1fr);
  }
}
[data-view="markup"] .markup-figure { min-width: 0; }
[data-view="markup"] .markup-figure-head {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-bottom: 6px;
}
[data-view="markup"] .markup-figure-caption { margin: 0; }
[data-view="markup"] .markup-stage {
  position: relative;
  width: 100%;
  /* No max-width. It was 900px, which on a wide board or a maximised panel left
     most of the row empty while the thing you are trying to point AT was
     rendered small -- reported 2026-08-27. Marks are stored as fractions of the
     image, so every one of them survives any scale; the only thing a cap bought
     was a smaller picture. side-by-side still constrains through its grid
     column. */
  background: var(--sunken);
  border: 1px solid var(--line);
  border-radius: 4px;
  overflow: hidden;
}
[data-view="markup"] .markup-img { display: block; width: 100%; height: auto; }
[data-view="markup"] .markup-svg {
  position: absolute;
  inset: 0;
  width: 100%;
  height: 100%;
  touch-action: none;
  cursor: crosshair;
}
[data-view="markup"] .markup-svg[data-tool="move"] { cursor: default; }
[data-view="markup"] .markup-svg[data-tool="move"] .markup-shape { cursor: grab; }
[data-view="markup"] .markup-svg[data-tool="move"] .markup-shape:active { cursor: grabbing; }
[data-view="markup"] .markup-svg[data-tool="resize"] { cursor: default; }
[data-view="markup"] .markup-svg.is-marks-hidden,
[data-view="markup"] .markup-labels.is-marks-hidden,
[data-view="markup"] .markup-handles.is-marks-hidden { display: none; }
[data-view="markup"] .markup-labels { position: absolute; inset: 0; pointer-events: none; }
[data-view="markup"] .markup-shape { stroke: var(--mark); stroke-width: 2; }
[data-view="markup"] .markup-shape--region { fill-opacity: 0.14; }
[data-view="markup"] .markup-shape--pen {
  fill: none;
  stroke-width: 2.5;
  stroke-linecap: round;
  stroke-linejoin: round;
}
/* Selection/hover must read as "selected" even when a mark's own colour is
   the same token used for the accent — so the ring is a neutral halo, never
   a colour swap, kept independent from the palette a mark can be. */
[data-view="markup"] .markup-shape.is-hover {
  stroke-width: 2.75;
  filter: drop-shadow(0 0 2px var(--line-strong));
}
[data-view="markup"] .markup-shape.is-selected {
  stroke-width: 3;
  filter: drop-shadow(0 0 2px var(--line-strong)) drop-shadow(0 0 2px var(--line-strong));
}
[data-view="markup"] .markup-preview {
  stroke: var(--accent);
  stroke-width: 1.5;
  stroke-dasharray: 0.01 0.01;
  fill: var(--accent);
  fill-opacity: 0.08;
  pointer-events: none;
}
[data-view="markup"] .markup-label {
  /* A chip, not a bubble: it holds an id like "bb168" now, not one digit. */
  position: absolute;
  transform: translate(-50%, -50%);
  height: 15px;
  padding: 0 4px;
  display: flex;
  align-items: center;
  justify-content: center;
  border-radius: 3px;
  color: var(--accent-ink);
  font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
  font-size: 0.67rem;
  font-weight: 650;
  letter-spacing: 0.01em;
  line-height: 1;
  white-space: nowrap;
  border: 1px solid var(--surface);
}
[data-view="markup"] .markup-label.is-hover,
[data-view="markup"] .markup-label.is-selected { box-shadow: 0 0 0 2px var(--line-strong); }
[data-view="markup"] .markup-handles {
  position: absolute;
  inset: 0;
  pointer-events: none;
}
[data-view="markup"] .markup-handle {
  position: absolute;
  width: 9px;
  height: 9px;
  transform: translate(-50%, -50%);
  background: var(--accent);
  border: 1px solid var(--surface);
  border-radius: 1px;
  pointer-events: auto;
  touch-action: none;
}
[data-view="markup"] .markup-handle:hover { background: var(--focus); }
[data-view="markup"] .markup-handle[data-handle="nw"],
[data-view="markup"] .markup-handle[data-handle="se"] { cursor: nwse-resize; }
[data-view="markup"] .markup-handle[data-handle="ne"],
[data-view="markup"] .markup-handle[data-handle="sw"] { cursor: nesw-resize; }
[data-view="markup"] .markup-handle[data-handle="n"],
[data-view="markup"] .markup-handle[data-handle="s"] { cursor: ns-resize; }
[data-view="markup"] .markup-handle[data-handle="e"],
[data-view="markup"] .markup-handle[data-handle="w"] { cursor: ew-resize; }
[data-view="markup"] .markup-empty,
[data-view="markup"] .markup-fail {
  border: 1px dashed var(--line);
  border-radius: 4px;
  padding: 20px;
  text-align: center;
  max-width: 900px;
}
[data-view="markup"] .markup-fail { padding: 12px; margin: 0; }
[data-view="markup"] .markup-resize-hint { margin: -6px 0 12px; }
[data-view="markup"] .markup-swatch-group { display: flex; align-items: center; gap: 6px; }
[data-view="markup"] .markup-swatch {
  width: 18px;
  height: 18px;
  padding: 0;
  border-radius: 50%;
  border: 1px solid var(--line-strong);
  cursor: pointer;
}
[data-view="markup"] .markup-swatch:hover { border-color: var(--text); }
[data-view="markup"] .markup-swatch[aria-pressed="true"] {
  outline: 2px solid var(--accent);
  outline-offset: 2px;
}
[data-view="markup"] .markup-bulk-color-btn {
  font: inherit;
  font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
  font-size: 0.72rem;
  letter-spacing: 0.05em;
  text-transform: uppercase;
  color: var(--muted);
  background: transparent;
  border: 1px dashed var(--line-strong);
  border-radius: 3px;
  padding: 2px 6px;
  cursor: pointer;
  flex: 0 0 auto;
}
[data-view="markup"] .markup-bulk-color-btn:hover:not(:disabled) {
  color: var(--text);
  border-color: var(--accent);
}
[data-view="markup"] .markup-bulk-color-btn:disabled { opacity: 0.35; cursor: default; }
[data-view="markup"] .markup-bulk-panel {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 10px;
  padding: 8px 10px;
  margin: -2px 0 10px;
  background: var(--surface);
  border: 1px solid var(--line);
  border-radius: 3px;
}
[data-view="markup"] .markup-bulk-panel[hidden] { display: none; }
[data-view="markup"] .markup-dialog p { margin: 0 0 14px; }
[data-view="markup"] .markup-caption-input { font-size: 0.83rem; max-width: 34ch; }
/* ONE grid for the header and every row, which is the only way their columns can
   be the same width rather than coincidentally similar.

   Each row used to declare this same template itself. Grid aligns tracks within
   a container and a row was its own container, so max-content on the colour
   track resolved to the bulk-recolour BUTTON in the header and to five 18px
   swatches in a row, and the fr tracks then split whatever was left of two
   different remainders. The COLOUR label sat over empty space and NOTE sat to
   the left of the note it labelled. Reported 2026-08-27.

   subgrid is what makes a row a box -- border, background, hover, the selected
   left rule -- while still taking its columns from the list. A row of
   display:contents would have shared the tracks too and thrown all of that
   away.

   No backticks in here, deliberately: this stylesheet is a template literal and
   one backtick in a comment ends it, which takes the whole shell down rather
   than the styling. See CLAUDE.md. */
[data-view="markup"] .markup-list {
  display: grid;
  grid-template-columns:
    var(--markup-img-col, 0px)   /* image name, 0 when there is only one image */
    22px                          /* mark number */
    minmax(5ch, max-content)      /* bb id */
    minmax(9ch, 1fr)              /* what and where */
    max-content                   /* colour swatches */
    minmax(10ch, 2fr)             /* note */
    24px;                         /* delete */
  column-gap: 8px;
  row-gap: 6px;
}
/* The two full-width children of the list that are not rows. */
[data-view="markup"] .markup-list > .markup-bulk-panel { grid-column: 1 / -1; }
[data-view="markup"] .markup-row {
  grid-column: 1 / -1;
  display: grid;
  grid-template-columns: subgrid;
  align-items: center;
  padding: 6px 8px;
  border: 1px solid var(--line);
  border-left: 3px solid transparent;
  border-radius: 3px;
  background: var(--surface);
}
[data-view="markup"] .markup-row.is-hover { border-color: var(--accent); }
[data-view="markup"] .markup-row.is-selected { border-color: var(--accent); border-left-color: var(--accent); }
[data-view="markup"] .markup-row.is-image-hidden { opacity: 0.55; }
[data-view="markup"] .markup-row-head {
  cursor: default;
  background: var(--sunken);
}
[data-view="markup"] .markup-row-head .markup-summary,
[data-view="markup"] .markup-row-head .markup-note {
  font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
  font-size: 0.72rem;
  letter-spacing: 0.05em;
  text-transform: uppercase;
  color: var(--muted);
}
[data-view="markup"] .markup-row-head-spacer { display: inline-block; width: 24px; flex: 0 0 auto; }
[data-view="markup"] .markup-index {
  width: 20px;
  height: 20px;
  display: flex;
  align-items: center;
  justify-content: center;
  border-radius: 50%;
  background: var(--sunken);
  border: 1px solid var(--line);
  color: var(--muted);
  font-size: 0.79rem;
}
[data-view="markup"] .markup-chip {
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  padding: 2px 6px;
  background: var(--sunken);
  border: 1px solid var(--line);
  border-radius: 3px;
  font-size: 0.79rem;
}
/* An empty image cell keeps its grid slot and gives up its box. See the note at
   the showImageTag assignment: taking it out of the flow instead is what shifted
   every column left. */
[data-view="markup"] .markup-row-image:empty { padding: 0; border: 0; }
[data-view="markup"] .markup-row-image {
  min-width: 0;
  max-width: 100%;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  padding: 2px 6px;
  background: var(--sunken);
  border: 1px solid var(--line);
  border-left: 2px solid var(--edge);
  border-radius: 3px;
  font-size: 0.72rem;
  color: var(--muted);
}
[data-view="markup"] .markup-summary { min-width: 0; color: var(--muted); font-size: 0.84rem; }
[data-view="markup"] .markup-row-head-spacer { width: 24px; }
[data-view="markup"] .markup-note { flex: 1 1 200px; min-width: 0; }
@media (prefers-reduced-motion: no-preference) {
  [data-view="markup"] .markup-shape,
  [data-view="markup"] .markup-label,
  [data-view="markup"] .markup-row {
    transition: stroke-width 120ms ease, background-color 120ms ease, border-color 120ms ease, opacity 120ms ease;
  }
}
`;
  document.head.append(style);
}
