---
name: build-something-interactive
description: "Ship a bespoke sandboxed widget as an html tab when the interaction itself is the point and no renderer covers it."
when_to_use: "Only when the INTERACTION is the point — canvas, drag-and-drop, a simulation. Prefer a `ui` tab whenever a component tree can express it: it cannot get the theme wrong and the next session can change one node of it."
tags: [html, widget, sandbox, bridge]
---

# Build something interactive that no type covers

```js
upsertTab(b, 'sorter', () => ({
  name: 'Risk sorter', type: 'html',
  state: {
    html: [
      '<h3>Rank by risk</h3><ul id="l" style="list-style:none;padding:0"></ul>',
      '<script>',
      '  var items = aboard.get().order || ["schema drift","auth rewrite"];',
      '  var l = document.getElementById("l");',
      '  function draw(){ l.textContent="";',
      '    items.forEach(function(t,i){',
      '      var li=document.createElement("li");',
      '      var up=document.createElement("button"); up.textContent="↑";',
      '      up.onclick=function(){ if(i>0){ var x=items[i-1]; items[i-1]=items[i]; items[i]=x;',
      '        aboard.set({order:items}); draw(); } };',
      '      var s=document.createElement("span"); s.textContent=" "+(i+1)+". "+t;',
      '      li.appendChild(up); li.appendChild(s); l.appendChild(li); });',
      '    aboard.fit(); }',
      '  draw();',
      '<\/script>',
    ].join('\n'),
    data: {},
  },
}));
```

The frame is sandboxed with no network access. It persists through
`aboard.set(value)`, which lands in `state.data` — read it back from there.
