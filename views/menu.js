// One context menu, shared by every renderer.
//
// Why it exists: ids are how the human and several agents refer to things across
// turns, and getting one out of the board used to mean reading it off a card and
// typing it back. Right-click, copy, paste — that is the whole feature, and it is
// used every session.
//
// Rules it follows, because a hand-rolled menu is easy to get wrong:
//   - Shift+right-click is left alone, so the browser's own menu is always
//     reachable (the caller checks that before calling in).
//   - Escape, an outside click, a scroll, or a blur closes it. Only one is ever
//     open.
//   - Arrow keys move, Enter runs, focus returns to where it came from. A menu
//     you cannot leave with the keyboard is a trap.
//   - It never mutates board state itself: entries are closures the caller owns.

import { button } from './controls.js';

const STYLE_ID = 'context-menu-style';
let openMenuEl = null;
let toastTimer = null;

function injectStyle() {
  if (document.getElementById(STYLE_ID)) return;
  const style = document.createElement('style');
  style.id = STYLE_ID;
  style.textContent = `
    .ctx-menu {
      position: fixed;
      z-index: 200;
      min-width: 208px;
      padding: 4px;
      background: var(--surface);
      border: 1px solid var(--line-strong);
      border-radius: 5px;
      box-shadow: 0 8px 24px rgb(0 0 0 / 55%);
      display: flex;
      flex-direction: column;
      gap: 1px;
    }
    .ctx-item {
      display: flex;
      align-items: baseline;
      justify-content: space-between;
      gap: 14px;
      width: 100%;
      text-align: left;
      font: inherit;
      font-size: 0.84rem;
      color: var(--text);
      background: none;
      border: 0;
      border-radius: 3px;
      padding: 6px 9px;
      cursor: pointer;
    }
    .ctx-item:hover, .ctx-item:focus-visible { background: var(--raised); outline: none; }
    .ctx-item[data-danger="yes"] { color: var(--danger); }
    .ctx-item .ctx-hint {
      font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
      font-size: 0.74rem;
      color: var(--dim);
    }
    .ctx-sep { height: 1px; background: var(--line); margin: 3px 2px; }
    .ctx-head {
      font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
      font-size: 0.72rem;
      letter-spacing: 0.06em;
      text-transform: uppercase;
      color: var(--dim);
      padding: 5px 9px 3px;
    }
    .ctx-toast {
      position: fixed;
      z-index: 210;
      padding: 6px 11px;
      background: var(--raised);
      border: 1px solid var(--accent-dim);
      border-radius: 4px;
      color: var(--text);
      font-size: 0.82rem;
      pointer-events: none;
    }
  `;
  document.head.append(style);
}

export function closeContextMenu() {
  if (!openMenuEl) return;
  const returnTo = openMenuEl.__returnFocus;
  openMenuEl.remove();
  openMenuEl = null;
  document.removeEventListener('pointerdown', onOutside, true);
  document.removeEventListener('keydown', onKey, true);
  window.removeEventListener('scroll', closeContextMenu, true);
  window.removeEventListener('blur', closeContextMenu);
  if (returnTo && document.contains(returnTo)) returnTo.focus?.();
}

function onOutside(e) {
  if (openMenuEl && !openMenuEl.contains(e.target)) closeContextMenu();
}

function onKey(e) {
  if (!openMenuEl) return;
  const items = [...openMenuEl.querySelectorAll('.ctx-item')];
  if (e.key === 'Escape') { e.preventDefault(); closeContextMenu(); return; }
  if (e.key !== 'ArrowDown' && e.key !== 'ArrowUp') return;
  e.preventDefault();
  const at = items.indexOf(document.activeElement);
  const next = e.key === 'ArrowDown'
    ? items[(at + 1) % items.length]
    : items[(at - 1 + items.length) % items.length];
  next?.focus();
}

/** A short confirmation near the pointer — copying with no feedback feels broken. */
export function flashToast(text, x, y) {
  injectStyle();
  clearTimeout(toastTimer);
  document.querySelector('.ctx-toast')?.remove();
  const el = document.createElement('div');
  el.className = 'ctx-toast';
  el.textContent = text;
  el.style.left = '0px';
  el.style.top = '0px';
  document.body.append(el);
  const box = el.getBoundingClientRect();
  el.style.left = Math.min(Math.max(8, x), innerWidth - box.width - 8) + 'px';
  el.style.top = Math.min(Math.max(8, y), innerHeight - box.height - 8) + 'px';
  toastTimer = setTimeout(() => el.remove(), 1400);
}

export async function copyText(text, ev) {
  const x = ev ? ev.clientX + 10 : innerWidth / 2;
  const y = ev ? ev.clientY + 10 : 40;
  try {
    await navigator.clipboard.writeText(text);
    flashToast('copied', x, y);
    return true;
  } catch {
    flashToast('copy blocked — select it by hand', x, y);
    return false;
  }
}

/**
 * items: array of { label, hint?, danger?, run } | 'separator' | { head }
 * Falsy entries are skipped, so callers can inline conditions.
 */
export function openContextMenu(ev, items) {
  injectStyle();
  closeContextMenu();
  ev.preventDefault();

  const menu = document.createElement('div');
  menu.className = 'ctx-menu';
  menu.setAttribute('role', 'menu');
  menu.__returnFocus = document.activeElement;

  for (const item of items) {
    if (!item) continue;
    if (item === 'separator') {
      const sep = document.createElement('div');
      sep.className = 'ctx-sep';
      menu.append(sep);
      continue;
    }
    if (item.head) {
      const head = document.createElement('div');
      head.className = 'ctx-head';
      head.textContent = item.head;
      menu.append(head);
      continue;
    }
    const btn = button('', '', { className: 'ctx-item' });
    btn.setAttribute('role', 'menuitem');
    if (item.danger) btn.dataset.danger = 'yes';
    const label = document.createElement('span');
    label.textContent = item.label;
    btn.append(label);
    if (item.hint) {
      const hint = document.createElement('span');
      hint.className = 'ctx-hint';
      hint.textContent = item.hint;
      btn.append(hint);
    }
    btn.addEventListener('click', () => {
      // Close first: an entry that opens a modal or moves focus should not be
      // fighting a menu that is still on screen.
      const run = item.run;
      closeContextMenu();
      run?.(ev);
    });
    menu.append(btn);
  }

  // Placed after measuring, so it never opens off the edge of the window.
  menu.style.left = '0px';
  menu.style.top = '0px';
  document.body.append(menu);
  const box = menu.getBoundingClientRect();
  menu.style.left = Math.min(ev.clientX, innerWidth - box.width - 6) + 'px';
  menu.style.top = Math.min(ev.clientY, innerHeight - box.height - 6) + 'px';

  openMenuEl = menu;
  document.addEventListener('pointerdown', onOutside, true);
  document.addEventListener('keydown', onKey, true);
  window.addEventListener('scroll', closeContextMenu, true);
  window.addEventListener('blur', closeContextMenu);
  menu.querySelector('.ctx-item')?.focus();
}

/** The board-wide address of one object: what "copy link" puts on the clipboard. */
export function referenceFor(tabId, nodeId) {
  const base = location.origin + location.pathname;
  return nodeId ? `${base}#tab=${tabId}&node=${nodeId}` : `${base}#tab=${tabId}`;
}
