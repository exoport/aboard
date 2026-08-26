#!/usr/bin/env sh
# Headless smoke test: mounts every view against a real running board and checks
# each one actually rendered something. Catches the failure mode a syntax check
# cannot — a module that parses fine but throws on mount.
#
# Usage:  ./test/smoke.sh                          (server must already be running)
#         PROJECT=/tmp/probe ./test/smoke.sh       (which board to run against)
#         PORT=41234 ./test/smoke.sh               (override the discovered port)

set -e
cd "$(dirname "$0")/.."
REPO=$PWD

# PROJECT is the board this suite runs against — a directory containing
# `.aboard/`. It used to be the repo root, unconditionally and invisibly, and
# there was no way to say otherwise: the paths were literals, not overrides.
#
# This suite WRITES. It applies documents, renames a tab, uploads an image and
# pokes the notify channel, releasing any session genuinely blocked on `aboard
# wait`. The repo root is where this project's own board lives, so it is the last
# board it should be aimed at, and the rule everywhere else here is that a board
# for testing goes in a scratch project.
#
#   cd /tmp/probe && aboard init --example --gitignore && aboard serve &
#   PROJECT=/tmp/probe make smoke
#
# There is deliberately NO default. Defaulting to the repo root would leave the
# forbidden target one forgotten variable away, and that is not hypothetical: it
# happened while this very change was being tested — a bare run against the repo
# read the repo's board over HTTP from the scratch server's port, and only died
# early, by luck, before its first write. A default the project's own rules
# forbid is not a default, it is a trap. Say `PROJECT=.` if you really mean this
# checkout.
if [ -z "$PROJECT" ]; then
  cat >&2 <<'USAGE'
PROJECT is required: it says which board to run against, and this suite WRITES to it
(applies documents, renames a tab, uploads an image, pokes the notify channel).

  mkdir -p /tmp/probe && cd /tmp/probe
  aboard init --example --gitignore
  aboard serve &                       # detached, from that directory
  PROJECT=/tmp/probe make smoke        # from the repo

Use PROJECT=. only if you really mean this checkout's own board.
USAGE
  exit 2
fi
case "$PROJECT" in
  /*) ;;
  *) PROJECT="$REPO/$PROJECT" ;;
esac
if [ ! -d "$PROJECT/.aboard" ]; then
  echo "no .aboard/ in $PROJECT — run 'aboard init --example --gitignore' there first" >&2
  exit 2
fi

# Every `aboard` subcommand that touches CONTENT goes through this, because the
# CLI resolves a project root from its working directory and this script's cwd is
# the repo. Without it the HTTP half of the suite talked to the server under test
# while `aboard apply` wrote to the repo's own board — two boards in one run, and
# not one assertion able to tell.
ab() { ./aboard --cwd "$PROJECT" "$@"; }

# Discover the port from the running instance rather than assuming one: the port
# is derived per project, so it is not a fixed number any more.
INSTANCE="$PROJECT/.aboard/run/instance.json"
STATE="$PROJECT/.aboard/aboard.json"
if [ -z "$PORT" ] && [ -f "$INSTANCE" ]; then
  PORT=$(sed -n 's/.*"port"[[:space:]]*:[[:space:]]*\([0-9]*\).*/\1/p' "$INSTANCE")
fi
if [ -z "$PORT" ]; then
  echo "no running board found ($INSTANCE missing) — start it with 'aboard serve'" >&2
  exit 1
fi
# A board served under --base-path answers only under that prefix, so build every
# URL from the instance record rather than from the port alone.
#
# The `|| true` is load-bearing under `set -e`: a command substitution that exits
# non-zero aborts the whole script, and with PORT supplied by hand there may be no
# instance file for sed to read. It aborted SILENTLY — no message, exit 1, and a
# `make smoke` whose entire output was the make error line.
BASE=$(sed -n 's/.*"url"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' "$INSTANCE" 2>/dev/null || true)
[ -z "$BASE" ] && BASE="http://localhost:$PORT"
# The web tree moved into the Go package that embeds it; the static greps below
# read source, so they need the real directory rather than the URL path.
WEB="pkg/aboard/web"
export STATE WEB

BROWSER=""
for c in chromium chromium-browser google-chrome google-chrome-stable; do
  if command -v "$c" >/dev/null 2>&1; then BROWSER="$c"; break; fi
done
if [ -z "$BROWSER" ]; then
  echo "no chromium-family browser found; skipping" >&2
  exit 0
fi

if ! curl -sf -o /dev/null "$BASE/aboard.json"; then
  echo "server not answering on $BASE — start it with 'aboard serve' in $PROJECT" >&2
  exit 1
fi

# Assertion helpers, shared by every section below.
check() {
  if [ "$2" = "$3" ]; then echo "  ok   $1 ($3)"; else echo "  FAIL $1 (want $3, got $2)"; FAILED=1; fi
}
check_ge() {
  if [ "$2" -ge "$3" ] 2>/dev/null; then echo "  ok   $1 ($2 >= $3)"; else echo "  FAIL $1 (want >= $3, got $2)"; FAILED=1; fi
}
waiting_count() {
  curl -s "$BASE/waiters" | node -e "
    let d='';process.stdin.on('data',c=>d+=c).on('end',()=>{
      try { process.stdout.write(String(JSON.parse(d).waiting)); } catch { process.stdout.write('?'); }
    })"
}
FAILED=0

echo "== views mount and render =="
# Captured once and reused: the harness costs ~10s to run, and a second assertion
# below reads the same log rather than paying for it twice.
SMOKE_LOG=$(timeout 90 "$BROWSER" --headless --no-sandbox --disable-gpu --disable-dev-shm-usage \
  --virtual-time-budget=10000 --dump-dom "$BASE/test/smoke.html" 2>/dev/null \
  | grep -o 'SMOKE_RESULT.*' | head -1 | sed 's/SMOKE_RESULT //' || true)
