// Mermaid diagram view: renders this tab's state.source and lets the human edit it.

import { api } from './api.js';

const VENDOR_SRC = api('/lib/mermaid.min.js');

let styleInjected = false;
let mermaidLoadPromise = null;

function ensureStyle() {
  if (styleInjected) return;
  styleInjected = true;
  const style = document.createElement('style');
  style.textContent = `
    [data-view="diagram"] .diagram-render {
      margin: 4px 0 12px;
      padding: 12px;
      border: 1px solid var(--line);
      border-radius: 4px;
      background: var(--sunken);
      overflow-x: auto;
      min-height: 80px;
    }
    [data-view="diagram"] .diagram-render svg { display: block; max-width: none; }
    [data-view="diagram"] .diagram-render pre { margin: 0; white-space: pre-wrap; }

    /* Mermaid's themeVariables cover fills and strokes but not weight or
       tracking, so the last mile is done here — the same restraint the Kanban
       cards use: hairline borders, one weight of emphasis, no drop shadows. */
    [data-view="diagram"] .diagram-render .node rect,
    [data-view="diagram"] .diagram-render .node polygon,
    [data-view="diagram"] .diagram-render .node circle,
    [data-view="diagram"] .diagram-render .node path {
      stroke-width: 1px;
    }
    /* No font-weight override on node labels: mermaid sizes each box by
       measuring its text first, so making the text heavier afterwards overflows
       the box it already computed and the label gets clipped. */
    [data-view="diagram"] .diagram-render .edgeLabel {
      font-size: 0.82em;
      color: var(--muted);
    }
    /* Mermaid paints an opaque box behind every edge label; let the ground show. */
    [data-view="diagram"] .diagram-render .edgeLabel rect,
    [data-view="diagram"] .diagram-render .edgeLabel .labelBkg {
      fill: var(--sunken);
      opacity: 1;
    }
    [data-view="diagram"] .diagram-render .edgePath path { stroke-width: 1.4px; }
    /* Arrowheads are not reachable through themeVariables — mermaid hardcodes an
       off-white that all but disappears on a light ground. Pin them to the edge
       colour so the heads always match the lines they terminate. */
    [data-view="diagram"] .diagram-render .arrowheadPath,
    [data-view="diagram"] .diagram-render marker path {
      fill: var(--edge);
      stroke: none;
    }
    [data-view="diagram"] .diagram-render .cluster rect { stroke-width: 1px; }
    [data-view="diagram"] .diagram-render .flowchart-link { stroke-linecap: round; }
    [data-view="diagram"] .diagram-error {
      margin: 0 0 12px;
      padding: 10px 12px;
      border: 1px solid var(--mark);
      border-left-width: 3px;
      border-radius: 3px;
      background: var(--surface);
      color: var(--mark);
      font-size: 0.84rem;
      white-space: pre-wrap;
      word-break: break-word;
    }
    [data-view="diagram"] .diagram-editor {
      display: flex;
      flex-direction: column;
      gap: 6px;
    }
    [data-view="diagram"] .diagram-editor[hidden] { display: none; }
    [data-view="diagram"] .diagram-editor textarea {
      width: 100%;
      min-height: 220px;
      font-size: 0.85rem;
    }
  `;
  document.head.append(style);
}

// The vendored bundle is a classic script that sets globalThis.mermaid.
// Loaded once per page; concurrent/second mount calls share this promise.
function loadMermaid() {
  if (globalThis.mermaid) return Promise.resolve(globalThis.mermaid);
  if (mermaidLoadPromise) return mermaidLoadPromise;
  mermaidLoadPromise = new Promise((resolve, reject) => {
    const script = document.createElement('script');
    script.src = VENDOR_SRC;
    script.addEventListener('load', () => {
      if (globalThis.mermaid) resolve(globalThis.mermaid);
      else reject(new Error('mermaid script loaded but did not set globalThis.mermaid'));
    });
    script.addEventListener('error', () => reject(new Error(`failed to load ${VENDOR_SRC}`)));
    document.head.append(script);
  });
  return mermaidLoadPromise;
}

