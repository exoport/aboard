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
  // Zoom and pan, per image: { z, tx, ty } with tx/ty in STAGE pixels.
  //
  // Per viewer and never saved, exactly like the hidden-marks toggle, the
  // selection and the drawing tool. Two people can read one board in the same
  // second and must disagree about zoom while agreeing about content — the same
  // rule that keeps scroll and theme out of the document.
  const zoomState = new Map();
  // The crop tool's transient rectangle, per image: {x,y,w,h} in image
  // fractions. NOT a mark: it is never given an id, never written, and never
  // survives a click elsewhere. It exists to be copied.
  const cropSel = new Map();
  // Below 100% as well as above it: a screenshot that is taller than the window
  // is unreadable at 1:1 and there was no way to pull back from it.
  const MIN_ZOOM = 0.2;
  const MAX_ZOOM = 8;

  function zoomOf(imageId) {
    return zoomState.get(imageId) || { z: 1, tx: 0, ty: 0 };
  }

  // Clamp the pan so the image can never be dragged off its own stage: at z=1
  // there is nowhere to go, and at any z the visible window stays inside the
  // picture. Without this, panning a zoomed image ends with an empty box and no
  // way back but Fit.
  function clampPan(rec, view) {
    const w = rec.stageEl.clientWidth;
    const h = rec.stageEl.clientHeight;
    // Zoomed IN: the visible window has to stay inside the picture, or panning
    // ends on an empty box with no way back but Fit.
    //
    // Zoomed OUT: there is nothing to pan to, and the picture is smaller than
    // its frame — so it is centred rather than pinned to a corner, which is what
    // every image viewer does and the only arrangement that does not look like a
    // layout bug.
    if (view.z < 1) {
      view.tx = (w - w * view.z) / 2;
      view.ty = (h - h * view.z) / 2;
      return view;
    }
    const maxX = w * (view.z - 1);
    const maxY = h * (view.z - 1);
    view.tx = Math.min(0, Math.max(-maxX, view.tx));
    view.ty = Math.min(0, Math.max(-maxY, view.ty));
    return view;
  }

  function applyZoom(rec) {
    const view = clampPan(rec, zoomOf(rec.id));
    zoomState.set(rec.id, view);
    rec.zoomEl.style.transform = `translate(${view.tx}px, ${view.ty}px) scale(${view.z})`;
    rec.stageEl.dataset.zoomed = view.z > 1 ? 'yes' : 'no';
    if (!rec.zoomLabel) return;
    // What the reader can actually measure off the screen: the size of a pixel
    // of the ORIGINAL, not the zoom factor. A screenshot wider than the row is
    // shrunk to fit it, and the readout said 100% while the picture was at
    // something like 60% — so "100%" meant two different things depending on how
    // wide the board happened to be. Reported 2026-08-27 with a picture of it.
    //
    // Fit is still z = 1; it is the LABEL that stops pretending z is the answer.
    const nat = rec.imgEl.naturalWidth;
    const shown = rec.imgEl.clientWidth;
    const effective = nat > 0 && shown > 0 ? (shown / nat) * view.z : view.z;
    rec.zoomLabel.textContent = Math.round(effective * 100) + '%';
    // Everything worth saying about zoom, in the one place that costs no layout:
    // the readout's own tooltip. There WAS an on-screen hint about panning — in
    // the head row, where it shuffled every button beside it, and then as a
    // fading overlay on the picture. Both were reported as annoying, and they
    // were: a permanent instruction is clutter for everyone who has read it
    // once. The gesture is declared in views/markup.spec.json, so the help panel
    // lists it for anybody looking for it, which is where somebody looks.
    rec.zoomLabel.title = nat > 0
      ? `${Math.round(effective * 100)}% of the original ${nat}px. Fit is whatever fills the row.`
        + ' Drag to pan with the Move tool, or with the middle button and any tool.'
      : '';
  }

  // Zoom about a point, so the pixel under the cursor stays under the cursor.
  // Zooming about the corner instead is the thing that makes a viewer feel like
  // it is fighting you.
  function zoomAt(rec, factor, px, py) {
    const view = zoomOf(rec.id);
    const next = Math.min(MAX_ZOOM, Math.max(MIN_ZOOM, view.z * factor));
    if (next === view.z) return;
    const k = next / view.z;
    view.tx = px - k * (px - view.tx);
    view.ty = py - k * (py - view.ty);
    view.z = next;
    zoomState.set(rec.id, view);
    applyZoom(rec);
  }

  function resetZoom(imageId) {
    zoomState.set(imageId, { z: 1, tx: 0, ty: 0 });
    const rec = imageRecords.get(imageId);
    if (rec) applyZoom(rec);
  }

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
  // The paste bar and the tools, stuck to the top of the scroll.
  //
  // With one image they were always on screen; with six they were a scroll away
  // from whichever picture you were working on, so choosing a colour meant
  // scrolling up and finding your place again. Reported 2026-08-27.
  //
  // Offset by --head-h, which aboard.html measures and republishes: the shell's
  // own head is sticky above this, and its height changes when the tab strip
  // wraps or a banner appears. A constant here would be right at one width.
  const stickyEl = document.createElement('div');
  stickyEl.className = 'markup-sticky';
  panel.append(stickyEl);

  const intakeEl = document.createElement('div');
  intakeEl.className = 'markup-intake';
  const intakeHint = document.createElement('span');
  intakeHint.className = 'hint';
  intakeHint.textContent = 'Paste a screenshot (Ctrl+V) or drop an image here to add it';
  const intakeStatus = document.createElement('span');
  intakeStatus.className = 'markup-intake-status mono';
  intakeEl.append(intakeHint, intakeStatus);
  stickyEl.append(intakeEl);

  const toolbarEl = document.createElement('div');
  toolbarEl.className = 'toolbar';
  stickyEl.append(toolbarEl);

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
  const cropBtn = ctl('crop');
  cropBtn.setAttribute('aria-pressed', 'false');
  toolbarEl.append(regionBtn, ellipseBtn, penBtn, moveBtn, resizeBtn, cropBtn);

  // The two copies, next to the tool that makes a selection for them. Disabled
  // until there IS one, because a button that silently does nothing is the thing
  // this board keeps being caught by.
  const copyRegionBtn = ctl('copy-region');
  copyRegionBtn.addEventListener('click', () => copyCrop(false));
  const copySeenBtn = ctl('copy-seen');
  copySeenBtn.addEventListener('click', () => copyCrop(true));
  const cropAddBtn = ctl('crop-to-image');
  cropAddBtn.addEventListener('click', () => { void cropToImage(); });
  toolbarEl.append(copyRegionBtn, copySeenBtn, cropAddBtn);

  const copyStatusEl = document.createElement('span');
  copyStatusEl.className = 'hint markup-copy-status';
  toolbarEl.append(copyStatusEl);

  const resizeHintEl = document.createElement('p');
  resizeHintEl.className = 'hint markup-resize-hint';
  resizeHintEl.textContent = 'Pen strokes cannot be resized — switch to the move tool to reposition one.';
  resizeHintEl.hidden = true;
  panel.append(resizeHintEl);

  const colorToolbarEl = document.createElement('div');
  colorToolbarEl.className = 'toolbar';
  stickyEl.append(colorToolbarEl);

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

  // The bulk-recolour picker, and there is ONE of it. Each image slice has its
  // own marks table, so this panel is MOVED into whichever slice's colour header
  // opened it — appending a node relocates it — rather than built fifteen times.
  // Its scope select still offers "all images", which is the whole reason it is
  // not simply per-image state.
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

  // The picture a refused copy falls back to. Built once, filled on demand.
  const imageDialogEl = document.createElement('dialog');
  // NOT `markup-dialog`: that class names the renderer's confirm dialog and is
  // what `dialog.markup-dialog` in the browser suite reaches for. A second
  // dialog wearing it makes that locator match two elements and the assertion
  // fails on strict mode rather than on behaviour — which is exactly what
  // happened when this was written.
  imageDialogEl.className = 'sheet-dialog markup-image-dialog';
  const imageDialogTitle = document.createElement('p');
  imageDialogTitle.className = 'panel-head';
  imageDialogEl.append(imageDialogTitle);
  const imageDialogWhy = document.createElement('p');
  imageDialogWhy.className = 'hint';
  imageDialogEl.append(imageDialogWhy);
  const imageDialogImg = document.createElement('img');
  imageDialogImg.className = 'markup-offer-img';
  imageDialogEl.append(imageDialogImg);

  const imageDialogAlt = document.createElement('p');
  imageDialogAlt.className = 'hint';
  imageDialogAlt.textContent = 'In a browser you can also right-click the picture to copy or save it. '
    + 'A VS Code panel has its own context menu and does not offer that.';
  imageDialogEl.append(imageDialogAlt);
  const imageDialogActions = document.createElement('div');
  imageDialogActions.className = 'dialog-actions';
  // The way out that needs no clipboard at all, offered where the refusal is
  // read rather than only in a toolbar the human has just been let down by.
  const imageDialogAdd = button('Add this picture to the tab', '', {
    className: 'primary-btn',
    onClick: () => { void addOffered(); },
  });
  imageDialogActions.append(button('Close', '', { onClick: () => imageDialogEl.close() }));
  imageDialogActions.append(imageDialogAdd);
  imageDialogEl.append(imageDialogActions);
  panel.append(imageDialogEl);

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
  cropBtn.addEventListener('click', () => setTool('crop'));

  // The crop rectangle, drawn straight into the image's svg. It is not a mark and
  // is not in the document — a separate element with its own class, rebuilt on
  // every render from cropSel.
  function renderCropSelection(imageId, rec) {
    const existing = rec.svgEl.querySelector('.markup-crop');
    const sel = tool === 'crop' ? cropSel.get(imageId) : null;
    if (!sel) { if (existing) existing.remove(); return; }
    const el = existing || document.createElementNS(SVG_NS, 'rect');
    el.setAttribute('class', 'markup-crop');
    el.setAttribute('vector-effect', 'non-scaling-stroke');
    el.setAttribute('x', String(sel.x));
    el.setAttribute('y', String(sel.y));
    el.setAttribute('width', String(sel.w));
    el.setAttribute('height', String(sel.h));
    if (!existing) rec.svgEl.append(el);
  }

  // Choosing a tool changes almost nothing on screen: which button looks pressed,
  // the svg's cursor, whether the resize hint applies, whether the crop
  // rectangle is drawn and whether the copy buttons are live.
  //
  // It used to call render(), which re-appends every figure and every row to
  // keep DOM order in sync with the document. Moving a node is a remove and an
  // insert, so on a tab with several tall images the page briefly lost its
  // height and the browser clamped the scroll — picking a tool threw you back to
  // the top. Reported 2026-08-27.
  //
  // So this touches only what the tool actually decides. The full render still
  // happens on every real change, where re-ordering is the point.
  function setTool(next) {
    tool = next;
    regionBtn.setAttribute('aria-pressed', String(tool === 'region'));
    ellipseBtn.setAttribute('aria-pressed', String(tool === 'ellipse'));
    penBtn.setAttribute('aria-pressed', String(tool === 'pen'));
    moveBtn.setAttribute('aria-pressed', String(tool === 'move'));
    resizeBtn.setAttribute('aria-pressed', String(tool === 'resize'));
    cropBtn.setAttribute('aria-pressed', String(tool === 'crop'));
    for (const rec of imageRecords.values()) {
      if (!rec.svgEl) continue;
      rec.svgEl.dataset.tool = tool;
      renderCropSelection(rec.id, rec);
    }
    updateResizeHint(readState());
    updateCopyButtons();
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

  function toggleBulkColorPanel(imageId) {
    const rec = imageRecords.get(imageId);
    if (!rec) return;
    const already = bulkPanelOpen && bulkPanelEl.parentElement === rec.listEl;
    if (already) { closeBulkColorPanel(); return; }
    // Relocated, not rebuilt. Appending an existing node moves it, so the panel
    // always opens inside the slice whose header was pressed and there is never
    // a second one open somewhere off screen.
    rec.listEl.insertBefore(bulkPanelEl, rec.colHeadEl.nextSibling);
    bulkPanelOpen = true;
    bulkPanelEl.hidden = false;
    renderBulkColorScope(readState(), imageId);
  }

  function closeBulkColorPanel() {
    bulkPanelOpen = false;
    bulkPanelEl.hidden = true;
  }

  // Same per-image-or-all scope choice the old toolbar dropdown offered,
  // just anchored to the marks list's colour header instead.
  function renderBulkColorScope(state, imageId) {
    const annotatables = state.images.filter((im) => im.annotatable);
    // The slice it was opened from leads, because that is the one the human was
    // looking at when they pressed it.
    const prev = imageId || bulkScopeSelect.value;
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
      return undefined;
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
      if (!res.ok) { intakeSay(payload.error || 'upload refused', true); return undefined; }
    } catch {
      intakeSay('upload failed — is the server running?', true);
      return undefined;
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
    return payload.url;
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
    // Move on empty canvas pans a zoomed image. It is the only tool whose verb
    // already means "reposition", and the alternative — a sixth tool that does
    // nothing until you have zoomed — is a button that is dead most of the time.
    // At z=1 clampPan pins it, so the gesture is harmless rather than absent.
    // The middle button pans with ANY tool, so you can zoom in, draw, move
    // across and draw again without changing tool twice each time.
    if (tool === 'move' || evt.button === 1) {
      const rec = imageRecords.get(imageId);
      if (!rec || zoomOf(imageId).z <= 1) return;
      evt.preventDefault();
      svgEl.setPointerCapture(evt.pointerId);
      const view = zoomOf(imageId);
      drag = {
        type: 'pan', pointerId: evt.pointerId, imageId,
        startX: evt.clientX, startY: evt.clientY, tx0: view.tx, ty0: view.ty,
      };
      return;
    }
    if (tool === 'crop') {
      evt.preventDefault();
      svgEl.setPointerCapture(evt.pointerId);
      const [cx, cy] = pointerToNorm(svgEl, evt);
      drag = { type: 'crop', pointerId: evt.pointerId, imageId, x0: cx, y0: cy, x1: cx, y1: cy };
      updateDragPreview(svgEl);
      return;
    }
    if (tool !== 'region' && tool !== 'ellipse' && tool !== 'pen') return; // resize only ever acts on an existing mark
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
    if (drag.type === 'pan') {
      const rec = imageRecords.get(drag.imageId);
      if (!rec) return;
      const view = zoomOf(drag.imageId);
      view.tx = drag.tx0 + (evt.clientX - drag.startX);
      view.ty = drag.ty0 + (evt.clientY - drag.startY);
      zoomState.set(drag.imageId, view);
      applyZoom(rec);
      return;
    }
    const [x, y] = pointerToNorm(svgEl, evt);
    if (drag.type === 'crop') {
      drag.x1 = x;
      drag.y1 = y;
      updateDragPreview(svgEl);
      return;
    }
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
    // A pan changed nothing that is written down, and a crop is a SELECTION —
    // it is copied, never stored. Both stop here, before ensureImage(), which
    // would otherwise mark the document dirty for a gesture that did not touch
    // it.
    if (drag.type === 'pan') return;
    if (drag.type === 'crop') {
      const x = Math.min(drag.x0, drag.x1);
      const y = Math.min(drag.y0, drag.y1);
      const w = Math.abs(drag.x1 - drag.x0);
      const h = Math.abs(drag.y1 - drag.y0);
      if (w < MIN_REGION_SIZE || h < MIN_REGION_SIZE) cropSel.delete(drag.imageId);
      else cropSel.set(drag.imageId, { x, y, w, h });
      return;
    }
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
    if (drag.type === 'region' || drag.type === 'crop') {
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
    // The bulk panel's scope select is only meaningful while the panel is open,
    // and it is now attached to whichever slice opened it.
    if (bulkPanelOpen) renderBulkColorScope(state);
    renderList(state);
    // Per slice, because each colour header now belongs to one image.
    for (const im of state.images) {
      if (!im.annotatable) continue;
      const rec = imageRecords.get(im.id);
      const btn = rec && rec.colHeadEl ? rec.colHeadEl.querySelector('.markup-bulk-color-btn') : null;
      if (btn) btn.disabled = im.regions.length + im.strokes.length === 0;
    }
    updateResizeHint(state);
    updateCopyButtons();
    applyHighlightClasses();
  }

  // A copy button with nothing selected is a press that does nothing, so it says
  // so before it is pressed rather than after.
  function updateCopyButtons() {
    const has = tool === 'crop' && cropSel.size > 0;
    copyRegionBtn.disabled = !has;
    copySeenBtn.disabled = !has;
    cropAddBtn.disabled = !has;
    cropAddBtn.title = has
      ? 'Put the selected rectangle on this tab as a new image to draw on'
      : (tool === 'crop' ? 'Draw a rectangle with the crop tool first.' : 'Choose the crop tool and draw a rectangle first.');
    const why = tool === 'crop' ? 'Draw a rectangle with the crop tool first.' : 'Choose the crop tool and draw a rectangle first.';
    copyRegionBtn.title = has ? 'Copy just the pixels inside the rectangle' : why;
    copySeenBtn.title = has ? 'Copy the rectangle with its marks drawn on' : why;
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
    // TWO columns, whatever the image count. It used to be one column per image,
    // so six screenshots became six columns of nothing — and now that a slice
    // carries a marks table as well as a picture, a third column is unreadable
    // before it is narrow. "Side by side" means a pair; several pairs stack.
    imagesWrapEl.style.setProperty('--markup-cols', String(Math.min(2, state.images.length || 1)));

    const seen = new Set();
    // What the wrap's children should be, in order. Built first and reconciled
    // once, because a pair whose marks table spans both columns puts that table
    // BETWEEN two figures — so "the figure at index n" stopped being a thing the
    // loop could count on.
    const desired = [];
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
        renderShapesForImage(im, rec); // replaceChildren()s the svg...
        renderHandlesForImage(im, rec);
        renderCropSelection(im.id, rec); // ...so the crop rectangle is re-added after it
      }
      applyZoom(rec);
    }
    // Built pair by pair rather than image by image, because a spanning table
    // belongs after ITS OWN pair — appending them all at the end put every
    // table below every picture, which is the layout this change exists to fix.
    layOutRows(state, desired);
    // Only what is actually out of place moves. Re-inserting a node that is
    // already where it belongs still detaches and re-inserts it, and with tall
    // images that briefly shortens the document and makes the browser clamp the
    // scroll position — which is how choosing a tool used to jump to the top.
    for (let i = 0; i < desired.length; i += 1) {
      if (imagesWrapEl.children[i] !== desired[i]) {
        imagesWrapEl.insertBefore(desired[i], imagesWrapEl.children[i] || null);
      }
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

  // A pair with ONE markable image gives its marks table the width of both.
  //
  // Two markable images means two tables, and they belong under their own
  // pictures — that is the slice, and it is what stopped the scrolling between a
  // note and the thing it was about. But a before/after pair, where the left is
  // a reference and only the right is drawn on, has one table and a whole empty
  // column beside it: the note column was squeezed into half the tab for no
  // reason, which is what the human reported on 2026-08-28.
  //
  // It moves the table OUT of its figure and into the wrap as a full-width grid
  // item after the pair. The picture stays in its own column — only the table
  // widens. In stacked layout, or with both images markable, the table goes back
  // inside its figure, so this is reversible on every render rather than a state
  // the view can get stuck in.
  function layOutRows(state, desired) {
    const paired = state.layout === 'side-by-side';
    const ims = state.images;
    for (let i = 0; i < ims.length; i += paired ? 2 : 1) {
      const pair = paired ? ims.slice(i, i + 2) : ims.slice(i, i + 1);
      const markable = pair.filter((im) => im.annotatable);
      const span = paired && pair.length === 2 && markable.length === 1;
      const spanning = [];
      for (const im of pair) {
        const rec = imageRecords.get(im.id);
        if (!rec) continue;
        desired.push(rec.figureEl);
        rec.figureEl.classList.toggle('has-spanning-table', span);
        if (!rec.listEl) continue;
        rec.listEl.classList.toggle('markup-list-span', span);
        if (span) {
          spanning.push(rec.listEl);
        } else if (rec.listEl.parentElement !== rec.figureEl) {
          rec.figureEl.append(rec.listEl);
        }
      }
      // After the pair's own figures, and before the next pair's.
      desired.push(...spanning);
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

    // Zoom lives on the image's own head row rather than the tab toolbar,
    // because with a slice per image there is no such thing as "the" image any
    // more — each one is zoomed on its own.
    // Zoom is offered on EVERY image, markable or not. A read-only reference
    // pane — annotatable: false — is exactly the thing you want to magnify while
    // you draw on the one beside it, and gating zoom behind "can be marked"
    // would take it away from the case it is most useful in. Panning still needs
    // the svg, so a reference image zooms from the buttons and the wheel and
    // centres itself; that is enough for a reference.
    let zoomLabel = null;
    {
      const outBtn = ctl('zoom-out');
      outBtn.addEventListener('click', () => {
        const rec = imageRecords.get(imageId);
        if (rec) zoomAt(rec, 1 / 1.25, rec.stageEl.clientWidth / 2, rec.stageEl.clientHeight / 2);
      });
      headRow.append(outBtn);

      zoomLabel = document.createElement('span');
      zoomLabel.className = 'hint markup-zoom-label';
      zoomLabel.textContent = '100%';
      headRow.append(zoomLabel);

      const inBtn = ctl('zoom-in');
      inBtn.addEventListener('click', () => {
        const rec = imageRecords.get(imageId);
        if (rec) zoomAt(rec, 1.25, rec.stageEl.clientWidth / 2, rec.stageEl.clientHeight / 2);
      });
      headRow.append(inBtn);

      const fitBtn = ctl('zoom-fit');
      fitBtn.addEventListener('click', () => resetZoom(imageId));
      headRow.append(fitBtn);

      const copyViewBtn = ctl('copy-view');
      copyViewBtn.addEventListener('click', () => copyView(imageId));
      headRow.append(copyViewBtn);
    }



    const removeBtn = ctl('remove-image');
    removeBtn.classList.add('icon-btn--danger');
    removeBtn.addEventListener('click', () => deleteImage(imageId));
    headRow.append(removeBtn);

    const stageEl = document.createElement('div');
    stageEl.className = 'markup-stage';
    figureEl.append(stageEl);

    // Everything that scales lives in here. The stage is the window; this is the
    // thing that moves behind it. Marks are stored as fractions of the image and
    // are positioned inside this same wrapper, so one transform carries the
    // picture and every mark on it together and none of the drawing maths has to
    // know that zoom exists.
    const zoomEl = document.createElement('div');
    zoomEl.className = 'markup-zoom';
    stageEl.append(zoomEl);

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
      if (!rec) return;
      // The readout is a fraction of naturalWidth, which is 0 until the picture
      // has actually loaded — so the first honest value is only knowable here.
      applyZoom(rec);
      if (!rec.failed) return;
      rec.failed = false;
      render();
    });

    // And again whenever the picture's LAYOUT size changes, which happens with
    // no interaction at all: narrowing the window shrinks a wide screenshot to
    // fit, and the percentage on screen has to follow it down. It also re-clamps
    // the pan, since the stage the pan is bounded by has just changed size.
    if (typeof ResizeObserver === 'function') {
      new ResizeObserver(() => {
        const rec = imageRecords.get(imageId);
        if (rec) applyZoom(rec);
      }).observe(stageEl);
    }
    zoomEl.append(imgEl);

    const failEl = document.createElement('p');
    failEl.className = 'hint markup-fail';
    failEl.hidden = true;
    stageEl.append(failEl); // outside the zoom: a failure message is chrome, not content

    let svgEl = null;
    let labelsEl = null;
    let handlesEl = null;
    if (annotatable) {
      svgEl = document.createElementNS(SVG_NS, 'svg');
      svgEl.setAttribute('class', 'markup-svg');
      svgEl.setAttribute('viewBox', '0 0 1 1');
      svgEl.setAttribute('preserveAspectRatio', 'none');
      zoomEl.append(svgEl);

      labelsEl = document.createElement('div');
      labelsEl.className = 'markup-labels';
      zoomEl.append(labelsEl);

      // A plain, unscaled HTML layer for resize handles: the svg above uses
      // viewBox="0 0 1 1" with preserveAspectRatio="none", so any shape drawn
      // inside it stretches to the image's own aspect ratio. A fixed-px div
      // positioned by percentage — the same trick markup-labels already uses
      // for its circular numbers — stays a true, constant-size square instead.
      handlesEl = document.createElement('div');
      handlesEl.className = 'markup-handles';
      zoomEl.append(handlesEl);

      svgEl.addEventListener('pointerdown', (evt) => onPointerDown(imageId, svgEl, evt));
      svgEl.addEventListener('pointermove', (evt) => onPointerMove(svgEl, evt));
      svgEl.addEventListener('pointerup', (evt) => onPointerUp(svgEl, evt));
      svgEl.addEventListener('pointercancel', onPointerCancel);
    }

    // Wheel to zoom, outside the annotatable guard for the same reason the
    // buttons are: a read-only reference is a thing you magnify. On the STAGE
    // rather than the svg, so it works over the failure message and the padding
    // too — and a non-annotatable image has no svg at all. `passive: false`
    // because the whole point is to stop the page scrolling under it.
    stageEl.addEventListener('wheel', (evt) => {
      if (!evt.ctrlKey && !evt.metaKey) {
        // Plain wheel scrolls the page. Zoom is Ctrl/Cmd+wheel, the gesture
        // every image viewer and every editor already uses, so a wheel over a
        // tall tab still scrolls it.
        return;
      }
      evt.preventDefault();
      const rec = imageRecords.get(imageId);
      if (!rec) return;
      const box = stageEl.getBoundingClientRect();
      zoomAt(rec, evt.deltaY < 0 ? 1.15 : 1 / 1.15, evt.clientX - box.left, evt.clientY - box.top);
    }, { passive: false });

    // ---- this image's own marks table, inside its own slice ----
    //
    // One table per image, under the image it belongs to. It used to be ONE
    // table under ALL the images: with several screenshots on a tab you scrolled
    // to the bottom to read a note, then back up to see what it was about, and a
    // row gave no clue which picture it came from beyond a caption column.
    // Reported 2026-08-27. A slice is caption, buttons, image, marks — and that
    // is also why the caption column is gone from the table: every row in this
    // one belongs to this image by construction.
    let listEl = null;
    let colHeadEl = null;
    let listEmptyEl = null;
    if (annotatable) {
      listEl = document.createElement('div');
      listEl.className = 'markup-list';
      figureEl.append(listEl);

      colHeadEl = document.createElement('div');
      colHeadEl.className = 'markup-row markup-row-head';
      listEl.append(colHeadEl);

      const chipHeadCell = document.createElement('span');
      chipHeadCell.className = 'mono markup-chip';
      chipHeadCell.textContent = 'Id';
      colHeadEl.append(chipHeadCell);

      const summaryHeadCell = document.createElement('span');
      summaryHeadCell.className = 'markup-summary';
      summaryHeadCell.textContent = 'Mark';
      colHeadEl.append(summaryHeadCell);

      const colorHeadBtn = ctl('bulk-colour', {
        className: 'markup-bulk-color-btn',
        onClick: () => toggleBulkColorPanel(imageId),
      });
      colHeadEl.append(colorHeadBtn);

      const noteHeadCell = document.createElement('span');
      noteHeadCell.className = 'markup-note';
      noteHeadCell.textContent = 'Note';
      colHeadEl.append(noteHeadCell);

      const delHeadCell = document.createElement('span');
      delHeadCell.className = 'markup-row-head-spacer';
      colHeadEl.append(delHeadCell);

      listEmptyEl = document.createElement('p');
      listEmptyEl.className = 'hint markup-list-empty';
      listEmptyEl.textContent = 'No marks on this image yet — draw a region or a pen stroke above.';
      listEl.append(listEmptyEl);
    }

    return {
      id: imageId, figureEl, capEl, hideBtn, clearBtn,
      stageEl, zoomEl, imgEl, failEl, svgEl, labelsEl, handlesEl,
      listEl, colHeadEl, listEmptyEl, zoomLabel,
      annotatable, failed: false,
    };
  }

  function updateFigure(rec, im) {
    rec.capEl.textContent = im.caption || '(unnamed)';
    rec.capEl.hidden = false;
    // The id first, because that is what you say to an agent — "the image ab214"
    // — and the caption is a label the human is free to change. Same discipline
    // as a mark's badge: the thing on screen and the thing in a sentence are one
    // identifier. Reported 2026-08-28 as wanting the id on hover.
    rec.capEl.title = im.id + ' · click to rename';
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
      rec.labelsEl.append(buildLabel(im.id, r.x, r.y, r.id, colorToken(r)));
    }
    for (const s of im.strokes) {
      index += 1;
      const pts = decodePoints(s.points);
      rec.svgEl.append(buildStrokeShape(im.id, s));
      if (pts.length) rec.labelsEl.append(buildLabel(im.id, pts[0][0], pts[0][1], s.id, colorToken(s)));
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

  function buildLabel(imageId, x, y, id, token) {
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

  // One pass over every image, filling each slice's own table.
  //
  // There is no mark NUMBER any more, and its removal is the end of a long
  // argument rather than a tidy-up. A per-image counter made "mark 1" name two
  // different things the moment a second image arrived; a tab-wide counter fixed
  // that and introduced its own problem, which is that it renumbers — delete the
  // first mark and every id anyone had written down moves by one. The label on
  // the picture stopped using it first, then the table's own chip carried the id
  // beside it, and by 2026-08-28 the `#` column was a second identifier for the
  // same object, agreeing with nothing outside this view. An `ab` id is unique
  // board-wide, never reused, and is what you would paste into a sentence — so
  // it is the only one left.
  function renderList(state) {
    const present = new Set();
    for (const im of state.images) {
      if (!im.annotatable) continue;
      const rec = imageRecords.get(im.id);
      const entries = [];
      for (const r of im.regions) {
        entries.push({ imageId: im.id, id: r.id, type: r.shape === 'ellipse' ? 'ellipse' : 'region', obj: r });
      }
      for (const st of im.strokes) {
        entries.push({ imageId: im.id, id: st.id, type: 'pen', obj: st });
      }
      if (!rec || !rec.listEl) continue;
      // Placed relative to the element before it rather than by index: the
      // bulk-recolour panel moves into this list when it is open, so an index
      // into children is right only some of the time.
      let prev = rec.colHeadEl;
      if (bulkPanelEl.parentElement === rec.listEl) prev = bulkPanelEl;
      for (const entry of entries) {
        const key = markKey(entry.imageId, entry.id);
        present.add(key);
        let row = rowEls.get(key);
        if (!row) {
          row = buildRow(entry.imageId, entry.id);
          rowEls.set(key, row);
        }
        updateRow(row, entry);
        // Same rule as the figures above: move it only if it is in the wrong
        // place. Moving a node that is already correct still detaches it, and
        // that is what made the page lose its height and jump to the top.
        if (row.previousElementSibling !== prev || row.parentElement !== rec.listEl) prev.after(row);
        prev = row;
      }
      rec.listEmptyEl.hidden = entries.length !== 0;
      // The empty line goes last so it sits under the header rather than under
      // rows that arrived after it.
      rec.listEl.append(rec.listEmptyEl);
    }
    for (const [key, row] of rowEls) {
      if (!present.has(key)) {
        row.remove();
        rowEls.delete(key);
      }
    }
  }

  function buildRow(imageId, id) {
    const key = markKey(imageId, id);
    const row = document.createElement('div');
    row.className = 'markup-row';
    row.dataset.markKey = key;

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

  function updateRow(row, entry) {
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

  /* ---------- copy to the clipboard ---------- */

  // Everything here draws onto a canvas BY HAND from the state, rather than
  // serialising the live <svg> and rasterising it. Two reasons, and both are
  // load-bearing:
  //
  //   1. The marks are painted with `var(--mark)` and friends. An <svg>
  //      serialised out of the document takes none of the page's CSS with it, so
  //      every stroke would come back black — and `colorVar()` already resolves a
  //      token to a real colour for exactly this kind of question.
  //   2. The id badges are HTML, not SVG. Getting them into a rasterised SVG
  //      means <foreignObject>, which several browsers refuse to draw to a canvas
  //      at all or taint the canvas for. Drawing a rounded box and some text is
  //      three lines and cannot be refused.
  //
  // The image itself is same-origin — `api(im.src)` under the board's own
  // uploads/ or assets/ — so the canvas is NOT tainted and toBlob() works. That
  // is the fact the whole feature rests on; a cross-origin image would fail here
  // with a SecurityError and no amount of care would help.
  function copyStatus(text, bad) {
    copyStatusEl.textContent = text;
    copyStatusEl.classList.toggle('is-bad', !!bad);
    if (copyStatusEl.timer) clearTimeout(copyStatusEl.timer);
    copyStatusEl.timer = setTimeout(() => { copyStatusEl.textContent = ''; copyStatusEl.classList.remove('is-bad'); }, 4000);
  }

  function drawMarksOnto(cx, im, sx, sy, sw, sh, scale) {
    const nudge = (v) => Math.round(v);
    let n = 0;
    for (const state of [im.regions, im.strokes]) void state;
    const all = [];
    for (const r of im.regions) all.push({ obj: r, kind: r.shape === 'ellipse' ? 'ellipse' : 'rect' });
    for (const st of im.strokes) all.push({ obj: st, kind: 'pen' });
    for (const entry of all) {
      n += 1;
      const stroke = resolvedColor(colorToken(entry.obj), '#ff0066');
      cx.strokeStyle = stroke;
      cx.lineWidth = 2;
      cx.lineJoin = 'round';
      cx.lineCap = 'round';
      let bx = 0;
      let by = 0;
      if (entry.kind === 'pen') {
        const pts = decodePoints(entry.obj.points);
        if (!pts.length) continue;
        cx.beginPath();
        pts.forEach((pt, i) => {
          const px = nudge((pt[0] * im.naturalW - sx) * scale);
          const py = nudge((pt[1] * im.naturalH - sy) * scale);
          if (i === 0) cx.moveTo(px, py); else cx.lineTo(px, py);
        });
        cx.stroke();
        bx = (pts[0][0] * im.naturalW - sx) * scale;
        by = (pts[0][1] * im.naturalH - sy) * scale;
      } else {
        const x = (entry.obj.x * im.naturalW - sx) * scale;
        const y = (entry.obj.y * im.naturalH - sy) * scale;
        const w = entry.obj.w * im.naturalW * scale;
        const h = entry.obj.h * im.naturalH * scale;
        cx.beginPath();
        if (entry.kind === 'ellipse') cx.ellipse(x + w / 2, y + h / 2, Math.abs(w / 2), Math.abs(h / 2), 0, 0, Math.PI * 2);
        else cx.rect(x, y, w, h);
        cx.stroke();
        bx = x;
        by = y;
      }
      // The id badge, drawn the way the overlay draws it: the id, because that
      // is the string a human types back to an agent.
      const text = entry.obj.id;
      cx.font = '11px ui-monospace, SFMono-Regular, Menlo, monospace';
      const tw = cx.measureText(text).width;
      cx.fillStyle = stroke;
      cx.fillRect(bx, by - 14, tw + 8, 14);
      cx.fillStyle = resolvedColor('accent-ink', '#111');
      cx.fillText(text, bx + 4, by - 3.5);
    }
    return n;
  }

  // A rectangle of an image, in image fractions, as a PNG on the clipboard.
  // `outWidth` is the width of the PICTURE PRODUCED, in pixels. Left undefined it
  // is the source region at its own resolution — right for a crop, which is a
  // closeup of the original and wants every pixel the original has.
  //
  // Copy view passes the stage's size instead, and that difference is the whole
  // of a bug reported on 2026-08-27: at 244% on a small pasted image, "copy the
  // view" produced 55x60. It was arithmetically correct — a region of the SOURCE
  // is smaller the further you zoom in — and exactly backwards from what the
  // button says. Zooming in to look at something and then copying it must not
  // hand back something smaller than what you were looking at.
  // The picture, drawn. Split out of copyRect because the clipboard is only ONE
  // thing to do with a crop — putting it on the tab as a new image is the other,
  // and it is the one that works in a VS Code panel.
  function drawCrop(imageId, rect, withMarks, outWidth) {
    const rec = imageRecords.get(imageId);
    const im = readState().images.find((x) => x.id === imageId);
    if (!rec || !im || !rec.imgEl.naturalWidth) { copyStatus('nothing to copy', true); return null; }
    const nw = rec.imgEl.naturalWidth;
    const nh = rec.imgEl.naturalHeight;
    const sx = Math.max(0, rect.x * nw);
    const sy = Math.max(0, rect.y * nh);
    const sw = Math.min(nw - sx, rect.w * nw);
    const sh = Math.min(nh - sy, rect.h * nh);
    if (sw < 1 || sh < 1) { copyStatus('that selection is empty', true); return null; }

    const scale = outWidth && sw > 0 ? outWidth / sw : 1;
    const canvas = document.createElement('canvas');
    canvas.width = Math.max(1, Math.round(sw * scale));
    canvas.height = Math.max(1, Math.round(sh * scale));
    const cx = canvas.getContext('2d');
    try {
      cx.drawImage(rec.imgEl, sx, sy, sw, sh, 0, 0, canvas.width, canvas.height);
    } catch {
      copyStatus('the image could not be read into a canvas', true);
      return null;
    }
    if (withMarks) drawMarksOnto(cx, { ...im, naturalW: nw, naturalH: nh }, sx, sy, sw, sh, scale);
    return canvas;
  }

  function canvasBlob(canvas) {
    return new Promise((resolve, reject) => {
      canvas.toBlob((b) => (b ? resolve(b) : reject(new Error('toBlob returned nothing'))), 'image/png');
    });
  }

  async function copyRect(imageId, rect, withMarks, what, outWidth) {
    const canvas = drawCrop(imageId, rect, withMarks, outWidth);
    if (!canvas) return;

    let blob;
    try {
      blob = await canvasBlob(canvas);
    } catch {
      copyStatus('this image cannot be copied (the canvas is tainted)', true);
      return;
    }
    // Loudly, not silently. In a VS Code panel the board is a cross-origin frame
    // and clipboard-write has to be delegated to it by permissions policy; if
    // that has not happened this rejects, and a copy button that quietly does
    // nothing is the exact failure this project keeps being bitten by.
    try {
      // Raced against a timeout, because the failure this guards is SILENCE.
      // navigator.clipboard.write does not always reject when it cannot proceed:
      // on a document that does not have focus Chromium leaves the promise
      // pending indefinitely, so a press produced no picture, no error and no
      // message — the exact shape of defect this board keeps finding. Measured:
      // it hung about one run in six in the browser suite before this.
      //
      // Three seconds is long for a local clipboard write and short enough that
      // nobody wonders whether they pressed it.
      await Promise.race([
        navigator.clipboard.write([new ClipboardItem({ 'image/png': blob })]),
        new Promise((_, reject) => setTimeout(() => reject(new Error('the clipboard did not answer — is this window focused?')), 3000)),
      ]);
      copyStatus(`${what} copied — ${canvas.width}×${canvas.height}, paste it with Ctrl+V`);
      // Gone the moment it has done its job. A dashed box that outlives the copy
      // sits on the picture looking like a mark that cannot be selected, noted
      // or deleted — which is what it was reported as. Copying twice from one
      // rectangle is rarer than wondering what the leftover box is.
      cropSel.delete(imageId);
      render();
    } catch (err) {
      // Before giving up: ask the HOST, if there is one that answers.
      //
      // A VS Code panel can put an image on the clipboard even though the page
      // in it cannot — the extension host is a Node process and can run xclip.
      // The board does not know or care that it is VS Code: it asks whoever
      // framed it, and a host that does not implement this simply never
      // answers, which the timeout turns into the same refusal as before.
      const viaHost = await askHostToCopy(blob);
      if (viaHost && viaHost.ok) {
        copyStatus(`${what} copied — ${canvas.width}×${canvas.height}, paste it with Ctrl+V`);
        cropSel.delete(imageId);
        render();
        return;
      }

      // The clipboard is not always reachable, and in the place this board most
      // wants to be — a VS Code panel — it usually is not. The webview applies a
      // permissions policy the board is inside of, so `clipboard-write` has to
      // be delegated to it by the HOST; when it is not, Chromium refuses with
      // "The Clipboard API has been blocked because of a permissions policy
      // applied to the current document". Nothing this page can do fixes that
      // from the inside.
      //
      // So there is a second way out, and it works everywhere because it asks
      // for nothing: put the picture on screen and let the human use their own
      // browser's context menu on it. Right-click, Copy Image — the same gesture
      // they would use on any image on any page, available in a VS Code webview
      // too. A refusal becomes one extra click rather than a dead end.
      // Three different failures, three different sentences. They used to share
      // one, so "the host never answered" and "xclip is missing" and "nothing is
      // framing this page" were indistinguishable to the person reading it.
      const why = (viaHost && viaHost.error) ? viaHost.error
        : window.parent === window
          ? (err && err.message ? String(err.message) : 'the clipboard is not permitted here')
          : hostClipboardGap();
      copyStatus(why, true);
      // The host's reason if it gave one — "xclip is not installed", which the
      // human can act on — and the browser's only if nobody answered.
      offerImage(canvas, what, { message: why }, imageId);
    }
  }

  // Ask whoever framed this board to put a PNG on the system clipboard.
  //
  // Unframed, or framed by something that does not implement it, this resolves
  // to null and the caller falls through to the picture. There is deliberately
  // no check for "is this VS Code": the board cannot tell, should not care, and
  // a host that answers has proved more than any sniff could.
  let clipboardAsk = 0;
  const clipboardWaiting = new Map();
  // The shell authenticates the host message (e.source === window.parent) and
  // re-emits it here, so this listener sees only messages that already passed
  // that check and nothing in this file has to repeat it.
  document.addEventListener('aboard:clipboard-result', (evt) => {
    const msg = evt.detail || {};
    const resolve = clipboardWaiting.get(msg.id);
    if (!resolve) return;
    clipboardWaiting.delete(msg.id);
    resolve({ ok: !!msg.ok, error: msg.error, tool: msg.tool });
  });

  // Why a copy could not go through the host, in words that name the hop rather
  // than the symptom. Called only AFTER an attempt, so every branch describes
  // something that has already been tried.
  function hostClipboardGap() {
    if (window.parent === window) return 'nothing is framing this board';
    const host = window.ABOARD_HOST;
    if (host && !host.clipboard) {
      return `the ${host.name} window framing this board cannot write images to the clipboard`;
    }
    if (!host) {
      return 'the window framing this board never said what it can do, and did not answer when asked — '
        + 'if this is a VS Code panel, the extension is older than this board (reinstall the .vsix) '
        + 'or the panel needs reloading';
    }
    return `the ${host.name} window framing this board did not answer within six seconds`;
  }

  // An announcement makes the FAILURE legible; it is not permission to try.
  //
  // This gate used to require one, which turned a diagnostic into a regression:
  // a panel one build older announces nothing and yet can copy perfectly well,
  // so the board refused to ask a host that would have said yes, and then
  // reported the silence it had caused itself. Found on 2026-08-28, the same
  // morning the announcement was added, and by the person it was added for.
  //
  // So: ask unless there is nobody to ask, or the host has said in so many words
  // that it cannot. Silence is answered by the timeout, as it was before.
  function askHostToCopy(blob) {
    if (window.parent === window) return Promise.resolve(null);
    const host = window.ABOARD_HOST;
    if (host && !host.clipboard) return Promise.resolve(null);
    return new Promise((resolve) => {
      const id = (clipboardAsk += 1);
      const reader = new FileReader();
      reader.onerror = () => resolve(null);
      reader.onload = () => {
        clipboardWaiting.set(id, resolve);
        try {
          parent.postMessage({ __aboard: 'clipboard-image', id, dataUrl: String(reader.result) }, '*');
        } catch {
          clipboardWaiting.delete(id);
          resolve(null);
          return;
        }
        // A host that never answers must not leave the human looking at
        // "copying…" forever. Six seconds is longer than the host's own timeout
        // on the tool it runs, so a slow xclip reports itself rather than being
        // overtaken by this.
        setTimeout(() => {
          if (!clipboardWaiting.has(id)) return;
          clipboardWaiting.delete(id);
          resolve(null);
        }, 6000);
      };
      reader.readAsDataURL(blob);
    });
  }

  // The fallback: the cropped picture in the board's own dialog, at a size worth
  // right-clicking. Deliberately NOT a download — a download from a sandboxed
  // frame is the next thing a host can refuse, and this asks permission for
  // nothing at all.
  // What the dialog is currently holding, so the button under it can add THAT.
  //
  // It used to re-derive a clean crop from the selection, which was right for
  // "Copy region" by accident and wrong for everything else: "Copy as seen"
  // showed a picture WITH marks and then added one without them, and "Copy view"
  // had no selection at all so the button was hidden. Reported 2026-08-28.
  let offered = null;

  function offerImage(canvas, what, err, imageId) {
    offered = { canvas, what, imageId };
    // The title no longer says "right-click", and that is the point of this
    // change. Right-click IS the answer in a browser; in a VS Code webview the
    // host owns the context menu and offers no "Copy image", so the instruction
    // was one more thing that appeared to do nothing — reported 2026-08-28, and
    // exactly the failure this whole dialog exists to avoid.
    //
    // So the heading states the situation, the ACTION that works everywhere is a
    // button, and right-click is mentioned as the browser option it is.
    imageDialogTitle.textContent = 'The clipboard is blocked in this window';
    // Always offered now. There is a picture on the dialog either way, and "put
    // this on the tab" is a sensible thing to do with any of them.
    imageDialogAdd.hidden = false;
    imageDialogWhy.textContent = (err && err.message ? err.message : 'the clipboard is not permitted here')
      + ' — this is the same ' + what + ', ' + canvas.width + '×' + canvas.height + '.';
    imageDialogImg.src = canvas.toDataURL('image/png');
    imageDialogImg.alt = 'The ' + what + ' you asked to copy';
    if (!imageDialogEl.open) imageDialogEl.showModal();
  }

  // The crop tool's selection. `withMarks` is the difference between the two
  // buttons: a clean closeup to annotate afresh, or the region as it looks now
  // to paste somewhere outside the board.
  // The crop, straight onto the tab as a new image.
  //
  // This is what the clipboard was ever FOR here — the ask was "copy a rectangle
  // so it could then be pasted as a new image, like a closeup" — and it is the
  // route that works in a VS Code panel, where the clipboard is blocked by a
  // permissions policy the board sits inside of and cannot change. It reuses the
  // paste/drop path exactly: POST /upload, then a new entry in `images`. Same
  // code, so a closeup added this way is indistinguishable from one pasted in.
  //
  // CLEAN pixels, deliberately. A closeup exists to be drawn on, and the marks
  // from the picture it came out of would be baked in as pixels that cannot be
  // selected, recoloured or deleted — sitting under the new marks you are about
  // to make.
  async function cropToImage() {
    const entries = [...cropSel.entries()];
    if (!entries.length) { copyStatus('draw a rectangle with the crop tool first', true); return; }
    const [imageId, rect] = entries[entries.length - 1];
    copyStatus('adding…');
    const canvas = drawCrop(imageId, rect, false);
    if (!canvas) return;
    let blob;
    try {
      blob = await canvasBlob(canvas);
    } catch {
      copyStatus('this image cannot be read (the canvas is tainted)', true);
      return;
    }
    const source = readState().images.find((x) => x.id === imageId);
    const base = (source && source.caption ? source.caption.replace(/\.[a-z0-9]+$/i, '') : 'image');
    const file = new File([blob], base + '-closeup.png', { type: 'image/png' });
    cropSel.delete(imageId);
    copyStatus(`adding as a new image — ${canvas.width}×${canvas.height}`);
    const url = await addImageFile(file);
    // The path as well as the picture. An upload is a real file under
    // `.aboard/uploads/`, so saying where it went is what makes the closeup
    // reachable from outside the board at all — which is the job the clipboard
    // was doing before a permissions policy took it away.
    copyStatus(url
      ? `added as a new image — ${canvas.width}×${canvas.height}, saved at ${url}`
      : `added as a new image — ${canvas.width}×${canvas.height}`);
    if (imageDialogEl.open) imageDialogEl.close();
  }

  // Add the picture the dialog is showing, exactly as it is showing it.
  //
  // Not a re-render from the selection: the human has just been shown a picture
  // and told they can put it on the tab, and adding a DIFFERENT one — the same
  // region without its marks — is the kind of quiet mismatch nobody thinks to
  // check. The toolbar's own "Add as image" still makes a clean closeup on
  // purpose; that button is about the crop, this one is about what is on screen.
  async function addOffered() {
    if (!offered) return;
    const { canvas, what, imageId } = offered;
    copyStatus('adding…');
    let blob;
    try {
      blob = await canvasBlob(canvas);
    } catch {
      copyStatus('that picture cannot be read back', true);
      return;
    }
    const source = readState().images.find((x) => x.id === imageId);
    const base = source && source.caption ? source.caption.replace(/\.[a-z0-9]+$/i, '') : 'image';
    const tag = what.replace(/[^a-z0-9]+/gi, '-').replace(/^-|-$/g, '') || 'copy';
    const file = new File([blob], `${base}-${tag}.png`, { type: 'image/png' });
    if (imageId) cropSel.delete(imageId);
    imageDialogEl.close();
    const url = await addImageFile(file);
    copyStatus(url
      ? `added as a new image — ${canvas.width}×${canvas.height}, saved at ${url}`
      : `added as a new image — ${canvas.width}×${canvas.height}`);
  }

  function copyCrop(withMarks) {
    // Says something SYNCHRONOUSLY, before any of the async work. Everything
    // below this line is a promise — toBlob, then the clipboard — and a press
    // whose only feedback arrives after them is a press that looks ignored while
    // they run, and looks ignored forever if one of them never settles.
    copyStatus('copying…');
    const entries = [...cropSel.entries()];
    if (!entries.length) { copyStatus('draw a rectangle with the crop tool first', true); return; }
    const [imageId, rect] = entries[entries.length - 1];
    void copyRect(imageId, rect, withMarks, withMarks ? 'region with marks' : 'region');
  }

  // What the stage is showing right now, marks included — the "capture the
  // zoomed view" half. Derived from the transform rather than from a selection,
  // so at 100% it is simply the whole image.
  function copyView(imageId) {
    copyStatus('copying…');
    const rec = imageRecords.get(imageId);
    if (!rec) return;
    const view = zoomOf(imageId);
    const w = rec.stageEl.clientWidth;
    const h = rec.stageEl.clientHeight;
    const rect = {
      x: (-view.tx / view.z) / w,
      y: (-view.ty / view.z) / (h || 1),
      w: 1 / view.z,
      h: 1 / view.z,
    };
    // The stage is as wide as the image and as tall as the image's own aspect
    // ratio makes it, so the visible fraction is the same on both axes.
    //
    // Rendered at what is ON SCREEN, times the device pixel ratio so it is not
    // softer than the screen it was copied from. Capped so a very deep zoom on a
    // hi-dpi display cannot ask for a canvas of tens of megapixels.
    // The LARGER of what is on screen and what the source region actually holds.
    //
    // Screen size alone would lose resolution the other way: a big screenshot
    // shrunk to fit the row, copied at rest, would come back at the shrunken
    // size when the original pixels were right there. Source size alone is the
    // bug being fixed. Neither direction should cost anything, so neither does.
    const dpr = Math.min(3, Math.max(1, window.devicePixelRatio || 1));
    const onScreen = Math.round(w * dpr);
    const sourceW = Math.round((rec.imgEl.naturalWidth || w) * rect.w);
    const out = Math.min(4096, Math.max(onScreen, sourceW));
    void copyRect(imageId, { x: clamp01(rect.x), y: clamp01(rect.y), w: rect.w, h: rect.h }, true, 'view', out);
  }

  /* ---------- colour token resolution ---------- */

  function colorToken(mark) {
    const c = mark && mark.color;
    return COLOR_TOKENS.indexOf(c) !== -1 ? c : DEFAULT_COLOR;
  }

  function colorVar(token) {
    return 'var(--' + token + ')';
  }

  // The same token as an ACTUAL colour, for the canvas — which cannot read a
  // `var()`. Resolved off the live panel, so it is whatever the viewer's theme
  // (or an embedder's palette) has made it at this moment, rather than a copy of
  // the stylesheet kept here to go stale.
  function resolvedColor(token, fallback) {
    const v = getComputedStyle(panel).getPropertyValue('--' + token).trim();
    return v || fallback;
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
/* The paste bar and the two tool rows, kept on screen. --head-h is the shell's
   own sticky head, measured in aboard.html; the fallback is 0 so a renderer used
   outside that shell still behaves. A background is required: a sticky element
   with none lets the content scroll through it. */
[data-view="markup"] .markup-sticky {
  position: sticky;
  top: var(--head-h, 0px);
  z-index: 5;
  background: var(--bg);
  padding-bottom: 8px;
  margin-bottom: 4px;
  border-bottom: 1px solid var(--line);
}

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
/* A SLICE: caption and buttons, the picture, then that picture's own marks. The
   three used to be spread across the tab -- every image stacked, then one table
   for all of them at the bottom -- so reading a note meant scrolling past every
   other screenshot to reach it and back up again to see what it was about. */
[data-view="markup"] .markup-figure {
  min-width: 0;
  padding-bottom: 14px;
  border-bottom: 1px solid var(--line);
}
[data-view="markup"] .markup-figure:last-child { border-bottom: 0; padding-bottom: 0; }
/* A pair whose single marks table spans both columns is ONE unit: the rule
   belongs under the table, not between each picture and it. */
[data-view="markup"] .markup-figure.has-spanning-table { border-bottom: 0; padding-bottom: 0; }
[data-view="markup"] .markup-list.markup-list-span {
  grid-column: 1 / -1;
  padding-bottom: 14px;
  border-bottom: 1px solid var(--line);
}
[data-view="markup"] .markup-list.markup-list-span:last-child { border-bottom: 0; padding-bottom: 0; }

/* The thing that moves behind the stage's window. transform-origin at the top
   left keeps the arithmetic in zoomAt honest: a centred origin makes every pan
   offset depend on the element's size. */
[data-view="markup"] .markup-zoom {
  transform-origin: 0 0;
  will-change: transform;
}
[data-view="markup"] .markup-zoom-label {
  min-width: 4ch;
  text-align: center;
  font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
  font-size: 0.72rem;
}
[data-view="markup"] .markup-stage[data-zoomed="yes"] .markup-svg[data-tool="move"] { cursor: grab; }
[data-view="markup"] .markup-stage[data-zoomed="yes"] .markup-svg[data-tool="move"]:active { cursor: grabbing; }
[data-view="markup"] .markup-svg[data-tool="crop"] { cursor: crosshair; }

/* The crop selection. Deliberately unlike a mark: dashed, no fill, no badge --
   it is a thing about to be copied, not a thing that was recorded. */
[data-view="markup"] .markup-crop {
  fill: none;
  stroke: var(--focus);
  stroke-width: 2;
  stroke-dasharray: 6 4;
}
[data-view="markup"] .markup-copy-status { margin: 0 0 0 4px; }
[data-view="markup"] .markup-offer-img {
  display: block;
  max-width: min(70vw, 900px);
  max-height: 60vh;
  width: auto;
  height: auto;
  margin: 10px 0 14px;
  border: 1px solid var(--line);
  border-radius: 3px;
  background: var(--sunken);
}
[data-view="markup"] .markup-copy-status.is-bad { color: var(--danger); }
/* WRAPS. A figure is one column of a pair, and its head carries a caption plus
   up to eight controls -- rename, hide, clear, three zoom controls, fit, copy.
   A flex row that cannot wrap does not shrink either: its items keep their
   content width, overflow the column and PAINT OVER the figure beside them, so
   the right image's caption landed on top of the left image's Copy view button.
   Reported 2026-08-28 from a narrow panel. Wrapping is the whole fix; the
   caption ellipsising is what stops a long file name from forcing it. */
[data-view="markup"] .markup-figure-head {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 8px;
  margin-bottom: 6px;
  min-width: 0;
}
[data-view="markup"] .markup-figure-caption {
  margin: 0;
  min-width: 0;
  flex: 0 1 auto;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
[data-view="markup"] .markup-stage {
  position: relative;
  touch-action: none;
  /* fit-content, not 100%: the stage is the frame around the picture, so it is
     as wide as the picture is allowed to be and no wider. With a stage at 100%
     and an image stretched to fill it, a 200px screenshot was blown up to 1300px
     and came out a blur -- reported 2026-08-27 as "a small image is shown huge".
     A LARGE image still uses the whole row, because max-width caps it there. */
  width: fit-content;
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
/* auto width so the natural size is one ceiling, max-width so the row is the
   other. The aspect ratio was never the problem -- auto height already kept it.
   Upscaling was. */
[data-view="markup"] .markup-img { display: block; width: auto; max-width: 100%; height: auto; }
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
  /* A chip, not a bubble: it holds an id like "ab168" now, not one digit. */
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
    minmax(5ch, max-content)      /* ab id */
    minmax(9ch, 1fr)              /* what and where */
    max-content                   /* colour swatches */
    minmax(10ch, 2fr)             /* note */
    24px;                         /* delete */
  column-gap: 8px;
  row-gap: 6px;
  margin-top: 10px;
}
/* Five columns, and each one that went says something. The image-name column
   went when every table became its own image's, so a caption repeated down the
   column was noise with a truncation problem attached. The mark-number column
   went on 2026-08-28: the id beside it already names the mark, is unique across
   the board and never moves, where a number renumbered every time a mark above
   it was deleted. */
[data-view="markup"] .markup-list > .markup-list-empty { grid-column: 1 / -1; margin: 2px 0 0; }
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