printf '%s' "$SMOKE_LOG" \
  | node -e "let d='';process.stdin.on('data',c=>d+=c).on('end',()=>{
      let lines;
      try { lines = JSON.parse(d.replace(/<\/div>.*/s, '')); }
      catch { console.error('  could not parse smoke output'); process.exit(1); }
      let bad = 0;
      for (const l of lines) {
        const fail = /THREW|MISSING EXPORT|ERROR|REJECTION/.test(l);
        if (fail) bad++;
        console.log((fail ? '  FAIL ' : '  ok   ') + l);
      }
      process.exit(bad ? 1 : 0);
    })"

# "It mounted" is not "it rendered the right text". kv resolved its pairs array
# but not the values inside it, so a {bind} came out as "[object Object]" — a
# mount check cannot see that, and the gallery could not either, because its kv
# used literal values. Asserted on the harness LOG rather than the dumped DOM:
# the page's own inline script contains these same strings, which has produced
# three phantom passes in this repo already.
printf '%s' "$SMOKE_LOG" | node -e "
  let d='';process.stdin.on('data',c=>d+=c).on('end',()=>{
    let line='';
    try { line=(JSON.parse(d.replace(/<\/div>.*/s,''))||[]).find((l)=>l.startsWith('kv resolves a bind:'))||''; } catch {}
    const got=line.replace('kv resolves a bind: ','');
    const want='bound key|bound value|literal|still works';
    const ok=got===want;
    console.log((ok?'  ok   ':'  FAIL ')+'kv resolves a {bind} on both sides of a pair ('+(got||'no probe line')+')');
    if(!ok) process.exitCode=1;
  })" || FAILED=1

# The runtime half of the control check: nothing may ask for a control id that no
# spec declares. The static grep below cannot see this — an id could be built at
# runtime — and the marker is what makes it visible rather than blank.
printf '%s' "$SMOKE_LOG" | node -e "
  let d='';process.stdin.on('data',c=>d+=c).on('end',()=>{
    let lines=[]; try { lines=JSON.parse(d.replace(/<\/div>.*/s,''))||[]; } catch {}
    const bad=(lines.find((l)=>l.startsWith('undeclared controls rendered:'))||'').replace('undeclared controls rendered: ','');
    const drawn=(lines.find((l)=>l.startsWith('declared controls rendered:'))||'').replace('declared controls rendered: ','');
    const ok=bad==='none' && Number(drawn)>0;
    console.log((ok?'  ok   ':'  FAIL ')+'every rendered control resolves to a declaration ('+drawn+' drawn, undeclared: '+(bad||'?')+')');
    if(!ok) process.exitCode=1;
  })" || FAILED=1

echo "== each tab activates in the real shell =="
# Tabs are data, and ids are bare numbers that also appear on nested objects, so
# parse the tabs array rather than grepping for an id-shaped string.
TABS=$(node -e "
  const b=JSON.parse(require('fs').readFileSync(process.env.STATE,'utf8'));
  process.stdout.write((b.tabs||[]).map(t=>t.id).join(' '));
")
for tab in $TABS; do
  dom=$(timeout 90 "$BROWSER" --headless --no-sandbox --disable-gpu --disable-dev-shm-usage \
    --virtual-time-budget=9000 --dump-dom "$BASE/?nosse=1&tab=$tab" 2>/dev/null)
  printf '%s' "$dom" | node -e "
    let d=''; process.stdin.on('data',c=>d+=c).on('end',()=>{
      const tab = process.argv[1];
      const active = (d.match(/data-tab=\"([A-Za-z0-9]+)\"[^>]*data-active=\"yes\"/) || [])[1];
      // Exclude the shell's own source: the template literals that BUILD these
      // messages contain the same words, and matched as false positives.
      const fail = (d.match(/(view failed to load|No renderer for type)[^<]*/g) || [])
        .filter((m) => !m.includes(String.fromCharCode(36, 123)))[0];
      const ok = active === tab && !fail;
      console.log((ok ? '  ok   ' : '  FAIL ') + 'tab=' + tab + ' active=' + active + (fail ? ' ' + fail : ''));
      if (!ok) process.exitCode = 1;
    });
  " "$tab"
done

echo "== a tab can be promoted into the project's own documents =="
# Promotion is the whole point of the middle tier: what a board exchange settles
# has to reach a spec file cheaply, or it decays. Like -capabilities, this must
# work with no server — an agent should never have to start one to read out a
# conclusion.
if [ -x ./aboard ]; then
  check "a tab exports as markdown with no server" \
    "$(ab export decisions 2>/dev/null | head -1 | cut -c1-1)" "#"
  check "a rows tab exports as csv" \
    "$(ab export table-example --format csv 2>/dev/null | head -1 | cut -d, -f1)" "id"
  # `set -e` aborts a subshell the moment the command inside it fails, so
  # `; echo $?` never runs and the check reads empty. Handle the failure inline.
  check "an unknown tab is refused, not silently empty" \
    "$(ab export definitely-not-a-tab >/dev/null 2>&1 && echo 0 || echo 1)" "1"
  # A gate export is only worth pasting if the REASON travels with the verdict.
  check "a gate export carries the reasons" \
    "$(ab export decisions 2>/dev/null | grep -c 'Why:' | awk '{print ($1>0)?"yes":"no"}')" "yes"
else
  echo "  skip no ./aboard binary — run make build"
fi

echo "== html tabs can be framed where the board is actually used =="
# frame-ancestors is checked against EVERY ancestor, so 'self' alone left every
# html tab blank inside VS Code's webview — the board is same-origin but the
# webview above it is not. If someone narrows this list back, that failure is
# invisible until a human opens the docked browser, so assert the header itself.
HTML_TAB=$(node -e "
  const b=JSON.parse(require('fs').readFileSync(process.env.STATE,'utf8'));
  const t=(b.tabs||[]).find((t)=>t.type==='html');
  process.stdout.write(t ? t.id : '');
")
if [ -n "$HTML_TAB" ]; then
  CSP=$(curl -s -D - -o /dev/null "$BASE/tab/$HTML_TAB/html" | tr -d '\r' | sed -n 's/^[Cc]ontent-[Ss]ecurity-[Pp]olicy: //p')
  case "$CSP" in
    *"frame-ancestors 'self' vscode-webview:"*) echo "  ok   html tab allows the vscode webview to frame it" ;;
    *) echo "  FAIL frame-ancestors no longer lists vscode-webview: ($CSP)"; FAILED=1 ;;
  esac
  # The two that are the actual containment must NOT have been relaxed alongside.
  case "$CSP" in
    *"connect-src 'none'"*) echo "  ok   html tab still has no network egress" ;;
    *) echo "  FAIL connect-src is no longer 'none' — that IS the containment"; FAILED=1 ;;
  esac

  # A literal <\/script> in a widget silently swallows the whole script: the
  # static markup renders, nothing runs, and it looks like a styling problem.
  # Cost an hour once; it is one grep.
  printf '%s' "$(curl -s "$BASE/tab/$HTML_TAB/html")" | node -e "
    let d='';process.stdin.on('data',c=>d+=c).on('end',()=>{
      const escaped = d.includes('<\\\\/script>');
      const real = d.includes('</script>');
      const ok = real && !escaped;
      console.log((ok ? '  ok   ' : '  FAIL ') +
        'the widget script is terminated (real </script>=' + real + ', escaped=' + escaped + ')');
      if (!ok) process.exitCode = 1;
    });
  "
  # The bridge is a NAME contract, in both directions: the widget calls it as a
  # bare global and the parent matches on the envelope property. Rename one half
  # and the frame posts messages the parent silently drops — no error anywhere.
  BODY=$(curl -s "$BASE/tab/$HTML_TAB/html")
  MISSING=""
  for want in 'window.aboard' '__ABOARD_DATA__'; do
    case "$BODY" in *"$want"*) ;; *) MISSING="$MISSING $want" ;; esac
  done
  check "the bridge is served under its aboard names" "${MISSING:-none}" "none"
  STALE=""
  for gone in 'window.board' '__BOARD_DATA__' '__board'; do
    case "$BODY" in *"$gone"*) STALE="$STALE $gone" ;; esac
  done
  check "no pre-rename bridge name survives in the served frame" "${STALE:-none}" "none"
