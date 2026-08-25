---
name: point-at-part-of-an-image
description: "Show an image the human can draw on, then read their regions and strokes back as pixels and name what each one landed on."
when_to_use: "When the thing you need them to point at is on a screen — a layout, a chart, a diff of two screenshots. Also when you need to prove you understood the mark they made."
tags: [markup, image, annotate, read-back]
---

# Point at part of an image

Put the image in `assets/`, then:

```js
upsertTab(b, 'layout', () => ({
  name: 'Layout review', type: 'markup',
  state: {
    layout: 'side-by-side',
    images: [
      { id: newId(b), src: 'assets/before.png', caption: 'Before', annotatable: true, regions: [], strokes: [] },
      { id: newId(b), src: 'assets/after.png',  caption: 'After',  annotatable: false },
    ],
  },
}));
```

`annotatable: false` makes the second image a reference only. Reading marks back,
converted to pixels and named:

```js
const tab = b.tabs.find((t) => t.key === 'layout');
const W = 1440, H = 900;                       // that image's real dimensions
for (const img of tab.state.images || []) {
  for (const r of img.regions || []) {
    console.log(`${img.id}/${r.id}: (${Math.round(r.x*W)},${Math.round(r.y*H)}) `
      + `${Math.round(r.w*W)}x${Math.round(r.h*H)} — ${r.note || 'no note'}`);
  }
  for (const s of img.strokes || []) {
    const pts = String(s.points || '').split(' ').map((p) => p.split(',').map(Number));
    const xs = pts.map((p) => p[0]*W), ys = pts.map((p) => p[1]*H);
    console.log(`${img.id}/${s.id}: scribble near `
      + `(${Math.round(Math.min(...xs))},${Math.round(Math.min(...ys))}) — ${s.note || 'no note'}`);
  }
}
```

Then name the element each mark landed on, so they can see you understood.
