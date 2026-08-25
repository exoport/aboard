// Getting board content out of the board.
//
// Half of what lands here deserves to end up in a commit message, an issue or a
// PR description, and work that cannot leave the surface it was made on stays a
// demo. So: markdown for anything with structure, CSV for node tables, and the
// diagram's own SVG (mermaid already rendered one — no canvas tricks needed).
//
// Clipboard first, download second, deliberately. A VS Code webview may refuse a
// script-initiated download, and an affordance that silently does nothing is
// worse than one that is not offered — so every export is also copyable, and the
// copy is what the menu leads with.

/** Text of one tab as markdown. Returns '' for a type with no sensible text form. */
export function tabToMarkdown(tab, state) {
  const title = `# ${tab.name || tab.id}\n\n`;
  const st = state || {};

  switch (tab.type) {
    case 'notes':
      return title + (typeof st.text === 'string' ? st.text : '') + '\n';

    case 'dag':
    case 'kanban': {
      const nodes = Array.isArray(st.nodes) ? st.nodes : [];
      const byParent = (parent) => nodes
        .filter((n) => (n.parent || null) === parent)
        .sort((a, b) => (a.order || 0) - (b.order || 0));

      // A kanban reads by column, a DAG by tree — the same nodes, the shape the
      // renderer gives them.
      if (tab.type === 'kanban') {
        const cols = Array.isArray(st.columns) ? st.columns : [];
        const out = [title.trimEnd(), ''];
        for (const col of cols) {
          const items = nodes.filter((n) => n.status === col)
            .sort((a, b) => (a.order || 0) - (b.order || 0));
          out.push(`## ${col} (${items.length})`, '');
          for (const n of items) {
            out.push(`- **${n.title}** \`${n.id}\``);
            if (n.note) out.push(`  ${n.note}`);
          }
          out.push('');
        }
        return out.join('\n');
      }

      const lines = [title.trimEnd(), ''];
      const walk = (parent, depth) => {
        for (const n of byParent(parent)) {
          const pad = '  '.repeat(depth);
          lines.push(`${pad}- **${n.title}** \`${n.id}\`${n.status ? ` — _${n.status}_` : ''}`);
          if (n.note) lines.push(`${pad}  ${n.note}`);
          walk(n.id, depth + 1);
        }
      };
      walk(null, 0);
      return lines.join('\n') + '\n';
    }

    case 'chat': {
      const msgs = Array.isArray(st.messages) ? st.messages : [];
      const out = [title.trimEnd(), ''];
      for (const m of msgs) {
        out.push(`**${m.by || 'unknown'}** · ${m.at || ''}`, '', String(m.text || ''), '');
      }
      return out.join('\n');
    }

    case 'form': {
      const fields = Array.isArray(st.fields) ? st.fields : [];
      const out = [title.trimEnd(), ''];
      if (st.intro) out.push(st.intro, '');
      for (const f of fields) {
        out.push(`- **${f.label || f.id}** (\`${f.id}\`): ${JSON.stringify(f.value)}`);
      }
      return out.join('\n') + '\n';
    }

    case 'diagram':
      return title + '```mermaid\n' + String(st.source || '').trim() + '\n```\n';

    case 'markup': {
      const images = Array.isArray(st.images) ? st.images : [];
      const out = [title.trimEnd(), ''];
      for (const img of images) {
        out.push(`## ${img.caption || img.id}`, '', `![${img.caption || ''}](${img.src})`, '');
        for (const r of img.regions || []) {
          out.push(`- region \`${r.id}\` at ${pct(r.x)},${pct(r.y)} ${pct(r.w)}×${pct(r.h)}` +
            (r.note ? ` — ${r.note}` : ''));
        }
        for (const s of img.strokes || []) {
          out.push(`- stroke \`${s.id}\`` + (s.note ? ` — ${s.note}` : ''));
        }
        out.push('');
      }
      return out.join('\n');
    }

    case 'stack': {
      const blocks = Array.isArray(st.blocks) ? st.blocks : [];
      const out = [title.trimEnd(), ''];
      for (const b of blocks) {
        const inner = tabToMarkdown({ name: b.title || b.type, id: b.id, type: b.type }, b.state);
        out.push(inner.replace(/^# /, '## '), '');
      }
      return out.join('\n');
    }

    default:
      return '';
  }
}

const pct = (v) => Math.round(Number(v || 0) * 100) + '%';

/** Nodes as CSV — the form a spreadsheet or a script wants. */
export function nodesToCSV(state) {
  const nodes = Array.isArray(state && state.nodes) ? state.nodes : [];
  const cell = (v) => {
    const s = v === undefined || v === null ? '' : String(v);
    return /[",\n]/.test(s) ? '"' + s.replace(/"/g, '""') + '"' : s;
  };
  const rows = [['id', 'title', 'status', 'parent', 'order', 'note'].join(',')];
  for (const n of nodes) {
    rows.push([n.id, n.title, n.status, n.parent || '', n.order, n.note || ''].map(cell).join(','));
  }
  return rows.join('\n') + '\n';
}

/** The diagram's rendered SVG, lifted out of the DOM mermaid already drew it in. */
export function diagramSVG(rootEl) {
  const svg = rootEl && rootEl.querySelector('svg');
  if (!svg) return '';
  const clone = svg.cloneNode(true);
  clone.setAttribute('xmlns', 'http://www.w3.org/2000/svg');
  // A themed diagram on a transparent ground is unreadable anywhere else, so
  // paint the board's own background into the exported file.
  const bg = getComputedStyle(document.body).backgroundColor;
  const rect = document.createElementNS('http://www.w3.org/2000/svg', 'rect');
  rect.setAttribute('width', '100%');
  rect.setAttribute('height', '100%');
  rect.setAttribute('fill', bg || '#000');
  clone.insertBefore(rect, clone.firstChild);
  return '<?xml version="1.0" encoding="UTF-8"?>\n' + clone.outerHTML + '\n';
}

/**
 * Offer a file to the browser. Returns false when the environment refused it,
 * which a VS Code webview may well do — callers should have offered a copy too.
 */
export function download(filename, text, mime = 'text/plain') {
  try {
    const blob = new Blob([text], { type: mime + ';charset=utf-8' });
    const url = URL.createObjectURL(blob);
    const a = document.createElement('a');
    a.href = url;
    a.download = filename;
    a.style.display = 'none';
    document.body.append(a);
    a.click();
    setTimeout(() => { a.remove(); URL.revokeObjectURL(url); }, 4000);
    return true;
  } catch {
    return false;
  }
}

/** A filename that will not surprise anyone: aboard-<tab>-<id>.<ext> */
export function exportName(tab, ext) {
  const slug = String(tab.name || tab.type || 'tab')
    .toLowerCase().replace(/[^a-z0-9]+/g, '-').replace(/(^-|-$)/g, '') || 'tab';
  return `aboard-${slug}-${tab.id}.${ext}`;
}