else
  echo "  skip no html tab on this board"
fi

# An html block inside a stack asks for /tab/<tabId>/<blockId>/html, because that
# is what its ctx reports as an id. serveTabHTML used to require an exact tab of
# type html, so the frame 404'd and the block rendered BLANK — no error, no
# marker, just an empty box that read as a styling problem. The block path is
# therefore its own assertion, and so is the sandbox, since the containment must
# be identical to a top-level tab's.
BLOCK_PATH=$(node -e "
  const b=JSON.parse(require('fs').readFileSync(process.env.STATE,'utf8'));
  for (const t of b.tabs||[]) {
    if (t.type!=='stack') continue;
    const blk=((t.state||{}).blocks||[]).find((x)=>x.type==='html');
    if (blk) { process.stdout.write(t.id+'/'+blk.id); break; }
  }
")
if [ -n "$BLOCK_PATH" ]; then
  ENC=$(printf '%s' "$BLOCK_PATH" | sed 's|/|%2F|')
  check "an html block inside a stack serves its own document" \
    "$(curl -s -o /dev/null -w '%{http_code}' "$BASE/tab/$ENC/html")" "200"
  # It must serve the BLOCK's state, not the parent tab's: a stack has no
  # state.html of its own, so reading the wrong level yields the empty-tab
  # placeholder and looks like an empty widget rather than a wrong lookup.
  case "$(curl -s "$BASE/tab/$ENC/html")" in
    *"An agent sets"*) echo "  FAIL the block path served the empty-tab placeholder, not the block's html"; FAILED=1 ;;
    *) echo "  ok   the block path serves the block's own html" ;;
  esac
  case "$(curl -s -D - -o /dev/null "$BASE/tab/$ENC/html" | tr -d '\r' | sed -n 's/^[Cc]ontent-[Ss]ecurity-[Pp]olicy: //p')" in
    *"connect-src 'none'"*) echo "  ok   a block's document is contained exactly like a tab's" ;;
    *) echo "  FAIL a block's document lost connect-src 'none'"; FAILED=1 ;;
  esac
  # Every wrong path must say WHICH thing was wrong. The whole cost of this defect
  # was that it failed silently, so a 404 with no reason would repeat the mistake
  # in a smaller way.
  STACK_TAB=${BLOCK_PATH%%/*}
  BAD=""
  for probe in "$STACK_TAB%2Fnope" "$STACK_TAB%2F"; do
    body=$(curl -s "$BASE/tab/$probe/html")
    case "$body" in *"has no block"*) ;; *) BAD="$BAD $probe" ;; esac
  done
  check "a wrong block id says so instead of 404ing blankly" "${BAD:-none}" "none"
else
  echo "  skip no stack tab has an html block"
fi

echo "== the board describes itself (and the skill cannot silently disagree) =="
# The manifest must answer with NO server: a fresh checkout, a copied binary, or
# another session holding the port must all still be able to ask what this board
# can do.
#
# These calls are the one group that stays on the REPO, not on $PROJECT: --check
# compares the binary's self-description against the skill reference committed
# here, and it treats a missing reference as "nothing to check", so aiming it at
# a scratch project would turn the drift check into a guaranteed pass.
if [ -x ./aboard ]; then
  check "the manifest answers with no server needed" \
    "$(PORT= ./aboard capabilities 2>/dev/null | node -e "
      let d='';process.stdin.on('data',c=>d+=c).on('end',()=>{
        try { const m=JSON.parse(d); process.stdout.write(String((m.types||[]).length>0 && !!m.capsHash)); }
        catch { process.stdout.write('false'); }
      })")" "true"

  check "one type can be asked for on its own" \
    "$(./aboard capabilities dag 2>/dev/null | node -e "
      let d='';process.stdin.on('data',c=>d+=c).on('end',()=>{
        try { const m=JSON.parse(d); process.stdout.write(String(m.types.length===1 && m.types[0].type==='dag')); }
        catch { process.stdout.write('false'); }
      })")" "true"

  # THE drift check: the committed reference must still match the binary.
  if ./aboard capabilities --check >/dev/null 2>&1; then
    echo "  ok   the committed skill reference matches the binary"
  else
    echo "  FAIL the committed skill reference is stale — run make caps"; FAILED=1
  fi

  # Every declared type must have a renderer, and every renderer a declaration.
  # A missing spec is how a capability goes undocumented; a missing mount is a
  # blank tab.
  printf '%s' "$(./aboard capabilities 2>/dev/null)" | node -e "
    const fs=require('fs');
    let d='';process.stdin.on('data',c=>d+=c).on('end',()=>{
      let declared=[];
      try { declared=(JSON.parse(d).types||[]).map((t)=>t.type).sort(); } catch {}
      const shell=fs.readFileSync(process.env.WEB+'/aboard.html','utf8');
      const block=shell.slice(shell.indexOf('const TYPES = {'), shell.indexOf('const \$tabs'));
      const mounted=[...block.matchAll(/^\s{2}(\w+):\s*\{\s*label:/gm)].map((m)=>m[1]).sort();
      const missingSpec=mounted.filter((t)=>!declared.includes(t));
      const missingMount=declared.filter((t)=>!mounted.includes(t));
      const ok=missingSpec.length===0 && missingMount.length===0 && declared.length>0;
      console.log((ok?'  ok   ':'  FAIL ')+'every type has a spec and a mount ('+declared.length+' types'
        +(missingSpec.length?', no spec for: '+missingSpec:'')
        +(missingMount.length?', no mount for: '+missingMount:'')+')');
      if(!ok) process.exitCode=1;
    });
  "

  # Controls: the declaration and the code must not drift apart, checked in BOTH
  # directions and neither of them by matching prose.
  #
  # This replaces the DOM sweep originally planned here (phase 4: collect every
  # button[title] and assert it appears in `gestures`). Measured on the dag tab,
  # that sweep found 23 candidate titles of which 17 were tab-strip chrome, to
  # surface ~4 real gaps — a ratio that gets a check muted. Rendering controls FROM
  # the declaration made the fuzzy half unnecessary: an undeclared control is
  # visible on screen, so what is left to test is static and exact.

  # 1. Every button in a renderer goes through the helper. A grep, not a browser:
  #    deterministic, instant, and it cannot be fooled by a control that only
  #    appears in a state the harness never drives.
  # aboard.html is included: it is the last place the "one way to make a button"
  # rule used to stop. Its buttons are shell chrome with no spec, so they use the
  # plain button() rather than a declared control — but they go through the helper
  # like everything else, which is what makes this grep total instead of "total
  # except one file nobody remembers".
  #    Comments are stripped first, and that is not fussiness: this check FIRED on
  #    views/diagram.js, whose header comment records why its four buttons were
  #    moved into a spec — and names the very call the fix stopped making. A check
  #    that fails on a note explaining the rule teaches people to delete the note.
  #    (Same family as the older `--dump-dom` bite, where the grep matched the
  #    page's own inline script.)
  RAW=""
  for f in "$WEB"/views/*.js "$WEB"/aboard.html; do
    case "$f" in *views/controls.js) continue ;; esac
    if sed 's://.*::' "$f" | grep -q "createElement('button')"; then
      RAW="$RAW $f"
    fi
  done
  RAW=$(echo $RAW)
  check "every button goes through controls.js" "${RAW:-none}" "none"

  # 2. Every DECLARED control is actually used by its renderer. The other
  #    direction, and the one nothing else covers: a control that is removed from
  #    the code leaves its declaration behind, and a spec describing a button that
  #    no longer exists is exactly the drift this whole series is about.
  ORPHANS=$(./aboard capabilities 2>/dev/null | node -e "
    const fs=require('fs');
    let d='';process.stdin.on('data',c=>d+=c).on('end',()=>{
      let types=[]; try { types=(JSON.parse(d).types||[]); } catch {}
      const bad=[];
      for (const t of types) {
        const ids=(t.controls||[]).map((c)=>c.id);
        if (!ids.length) continue;
        let src=''; try { src=fs.readFileSync(process.env.WEB+'/views/'+t.type+'.js','utf8'); } catch { continue; }
        for (const id of ids) if (!src.includes(\"'\"+id+\"'\")) bad.push(t.type+'.'+id);
      }
      process.stdout.write(bad.length ? bad.join(' ') : 'none');
    })")
  check "every declared control is used by its renderer" "${ORPHANS:-none}" "none"

  # 3. Advisory, never a failure: which renderer buttons are still plain ones.
  #    Whether a button is a CAPABILITY an agent should know about or merely an
  #    affordance is a judgement no rule can make — a dialog's Cancel is not worth
  #    declaring, a delete-row button is. So this reports and a human decides,
  #    which is the honest version of the check that was planned here.
  PLAIN=$(grep -cE "(^|[^.\w])button\(" "$WEB"/views/*.js 2>/dev/null \
    | grep -v 'views/controls.js' | grep -v ':0$' | sed "s|$WEB/||" | tr '\n' ' ' || true)
  echo "  note plain (undeclared) buttons remain in: ${PLAIN:-none}"

  # And every route the manifest advertises must actually answer.
  MISSING=""
  for pair in "GET /capabilities" "GET /waiters" "GET /journal" "GET /health"; do
    m=$(echo "$pair" | cut -d' ' -f1); path=$(echo "$pair" | cut -d' ' -f2)
    code=$(curl -s -o /dev/null -w '%{http_code}' -X "$m" "$BASE$path")
    [ "$code" = "200" ] || MISSING="$MISSING $path($code)"
  done
  check "the advertised routes answer" "${MISSING:-none}" "none"
else
  echo "  skip no ./aboard binary — run make build"
fi

echo "== the agent hears about a bad write, not the human =="
# The failure this section guards: `ui` fails silently and successfully. -apply
# prints "applied", exit 0, and the human finds the empty panel on their screen a
# round trip later — by which time the agent that caused it has moved on. Every
# check here asserts a detection happens at the WRITE.
#
# All of them run -apply against a document with a deliberately stale updatedAt:
# the warnings print before the POST, and the POST is then refused by
# compare-and-set, so the suite asserts the detector without touching the board.
if [ -x ./aboard ]; then
  BADDOC=$(mktemp)
  node -e "
    const fs=require('fs');
    const d=JSON.parse(fs.readFileSync(process.env.STATE,'utf8'));
    d.version = 2;                                  // the incident: copied from a stale doc
    d.updatedAt = '1999-01-01T00:00:00.000Z';       // stale on purpose, so the POST is refused
    d.tabs = [{ id:'zz1', name:'probe', type:'ui', state:{
      root:{ type:'card', title:'x', tone:'accent', children:[
        { type:'badge', value:'x', tone:'claude' },     // a name that USED to work
        { type:'stat', value:'1', label:'ok', tone:'agent' },  // the current one
        { type:'kv', items:[{ k:'a', v:'b' }] },
        { type:'kv', pairs:[{ key:'a', value:{ bind:'nope' } },
                            { key:'b', value:{ bind:'initialised' } }] },
        { type:'sparkline' },
        { type:'field', label:'ok', bind:'written.later', field:'text' },
      ]},
      data:{ initialised: null },
    }}, { id:'zz2', name:'probe2', type:'stack', state:{ blocks:[
      { id:'zz3', type:'notes', name:'wrong', state:{ txt:'x' } },
    ]}}, { id:'zz4', name:'probe3', type:'markup', state:{ images:[
      { id:'i1', src:'assets/x.png', marks:[
        { id:'m1', color:'claude' },                    // likewise
        { id:'m2', color:'mark' },
      ]},
    ]}}];
    fs.writeFileSync(process.argv[1], JSON.stringify(d));
  " "$BADDOC"
  WARN=$(ab apply --by "smoke" < "$BADDOC" 2>&1 >/dev/null || true)
  rm -f "$BADDOC"

  want_warning() {
    case "$WARN" in
      *"$2"*) echo "  ok   $1" ;;
      *) echo "  FAIL $1 (no warning matching '$2')"; FAILED=1 ;;
    esac
  }
  # A version no renderer accepts is worse than an undeclared field: it blanks the
  # WHOLE board rather than one component. It was the one thing on the write path
  # nothing checked.
  want_warning "a stale schema version is reported to the writer" 'says "version": 2'
  want_warning "an unknown prop on a known component is reported" 'does not read "items"'
  want_warning "an unknown prop on a container is reported" 'does not read "tone"'
  want_warning "a component outside the catalog is reported" 'is not in the catalog'
  want_warning "a {bind} that resolves nowhere is reported" 'is not in state.data'
  want_warning "a stack block's own field is checked" 'is not a block field'
  # The failure a rename creates: a colour name that was valid yesterday and
  # meaningless today, with nothing on either side saying so. A ui tone falls back
  # to the default and a markup colour resolves an undefined custom property —
  # both render, both look deliberate.
  want_warning "a tone the board no longer has is reported" 'is not a tone this board has'
  want_warning "a markup colour the board no longer has is reported" 'is not a colour this board has'
  want_warning "state inside a stack block is checked" 'state.txt is not declared'

  # The other half of a useful detector: it must stay quiet about correct writes.
  # A field's `bind` is a WRITE path, so it is not required to exist yet — warn on
  # that and the noise trains the reader to ignore the whole channel.
  case "$WARN" in
    *"written.later"*) echo "  FAIL a field's write path was reported as a broken bind"; FAILED=1 ;;
    *) echo "  ok   a field's write path is not mistaken for a broken bind" ;;
  esac
  # A key that EXISTS and holds null is initialised-empty, not missing. Asking
  # "is the value non-nil" instead of "was the key found" flagged a real gallery
  # node the first time this ran, and a checker that calls correct state a
  # mistake is the noise that teaches people to skip stderr entirely.
  case "$WARN" in
    *"data.initialised"*) echo "  FAIL a null value was reported as a missing bind path"; FAILED=1 ;;
    *) echo "  ok   a bind to an initialised-null value is not a broken bind" ;;
  esac
  # And the palette check must stay quiet about the names that ARE right, or it
  # is just noise with a colour in it.
  case "$WARN" in
    *'"agent" is not a tone'*|*'"mark" is not a colour'*)
      echo "  FAIL a valid palette name was reported as unknown"; FAILED=1 ;;
    *) echo "  ok   valid tone and colour names are not reported" ;;
  esac

  # And the stamp itself: the server owns `version`, so a document that names the
  # wrong one is corrected rather than stored. This is the one check here that
  # REALLY writes, because the stamping happens server-side and the response
  # carries only ok/updatedAt.
  #
  # It is a no-op apart from `version`, so it changes no tab, which means no
  # `touched` markers and no journal entry. The one trace it leaves is
  # `lastEditedBy: "smoke-test"`, which the header shows until the next write —
  # named to be self-explanatory rather than to look like a participant.
  STAMPED=$(node -e "
    const fs=require('fs'), http=require('http');
    const d=JSON.parse(fs.readFileSync(process.env.STATE,'utf8'));
    d.version = 2; d.__base = d.updatedAt; d.__by = 'smoke-test';
    const body=JSON.stringify(d);
    const req=http.request('$BASE/aboard.json',{method:'POST',headers:{'Content-Type':'application/json','Content-Length':Buffer.byteLength(body)}},(res)=>{
      let out='';res.on('data',(c)=>out+=c).on('end',()=>{
        if (res.statusCode===409) { process.stdout.write('conflict'); return; }
        // Read it back off disk: the response carries only ok/updatedAt.
        process.stdout.write(String(JSON.parse(fs.readFileSync(process.env.STATE,'utf8')).version));
      });
    });
    req.on('error',()=>process.stdout.write('error'));
    req.end(body);
  ")
  if [ "$STAMPED" = "conflict" ]; then
    echo "  note version stamping not asserted — the board changed mid-check (that is CAS working)"
  else
    check "a version the board does not write is stamped, not stored" "$STAMPED" "3"
  fi
else
  echo "  skip no ./aboard binary — run make build"
fi

echo "== journal, watch, predicates, uploads =="
jfield() {
  curl -s "$BASE/journal?limit=5" | node -e "
    let d='';process.stdin.on('data',c=>d+=c).on('end',()=>{
      try { const e=JSON.parse(d).entries||[]; process.stdout.write(String(e.length)); }
      catch { process.stdout.write('?'); }
    })"
}

# Make a real write FIRST, then assert the journal saw it.
#
# Counting entries alone passed on history: a journal file left over from any
# earlier session satisfied ">= 1" without this suite ever exercising the write
# path, so the check could not have failed even with journalling switched off.
# So: rename a tab through `aboard apply`, revert the name, and assert the newest
# entry is the revert — right author, right tab.
#
# It leaves one mark behind, and deliberately: an agent write stamps `touched`,
# and only the human's dismiss clears it (that is guarantee 2). A test cannot
# undo that without pretending to be the human, which is worse than a dot.
if [ -x ./aboard ]; then
  PROBE=$(node -e "
    const fs=require('fs');
    const d=JSON.parse(fs.readFileSync(process.env.STATE,'utf8'));
    const t=(d.tabs||[])[0];
    process.stdout.write(t ? t.id + '\u0009' + t.name : '');
  ")
  PROBE_ID=$(printf '%s' "$PROBE" | cut -f1)
  PROBE_NAME=$(printf '%s' "$PROBE" | cut -f2-)
  if [ -n "$PROBE_ID" ]; then
    apply_name() {
      DOC=$(mktemp)
      node -e "
        const fs=require('fs');
        const d=JSON.parse(fs.readFileSync(process.env.STATE,'utf8'));
        const t=(d.tabs||[]).find((x)=>x.id===process.argv[1]);
        t.name=process.argv[2];
        fs.writeFileSync(process.argv[3], JSON.stringify(d));
      " "$PROBE_ID" "$1" "$DOC"
      ab apply --by "smoke" < "$DOC" >/dev/null 2>&1
      rc=$?
      rm -f "$DOC"
      return $rc
    }
    ok1=$(apply_name "$PROBE_NAME (smoke)" && echo 0 || echo 1)
    ok2=$(apply_name "$PROBE_NAME" && echo 0 || echo 1)
    check "a rename applies and reverts through the running board" "$ok1$ok2" "00"
    check "the name really went back" \
      "$(node -e "
        const fs=require('fs');
        const d=JSON.parse(fs.readFileSync(process.env.STATE,'utf8'));
        const t=(d.tabs||[]).find((x)=>x.id===process.argv[1]);
        process.stdout.write(t && t.name===process.argv[2] ? 'yes' : 'no');
      " "$PROBE_ID" "$PROBE_NAME")" "yes"
    check "the journal recorded the write that just happened" \
      "$(curl -s "$BASE/journal?limit=5" | node -e "
        let d='';process.stdin.on('data',c=>d+=c).on('end',()=>{
          let e=[]; try { e=JSON.parse(d).entries||[]; } catch {}
          const last=e[e.length-1]||{};
          const ok = last.by==='smoke' && (last.tabs||[]).includes(process.argv[1]);
          process.stdout.write(ok ? 'yes' : 'no');
        })" "$PROBE_ID")" "yes"
  else
    echo "  skip the board has no tabs to rename"
  fi
else
  echo "  skip no ./aboard binary — run make build"
fi

check_ge "the journal records writes" "$(jfield)" "1"

# A valid predicate must be ACCEPTED and then time out; an invalid one is refused
# up front. Those are different failures and the distinction is the whole point.
check "a valid predicate is accepted and times out cleanly" \
  "$(curl -s "$BASE/wait?for=change&timeout=1" | node -e "
    let d='';process.stdin.on('data',c=>d+=c).on('end',()=>{
      try { process.stdout.write(JSON.parse(d).event); } catch { process.stdout.write('?'); }
    })")" "timeout"

check "a malformed node predicate is refused" \
  "$(curl -s -o /dev/null -w '%{http_code}' "$BASE/wait?for=node%20bb58&timeout=2")" "400"

# Uploads: sniffed from the bytes, so a lie about the type does not get through.
PNG=$(mktemp /tmp/aboard-smoke-XXXXXX.png)
node -e "
  const zlib=require('zlib'),fs=require('fs');
  const chunk=(t,d)=>{const c=Buffer.concat([Buffer.from(t),d]);
    const len=Buffer.alloc(4);len.writeUInt32BE(d.length);
    const crc=Buffer.alloc(4);crc.writeUInt32BE(zlib.crc32?zlib.crc32(c):require('zlib').crc32(c));
    return Buffer.concat([len,c,crc]);};
  const ihdr=Buffer.alloc(13); ihdr.writeUInt32BE(4,0); ihdr.writeUInt32BE(4,4);
  ihdr[8]=8; ihdr[9]=2;
  const raw=Buffer.concat(Array.from({length:4},()=>Buffer.concat([Buffer.from([0]),Buffer.alloc(12,120)])));
  fs.writeFileSync(process.argv[1], Buffer.concat([
    Buffer.from([0x89,0x50,0x4e,0x47,0x0d,0x0a,0x1a,0x0a]),
    chunk('IHDR',ihdr), chunk('IDAT',zlib.deflateSync(raw)), chunk('IEND',Buffer.alloc(0))]));
" "$PNG" 2>/dev/null || printf '\x89PNG\r\n\x1a\n rest-not-a-real-png' > "$PNG"

UP=$(curl -s -X POST --data-binary @"$PNG" "$BASE/upload?name=smoke%20probe" | node -e "
  let d='';process.stdin.on('data',c=>d+=c).on('end',()=>{
    try { process.stdout.write(JSON.parse(d).url || ''); } catch { process.stdout.write(''); }
  })")
case "$UP" in
  uploads/*) echo "  ok   an image upload lands in uploads/ ($UP)" ;;
  *) echo "  FAIL an image upload lands in uploads/ (got '$UP')"; FAILED=1 ;;
esac
check "the uploaded file serves as an image" \
  "$(curl -s -o /dev/null -w '%{content_type}' "$BASE/$UP")" "image/png"
check "a non-image upload is refused" \
  "$(curl -s -o /dev/null -w '%{http_code}' -X POST --data-binary 'plain text, not an image' "$BASE/upload")" "415"
check "an encoded traversal under uploads/ is refused" \
  "$(curl -s --path-as-is -o /dev/null -w '%{http_code}' "$BASE/uploads/%2e%2e%2faboard.json")" "404"
rm -f "$PNG"
# $UP is a URL path ("uploads/<file>"); on disk it lives under .aboard/, which is
# where the board keeps its content now. Removing the URL relative to the cwd
# quietly removed nothing and left the probe image behind on every run.
[ -n "$UP" ] && rm -f "$PROJECT/.aboard/$UP"

echo "== an action strip records an intent instead of acting =="
ACT_TAB=$(node -e "
  const b=JSON.parse(require('fs').readFileSync(process.env.STATE,'utf8'));
  const t=(b.tabs||[]).find((t)=>t.state && Array.isArray(t.state.actions) && t.state.actions.length);
  process.stdout.write(t ? t.id : '');
")
if [ -n "$ACT_TAB" ]; then
  dom=$(timeout 90 "$BROWSER" --headless --no-sandbox --disable-gpu --disable-dev-shm-usage \
    --virtual-time-budget=9000 --dump-dom "$BASE/?nosse=1&tab=$ACT_TAB" 2>/dev/null)
  printf '%s' "$dom" | node -e "
    let d=''; process.stdin.on('data',c=>d+=c).on('end',()=>{
      const strip = (d.match(/<div class=\"action-strip\">[\s\S]*?<\/div>/) || [''])[0];
      const buttons = (strip.match(/class=\"icon-btn action-btn\"/g) || []).length;
      const ok = buttons > 0;
      console.log((ok ? '  ok   ' : '  FAIL ') + 'action strip renders (' + buttons + ' buttons)');
      if (!ok) process.exitCode = 1;
    });
  "
else
  echo "  skip no tab declares state.actions"
fi

echo "== the stream tells a page which code it should be running =="
# This one frame is what makes a restart self-healing: the browser reconnects by
# itself and compares. If it stops arriving FIRST, every open page silently keeps
# running stale JavaScript again.
FRAME=$(timeout 2 curl -sN "$BASE/events" 2>/dev/null | grep -m1 '"ui"' || true)
printf '%s' "$FRAME" | node -e "
  let d=''; process.stdin.on('data',c=>d+=c).on('end',()=>{
    let sig = null;
    try { sig = JSON.parse(d.replace(/^data: /, '')).ui; } catch {}
    const ok = sig && typeof sig.html === 'string' && sig.html.length > 8
      && typeof sig.css === 'string' && typeof sig.js === 'string'
      && sig.html !== sig.css;
    console.log((ok ? '  ok   ' : '  FAIL ') + 'first SSE frame carries the ui signature'
      + (sig ? ' (html=' + sig.html.slice(0, 8) + '… css=' + sig.css.slice(0, 8) + '…)' : ' (none)'));
    if (!ok) process.exitCode = 1;
  });
"

echo "== a read-only kanban offers nothing to edit =="
# The point of readOnly is that the affordances are GONE, not disabled: a card you
# can drag which then snaps back reads as a bug. Asserted on rendered DOM, and on
# a tab that actually sets the flag.
RO_TAB=$(node -e "
  const b=JSON.parse(require('fs').readFileSync(process.env.STATE,'utf8'));
  const t=(b.tabs||[]).find((t)=>t.type==='kanban' && t.state && t.state.readOnly===true);
  process.stdout.write(t ? t.id : '');
")
if [ -n "$RO_TAB" ]; then
  dom=$(timeout 90 "$BROWSER" --headless --no-sandbox --disable-gpu --disable-dev-shm-usage \
    --virtual-time-budget=9000 --dump-dom "$BASE/?nosse=1&tab=$RO_TAB" 2>/dev/null)
  printf '%s' "$dom" | node -e "
    let d=''; process.stdin.on('data',c=>d+=c).on('end',()=>{
      // Structural facts over container extraction. Two earlier attempts failed
      // for opposite reasons: counting closing divs over-captured into the page's
      // own script (where the help table has the same ▲▼ glyphs), and matching
      // <section> under-captured, because a COLUMN is a section too. These
      // strings only ever come from the mounted renderer, and only one tab is
      // mounted at a time.
      const count = (re) => (d.match(re) || []).length;
      const cards = count(/class=\"card\"/g);
      const chips = count(/class=\"id-chip\"/g);
      const badge = /ro-badge/.test(d) && /data-readonly=\"yes\"/.test(d);
      const noDrag = !/draggable=\"true\"/.test(d);
      const noEdit = !/contenteditable=\"true\"/i.test(d);
      // In read-only mode a card's foot holds its id chip and nothing else, so
      // one chip per card means no controls were rendered. An EMPTY board is a
      // legitimate state — the queue is empty when nothing is outstanding — and
      // requiring cards > 0 made 'nothing to do' look like a failure.
      const bare = chips === cards;
      const ok = badge && noDrag && noEdit && bare;
      console.log((ok ? '  ok   ' : '  FAIL ') + 'read-only kanban: badge=' + badge +
        ' no-drag=' + noDrag + ' no-edit=' + noEdit + ' cards=' + cards + ' chips=' + chips +
        (cards === 0 ? ' (empty queue, which is a valid state)' : ''));
      if (!ok) process.exitCode = 1;
    });
  "
else
  echo "  skip no read-only kanban on this board"
fi

echo "== notify channel: wait, waiters, poke =="
# The button's whole claim is "someone is listening", so the test is end to end:
# a real -wait process must appear in /waiters, be released by POST /poke, and
# exit 0 having printed the event.
#
# TWO THINGS THIS SECTION DOES TO A LIVE BOARD, deliberately and unavoidably:
# it pokes, which RELEASES any session genuinely waiting on this board; and it
# therefore cannot assume the board starts idle. So it measures from whatever
# was already waiting and says so, rather than failing because a real agent was
# doing its job. (Both mistakes were made writing it: a parked session made the
# idle assertions fail, and running the suite released it.)

BASE_WAIT=$(waiting_count)
if [ "$BASE_WAIT" = "0" ]; then
  check "poke with nobody waiting releases 0" "$(curl -s -X POST "$BASE/poke" -d '{"by":"smoke"}' | node -e "
    let d='';process.stdin.on('data',c=>d+=c).on('end',()=>process.stdout.write(String(JSON.parse(d).released)))")" "0"
else
  echo "  note $BASE_WAIT session(s) already waiting — this test will release them"
fi

check "an unknown predicate is refused, not silently awaited" \
  "$(curl -s -o /dev/null -w '%{http_code}' "$BASE/wait?for=form%2015%20answered&timeout=2")" "400"

if [ -x ./aboard ]; then
  WANT=$((BASE_WAIT + 1))
  WAITLOG=$(mktemp)
  ( ab wait --by smoke-waiter --timeout 30s --note "checking the notify channel" > "$WAITLOG" 2>&1; echo "exit=$?" >> "$WAITLOG" ) &
  WAITPID=$!

  # Registration is a round trip, so poll rather than sleep a guessed amount.
  n=0
  while [ "$n" -lt 40 ]; do
    seen=$(waiting_count)
    [ "$seen" = "$WANT" ] && break
    n=$((n + 1)); sleep 0.1
  done
  check "a -wait session registers" "$seen" "$WANT"

  # The button shows a countdown, so the waiter has to declare a deadline and a
  # reason — a label with neither is the mystery this was built to avoid.
  check "it declares its deadline and reason" "$(curl -s "$BASE/waiters" | node -e "
    let d='';process.stdin.on('data',c=>d+=c).on('end',()=>{
      const w = (JSON.parse(d).waiters || []).find((x) => x.by === 'smoke-waiter') || {};
      const left = new Date(w.until).getTime() - Date.now();
      process.stdout.write(String(w.timeout === 30 && left > 0 && left <= 30000 && w.note === 'checking the notify channel'));
    })")" "true"

  check_ge "poke releases it" "$(curl -s -X POST "$BASE/poke" -d '{"by":"smoke","note":"go"}' | node -e "
    let d='';process.stdin.on('data',c=>d+=c).on('end',()=>process.stdout.write(String(JSON.parse(d).released)))")" "1"

  wait $WAITPID 2>/dev/null || true
  check "the released session printed the poke" \
    "$(grep -c '"event":"poke"' "$WAITLOG" || true)" "1"
  check "the released session exited 0" "$(sed -n 's/^exit=//p' "$WAITLOG")" "0"
  check "a poke releases everyone, so nobody is left waiting" "$(waiting_count)" "0"
  rm -f "$WAITLOG"
else
  echo "  skip no ./aboard binary — run make build for the `wait` end-to-end check"
fi

# The button itself: assert the rendered control, not the source text that builds
# it (grepping --dump-dom for an id matches this page's own script too).
dom=$(timeout 90 "$BROWSER" --headless --no-sandbox --disable-gpu --disable-dev-shm-usage \
  --virtual-time-budget=9000 --dump-dom "$BASE/?nosse=1" 2>/dev/null)
printf '%s' "$dom" | node -e "
  let d=''; process.stdin.on('data',c=>d+=c).on('end',()=>{
    const btn = (d.match(/<button[^>]*id=\"poke\"[^>]*>[\s\S]*?<\/button>/) || [])[0] || '';
    const disabled = /disabled/.test(btn) && /data-live=\"no\"/.test(btn);
    const labelled = /no session waiting/.test(btn);
    const ok = btn && disabled && labelled;
    console.log((ok ? '  ok   ' : '  FAIL ') + 'notify button renders, disabled with nobody waiting');
    if (!ok) process.exitCode = 1;
  });
"
if [ "$FAILED" = "1" ]; then exit 1; fi

echo "done"