export function mountDiagram(root, ctx) {
  ensureStyle();

  root.innerHTML = `
    <div class="panel">
      <p class="panel-head">Diagram</p>
      <div class="toolbar">
        <button type="button" class="primary-btn" data-action="rerender" title="Re-render the diagram from the current source">Re-render</button>
        <button type="button" class="icon-btn" data-action="copy" title="Copy the diagram source to the clipboard">Copy source</button>
        <button type="button" class="icon-btn" data-action="palette" title="Append classDef colour classes taken from the board palette">Add colours</button>
        <button type="button" class="icon-btn" data-action="toggle" aria-pressed="true" title="Show or hide the source editor">Hide source</button>
        <span class="toolbar-label" data-role="status"></span>
      </div>
      <div class="diagram-render" data-role="render"></div>
      <div class="diagram-error" data-role="error" hidden></div>
      <div class="diagram-editor" data-role="editor">
        <textarea class="mono" data-role="source" rows="12" spellcheck="false" aria-label="Mermaid diagram source"></textarea>
        <p class="hint">Edit the Mermaid source above. Changes save automatically and re-render after a short pause.</p>
      </div>
    </div>
  `;

  const $render = root.querySelector('[data-role="render"]');
  const $error = root.querySelector('[data-role="error"]');
  const $source = root.querySelector('[data-role="source"]');
  const $status = root.querySelector('[data-role="status"]');
  const $editor = root.querySelector('[data-role="editor"]');
  const $toggleBtn = root.querySelector('[data-action="toggle"]');
  const $rerenderBtn = root.querySelector('[data-action="rerender"]');
  const $copyBtn = root.querySelector('[data-action="copy"]');
  const $paletteBtn = root.querySelector('[data-action="palette"]');

  let renderTimer = null;
  let renderSeq = 0;
  let activeToken = 0;
  let lastRenderedSource = null;

  // ctx.state is THIS TAB's state: { source }.
  function getDiagram() {
    if (typeof ctx.state.source !== 'string') ctx.state.source = '';
    return ctx.state;
  }

  // Drive mermaid off the board's own CSS tokens instead of one of its stock
  // themes, so a diagram reads as part of this tool rather than an embed.
  // Mermaid emits nodes as <g class="node" id="flowchart-P-3">. The label is
  // already on screen; the mermaid node id is not, and that is what you need in
  // order to edit the source. Add a native tooltip carrying both.
  function annotateNodes() {
    const svg = $render.querySelector('svg');
    if (!svg) return;
    for (const g of svg.querySelectorAll('g.node, g.nodes > g, g.statediagram-state, g.classGroup')) {
      const raw = g.getAttribute('id') || '';
      // Mermaid ids look like "diagram-render-0-flowchart-P-3": our own render
      // id, then a diagram-kind marker, then the author's node key, then a
      // counter. Take what follows the marker, minus the counter.
      // Work on segments, not a regex: an unanchored alternation matched the
      // "er" inside our own "render-0-" prefix and captured the rest.
      const parts = raw.replace(/-\d+$/, '').split('-');
      const KINDS = ['flowchart', 'graph', 'statediagram', 'state', 'classid', 'class',
        'entity', 'er', 'node', 'mindmap', 'block', 'timeline'];
      let cut = -1;
      parts.forEach((seg, i) => { if (KINDS.includes(seg)) cut = i; });
      const key = cut >= 0 && cut < parts.length - 1
        ? parts.slice(cut + 1).join('-')
        : parts[parts.length - 1];
      const label = (g.textContent || '').replace(/\s+/g, ' ').trim();
      if (!key && !label) continue;
      let title = g.querySelector(':scope > title');
      if (!title) {
        title = document.createElementNS('http://www.w3.org/2000/svg', 'title');
        g.prepend(title);
      }
      title.textContent = key && label && key !== label ? `${key} — ${label}` : (label || key);
      g.style.cursor = 'help';
    }
  }

  function themeConfig() {
    const css = getComputedStyle(document.documentElement);
    const token = (name, fallback) => (css.getPropertyValue(name).trim() || fallback);

    const surface = token('--surface', '#151515');
    const sunken = token('--sunken', '#0a0a0a');
    const text = token('--text', '#bac6db');
    const muted = token('--muted', '#8a8a8a');
    const line = token('--line', '#2a2a2a');
    const accent = token('--accent', '#a4bd00');
    const edge = token('--edge', '#4a4a4a');
    const bg = token('--bg', '#000000');

    return {
      startOnLoad: false,
      securityLevel: 'strict',
      theme: 'base',
      fontFamily: 'ui-sans-serif, system-ui, -apple-system, "Segoe UI", sans-serif',
      flowchart: { curve: 'basis', useMaxWidth: true, padding: 14 },
      themeVariables: {
        // The board is single-theme dark, so this is a constant, not a probe.
        darkMode: true,
        background: bg,
        fontSize: '14px',

        // nodes: flat surface, hairline border — matches a Kanban card
        primaryColor: surface,
        mainBkg: surface,
        primaryTextColor: text,
        primaryBorderColor: line,
        nodeBorder: line,
        nodeTextColor: text,

        // the accent is spent on edges and emphasis, not on filling every box
        lineColor: edge,
        edgeLabelBackground: bg,
        tertiaryColor: sunken,
        tertiaryTextColor: text,
        tertiaryBorderColor: line,
        secondaryColor: sunken,
        secondaryTextColor: muted,
        secondaryBorderColor: line,

        clusterBkg: sunken,
        clusterBorder: line,
        titleColor: text,
        textColor: text,
        noteBkgColor: sunken,
        noteTextColor: text,
        noteBorderColor: line,
        accentColor: accent,
      },
    };
  }

  function showFallback(source, err) {
    $render.innerHTML = '';
    const pre = document.createElement('pre');
    pre.className = 'mono';
    pre.textContent = source;
    $render.append(pre);
    $error.hidden = false;
    $error.textContent = `Could not load mermaid: ${err && err.message ? err.message : String(err)}`;
  }

  async function renderNow(force = false) {
    const source = getDiagram().source || '';
    if (!force && source === lastRenderedSource) return;
    const token = ++activeToken;

    if (!source.trim()) {
      $render.innerHTML = '';
      const hint = document.createElement('p');
      hint.className = 'hint';
      hint.textContent = 'No diagram yet — type Mermaid source below to get started.';
      $render.append(hint);
      $error.hidden = true;
      $status.textContent = 'empty';
      lastRenderedSource = source;
      return;
    }

    let mermaid;
    try {
      mermaid = await loadMermaid();
    } catch (err) {
      if (token !== activeToken) return;
      $status.textContent = 'mermaid unavailable';
      showFallback(source, err);
      return;
    }

    const id = `diagram-render-${renderSeq++}`;
    try {
      mermaid.initialize(themeConfig());
      const { svg } = await mermaid.render(id, source);
      if (token !== activeToken) return;   // a newer render superseded this one
      $render.innerHTML = svg;
      annotateNodes();
      $error.hidden = true;
      $error.textContent = '';
      $status.textContent = 'rendered';
      lastRenderedSource = source;
    } catch (err) {
      document.getElementById(id)?.remove();   // mermaid can leave a stray node behind on failure
      if (token !== activeToken) return;
      // Keep whatever was rendered last (still in $render) and surface the error alongside it.
      $error.hidden = false;
      $error.textContent = `Mermaid syntax error: ${err && err.message ? err.message : String(err)}`;
      $status.textContent = 'render error — showing last good render';
    }
  }

  function scheduleRender() {
    clearTimeout(renderTimer);
    renderTimer = setTimeout(() => renderNow(), 400);
  }

  $source.addEventListener('input', () => {
    getDiagram().source = $source.value;
    ctx.save();
    scheduleRender();
  });

  $rerenderBtn.addEventListener('click', () => renderNow(true));

  $copyBtn.addEventListener('click', async () => {
    try {
      await navigator.clipboard.writeText(getDiagram().source || '');
      $status.textContent = 'copied';
      setTimeout(() => { if ($status.textContent === 'copied') $status.textContent = ''; }, 1500);
    } catch {
      $status.textContent = 'copy failed';
    }
  });

  $toggleBtn.addEventListener('click', () => {
    const nowHidden = !$editor.hidden;
    $editor.hidden = nowHidden;
    $toggleBtn.textContent = nowHidden ? 'Show source' : 'Hide source';
    $toggleBtn.setAttribute('aria-pressed', String(!nowHidden));
  });

  // Colour classes resolved from the live tokens, so a hand-coloured node still
  // belongs to the board's palette instead of fighting it. Two families: `ring`
  // keeps the dark surface and colours only the border (quiet, reads best on
  // black), `fill` is solid with dark ink for the one node that must shout.
  function paletteBlock() {
    const css = getComputedStyle(document.documentElement);
    const t = (name, fallback) => (css.getPropertyValue(name).trim() || fallback);
    const surface = t('--surface', '#151515');
    const text = t('--text', '#ccd4e0');
    const hues = [
      ['accent', t('--accent', '#a4bd00'), '#151515'],
      ['info', t('--focus', '#39bae6'), '#08141a'],
      ['warn', t('--mark', '#fb8c00'), '#1a0f00'],
      ['agent', t('--agent', '#a7adf4'), '#12142b'],
    ];
    const lines = ['', '%% palette from the board tokens'];
    for (const [name, hue] of hues) {
      lines.push(`classDef ${name} fill:${surface},stroke:${hue},stroke-width:2px,color:${text}`);
    }
    for (const [name, hue, ink] of hues) {
      lines.push(`classDef ${name}Fill fill:${hue},stroke:${hue},color:${ink}`);
    }
    lines.push(`classDef quiet fill:${surface},stroke:${t('--line-strong', '#3d3d3d')},color:${t('--muted', '#b4b4b4')}`);
    lines.push('%% use: NodeId:::accent     or     class A,B accentFill');
    return lines.join('\n');
  }

  $paletteBtn.addEventListener('click', () => {
    const diagram = getDiagram();
    if (diagram.source.includes('%% palette from the board tokens')) {
      $status.textContent = 'palette already there';
      setTimeout(() => { if ($status.textContent === 'palette already there') $status.textContent = ''; }, 1800);
      return;
    }
    diagram.source = (diagram.source.replace(/\s+$/, '')) + '\n' + paletteBlock() + '\n';
    $source.value = diagram.source;
    ctx.save();
    renderNow(true);
    $status.textContent = 'palette added';
    setTimeout(() => { if ($status.textContent === 'palette added') $status.textContent = ''; }, 1800);
  });


  $source.value = getDiagram().source;
  renderNow();

  return {
    refresh() {
      const diagram = getDiagram();
      if ($source.value !== diagram.source) $source.value = diagram.source;
      renderNow();
    },
  };
}
