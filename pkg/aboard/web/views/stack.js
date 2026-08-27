// The stack type: several renderers in one tab, top to bottom.
//
// Most of what an agent wants to explain is not one shape. It is "here is the
// dependency graph, here is the decision I need from you about it, and here is
// the screenshot of the thing we are arguing about". A stack tab is that: an
// ordered list of blocks, each block a renderer with its own slice of state,
// mounted through the same contract the shell uses.
//
// Nesting is capped at one level. A stack inside a stack renders as a notice
// rather than recursing — the value is composition, not a tree of frames.

const STYLE_ID = 'stack-view-style';

const CSS = `
[data-view="stack"] .stack {
  display: flex;
  flex-direction: column;
  gap: 14px;
}
[data-view="stack"] .block {
  border: 1px solid var(--line);
  border-radius: 5px;
  background: var(--sunken);
  overflow: hidden;
}
[data-view="stack"] .block-head {
  display: flex;
  align-items: center;
  gap: 9px;
  padding: 8px 11px;
  background: var(--surface);
  border-bottom: 1px solid var(--line);
  cursor: pointer;
  user-select: none;
}
[data-view="stack"] .block[data-open="no"] .block-head { border-bottom: none; }
[data-view="stack"] .block-title {
  font-weight: 600;
  font-size: 0.92rem;
}
[data-view="stack"] .block-kind {
  font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
  font-size: 0.66rem;
  letter-spacing: 0.04em;
  text-transform: uppercase;
  color: var(--dim);
  border: 1px solid var(--line);
  border-radius: 2px;
  padding: 1px 4px;
}
[data-view="stack"] .block-caret {
  color: var(--muted);
  font-size: 0.8rem;
  width: 1em;
  text-align: center;
}
[data-view="stack"] .block-body { padding: 12px; }
[data-view="stack"] .block[data-open="no"] .block-body { display: none; }
/* A renderer sized for a full tab is too tall once it is one block of several.
   More specific than the renderers' own rules, so it wins. */
[data-view="stack"] [data-view="dag"] .canvas-wrap { height: min(38vh, 340px); }
[data-view="stack"] [data-view="diagram"] .diagram-editor textarea,
[data-view="stack"] [data-view="html"] .html-editor textarea { min-height: 150px; }
[data-view="stack"] [data-view="notes"] textarea { min-height: 26vh; }
[data-view="stack"] [data-view="chat"] .chat-scroll { max-height: 34vh; }
[data-view="stack"] .panel { border: none; background: none; padding: 0; }
[data-view="stack"] .stack-empty {
  color: var(--dim);
  font-size: 0.83rem;
}
`;

function injectStyle() {
  if (document.getElementById(STYLE_ID)) return;
  const style = document.createElement('style');
  style.id = STYLE_ID;
  style.textContent = CSS;
  document.head.append(style);
}

