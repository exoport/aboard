#!/usr/bin/env node
// Minimal zero-dependency board server.
//
//   GET  /             -> board.html
//   GET  /board.json   -> current state
//   POST /board.json   -> write state to disk (compare-and-set, see below)
//   GET  /events       -> SSE stream; pings whenever board.json changes on disk
//   GET  /<file>       -> static assets (views/, vendor/, assets/, css)
//
// board.json is the single source of truth, living on disk in the repo. The
// browser is one editor of it; Claude Code is another.
//
// Writes are compare-and-set: a POST carries the `updatedAt` it was based on,
// and is refused with 409 if the file moved on since. That keeps an agent write
// from silently clobbering an edit made in the browser a moment earlier.

const http = require('http');
const fs = require('fs');
const path = require('path');

const DIR = __dirname;
const STATE_FILE = path.join(DIR, 'board.json');
const PAGE_FILE = path.join(DIR, 'board.html');
const PORT = Number(process.env.PORT) || 4173;

const SERVE_DIRS = ['views', 'vendor', 'assets', 'test'];
const SERVE_FILES = ['app.css', 'board.html'];
const MIME = {
  '.html': 'text/html; charset=utf-8',
  '.js': 'text/javascript; charset=utf-8',
  '.mjs': 'text/javascript; charset=utf-8',
  '.css': 'text/css; charset=utf-8',
  '.json': 'application/json',
  '.svg': 'image/svg+xml',
  '.png': 'image/png',
  '.jpg': 'image/jpeg',
  '.jpeg': 'image/jpeg',
  '.webp': 'image/webp',
  '.gif': 'image/gif',
  '.ico': 'image/x-icon',
};

const clients = new Set();

// Set when a write came in over HTTP, so we can tell that browser "this echo is
// yours, ignore it" while still notifying every other listener.
let lastWriteOrigin = null;

function reply(res, code, type, body) {
  res.writeHead(code, { 'Content-Type': type, 'Cache-Control': 'no-store' });
  res.end(body);
}

function serveFile(res, file, type) {
  fs.readFile(file, (err, buf) => {
    if (err) return reply(res, 404, 'text/plain; charset=utf-8', 'not found');
    reply(res, 200, type, buf);
  });
}

function serveStatic(res, pathname) {
  const rel = decodeURIComponent(pathname.replace(/^\/+/, ''));
  const full = path.resolve(DIR, rel);
  // Traversal guard: the resolved path must stay inside the project, and only
  // the whitelisted subdirectories and top-level files are reachable. Anything
  // else — including this server's own source — is refused.
  const inServeDir = SERVE_DIRS.some((d) => full.startsWith(path.join(DIR, d) + path.sep));
  const isServeFile = SERVE_FILES.includes(path.relative(DIR, full));
  if (!inServeDir && !isServeFile) {
    return reply(res, 403, 'text/plain; charset=utf-8', 'forbidden');
  }
  const type = MIME[path.extname(full).toLowerCase()];
  if (!type) return reply(res, 415, 'text/plain; charset=utf-8', 'unsupported type');
  serveFile(res, full, type);
}

function handlePost(req, res) {
  let body = '';
  let aborted = false;
  req.on('data', (chunk) => {
    body += chunk;
    if (body.length > 8_000_000) {
      aborted = true;
      req.destroy();
    }
  });
  req.on('end', () => {
    if (aborted) return;
    let incoming;
    try {
      incoming = JSON.parse(body);
    } catch {
      return reply(res, 400, 'application/json', '{"error":"invalid json"}');
    }
    if (!incoming || !Array.isArray(incoming.nodes)) {
      return reply(res, 400, 'application/json', '{"error":"expected a nodes array"}');
    }

    const origin = typeof incoming.__origin === 'string' ? incoming.__origin : 'browser';
    const base = typeof incoming.__base === 'string' ? incoming.__base : null;
    delete incoming.__origin;
    delete incoming.__base;

    // Compare-and-set against what is on disk right now.
    let current = null;
    try {
      current = JSON.parse(fs.readFileSync(STATE_FILE, 'utf8'));
    } catch {
      current = null;
    }
    if (base && current && current.updatedAt && current.updatedAt !== base) {
      return reply(
        res,
        409,
        'application/json',
        JSON.stringify({ error: 'conflict', live: current.updatedAt })
      );
    }

    incoming.updatedAt = new Date().toISOString();
    incoming.lastEditedBy = 'human';
    fs.writeFile(STATE_FILE, JSON.stringify(incoming, null, 2) + '\n', (err) => {
      if (err) return reply(res, 500, 'application/json', '{"error":"write failed"}');
      lastWriteOrigin = origin;
      reply(res, 200, 'application/json', JSON.stringify({ ok: true, updatedAt: incoming.updatedAt }));
    });
  });
}

const server = http.createServer((req, res) => {
  const url = new URL(req.url, `http://localhost:${PORT}`);

  if (req.method === 'GET' && (url.pathname === '/' || url.pathname === '/board.html')) {
    return serveFile(res, PAGE_FILE, 'text/html; charset=utf-8');
  }
  if (req.method === 'GET' && url.pathname === '/board.json') {
    return serveFile(res, STATE_FILE, 'application/json');
  }
  if (req.method === 'POST' && url.pathname === '/board.json') {
    return handlePost(req, res);
  }
  if (req.method === 'GET' && url.pathname === '/events') {
    res.writeHead(200, {
      'Content-Type': 'text/event-stream',
      'Cache-Control': 'no-cache',
      Connection: 'keep-alive',
    });
    res.write('retry: 1000\n\n');
    clients.add(res);
    req.on('close', () => clients.delete(res));
    return;
  }
  if (req.method === 'GET') return serveStatic(res, url.pathname);

  reply(res, 404, 'text/plain; charset=utf-8', 'not found');
});

function broadcast() {
  const payload = JSON.stringify({ origin: lastWriteOrigin });
  lastWriteOrigin = null;
  for (const res of clients) {
    try {
      res.write(`data: ${payload}\n\n`);
    } catch {
      clients.delete(res);
    }
  }
}

// Watch the directory rather than the file: editors and tools often replace a
// file instead of truncating it, which silently kills a single-file watcher.
let debounce = null;
fs.watch(DIR, (_event, filename) => {
  if (filename !== 'board.json') return;
  clearTimeout(debounce);
  debounce = setTimeout(broadcast, 120);
});

server.listen(PORT, '127.0.0.1', () => {
  console.log(`board  ->  http://localhost:${PORT}`);
  console.log(`state  ->  ${STATE_FILE}`);
  console.log('In VS Code: Ctrl/Cmd+Shift+P -> "Simple Browser: Show" -> paste the URL above.');
});
