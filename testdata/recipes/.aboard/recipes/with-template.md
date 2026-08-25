---
name: with-template
description: "Carries a tab skeleton, so --template has something to print."
when_to_use: "When the test needs a recipe whose template extraction can be checked."
tags: [fixture, template]
---

# with-template

A code block that is NOT the template, first, so "the first fenced block" would
pick the wrong one:

```json
{ "this": "is not the template" }
```

And the template itself:

```aboard-template
{
  "name": "Fixture tab",
  "type": "notes",
  "state": { "text": "replace me" }
}
```