export function mountStack(root, ctx) {
  injectStyle();

  const depth = Number(ctx.depth) || 0;

  if (!Array.isArray(ctx.state.blocks)) ctx.state.blocks = [];

  // Collapsed/expanded is this viewer's business, not the document's.
  const collapsed = new Set();
  const mountedBlocks = new Map();   // block id -> { handle, body, type }

  const wrap = document.createElement('div');
  wrap.className = 'stack';
  root.append(wrap);

  const blocks = () => (Array.isArray(ctx.state.blocks) ? ctx.state.blocks : []);

  function blockState(block) {
    if (!block.state || typeof block.state !== 'object') block.state = {};
    return block.state;
  }

  // Bound by id for the same reason the shell binds tabs by id: an external
  // write reparses the whole document, so a captured block object is orphaned
  // and every later read and write goes to a graph nobody serialises. Blocks
  // are hit harder than tabs — refresh() below deliberately does NOT rebuild
  // when the block set is unchanged, which is exactly the common case.
  function ctxForBlock(blockId) {
    const live = () => blocks().find((b) => b.id === blockId) || { id: blockId };
    return {
      get state() { return blockState(live()); },
      get tab() { const b = live(); return { id: `${ctx.tab.id}/${b.id}`, name: b.title || b.type, type: b.type }; },
      save: ctx.save,
      nextId: ctx.nextId,
      types: ctx.types,
      initFor: ctx.initFor,
      mountType: ctx.mountType,
      depth: depth + 1,
      refreshOthers: () => {
        for (const [id, entry] of mountedBlocks) {
          if (id !== blockId) entry.handle?.refresh?.();
        }
      },
    };
  }

  function buildBlock(block, index) {
    const el = document.createElement('section');
    el.className = 'block';
    el.dataset.blockId = block.id;
    // Nested renderers style themselves off [data-view], so each block needs
    // its own marker or a stacked DAG would inherit the stack's rules.
    el.dataset.view = block.type;
    const open = !collapsed.has(block.id);
    el.dataset.open = open ? 'yes' : 'no';

    const head = document.createElement('div');
    head.className = 'block-head';
    head.setAttribute('role', 'button');
    head.tabIndex = 0;
    head.setAttribute('aria-expanded', String(open));

    const caret = document.createElement('span');
    caret.className = 'block-caret';
    caret.textContent = open ? '▾' : '▸';

    const title = document.createElement('span');
    title.className = 'block-title';
    title.textContent = block.title || `Block ${index + 1}`;

    const kind = document.createElement('span');
    kind.className = 'block-kind';
    const known = (ctx.types ? ctx.types() : []).find((t) => t.type === block.type);
    kind.textContent = known ? known.label : block.type;

    head.append(caret, title, kind);

    const body = document.createElement('div');
    body.className = 'block-body';

    const toggle = () => {
      const nowOpen = collapsed.has(block.id);
      if (nowOpen) collapsed.delete(block.id); else collapsed.add(block.id);
      el.dataset.open = nowOpen ? 'yes' : 'no';
      caret.textContent = nowOpen ? '▾' : '▸';
      head.setAttribute('aria-expanded', String(nowOpen));
      // Mounting is deferred until first expand, so a long stack does not pay
      // for renderers nobody opened.
      if (nowOpen) mountBody(block, body);
      else if (nowOpen === false) mountedBlocks.get(block.id)?.handle?.refresh?.();
    };

    head.addEventListener('click', toggle);
    head.addEventListener('keydown', (e) => {
      if (e.key === 'Enter' || e.key === ' ') { e.preventDefault(); toggle(); }
    });

    el.append(head, body);
    if (open) mountBody(block, body);
    return el;
  }

  function mountBody(block, body) {
    if (mountedBlocks.has(block.id)) {
      mountedBlocks.get(block.id).handle?.refresh?.();
      return;
    }
    body.replaceChildren();

    if (block.type === 'stack' || depth >= 1) {
      if (block.type === 'stack') {
        const p = document.createElement('p');
        p.className = 'hint';
        p.textContent = 'A stack cannot contain another stack. Put the blocks in this one instead.';
        body.append(p);
        return;
      }
    }

    let handle = null;
    try {
      handle = ctx.mountType(block.type, body, ctxForBlock(block.id));
      if (handle === null && !(ctx.types ? ctx.types() : []).some((t) => t.type === block.type)) {
        const p = document.createElement('p');
        p.className = 'hint';
        p.textContent = `No renderer for type "${block.type}".`;
        body.append(p);
      }
    } catch (err) {
      body.replaceChildren();
      const p = document.createElement('p');
      p.className = 'hint';
      p.textContent = `${block.type} block failed to load: ${err && err.message ? err.message : err}`;
      body.append(p);
      console.error(`[aboard] stack block ${block.id} (${block.type}) failed`, err);
    }
    mountedBlocks.set(block.id, { handle, body, type: block.type });
  }

  // A block's renderer may hold a timer or a listener on window/document — an
  // `html` block registers both — and neither is torn down by dropping the DOM.
  // Every path that stops minding a block goes through here.
  function dropBlock(id) {
    const entry = mountedBlocks.get(id);
    if (!entry) return;
    try { entry.handle?.destroy?.(); } catch { /* a block must not stop the stack */ }
    mountedBlocks.delete(id);
  }

  function render() {
    const list = blocks();

    // Drop anything whose block disappeared or changed type.
    for (const [id, entry] of [...mountedBlocks]) {
      const still = list.find((b) => b.id === id);
      if (!still || still.type !== entry.type) dropBlock(id);
    }

    if (!list.length) {
      const p = document.createElement('p');
      p.className = 'stack-empty';
      p.textContent = 'Empty stack. An agent adds blocks to state.blocks — each one { id, type, title, state }.';
      wrap.replaceChildren(p);
      return;
    }
    wrap.replaceChildren(...list.map(buildBlock));
  }

  render();

  return {
    refresh() {
      const list = blocks();
      const sameShape = list.length === wrap.querySelectorAll(':scope > .block').length
        && list.every((b, i) => wrap.children[i]?.dataset.blockId === b.id
          && wrap.children[i]?.dataset.view === b.type);

      // Only rebuild when the set of blocks actually changed; otherwise let each
      // renderer refresh in place so none of them lose their internal state.
      if (!sameShape) {
        render();
        return;
      }
      for (const entry of mountedBlocks.values()) entry.handle?.refresh?.();
    },
    // The shell calls this when the tab is torn down. Without it an `html` block
    // left its window `message` and document `aboard:theme` listeners behind for
    // the life of the page — the leak html.js's own destroy() fixes for a
    // top-level html TAB, and which nothing was calling for a block.
    destroy() {
      for (const id of [...mountedBlocks.keys()]) dropBlock(id);
    },
  };
}
