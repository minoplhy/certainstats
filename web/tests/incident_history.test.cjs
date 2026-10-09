const { test } = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');

// Small DOM harness keeps the active Go-panel tests independent of React's
// dependencies. Browser rendering is also checked separately against Chromium.
class Element {
  constructor(tag) { this.tagName = tag; this.children = []; this.dataset = {}; this.attrs = {}; this.handlers = {}; this.hidden = false; this.disabled = false; this.value = ''; this._text = ''; this.className = ''; this.classList = { toggle: (name, active) => { const classes = new Set(this.className.split(' ').filter(Boolean)); active ? classes.add(name) : classes.delete(name); this.className = [...classes].join(' '); } }; }
  set textContent(value) { this._text = String(value); this.children = []; }
  get textContent() { return this._text + this.children.map(n => n.textContent).join(''); }
  append(...nodes) { for (const n of nodes) { n.parentElement = this; this.children.push(n); } }
  replaceChildren(...nodes) { for (const n of this.children) n.parentElement = null; this.children = []; this._text = ''; this.append(...nodes); }
  setAttribute(key, value) { this.attrs[key] = String(value); }
  addEventListener(event, callback) { (this.handlers[event] ||= []).push(callback); }
  dispatch(event) { for (const callback of this.handlers[event] || []) callback({ target: this }); }
  matches(selector) {
    if (selector === 'details') return this.tagName === 'details';
    if (selector === '[hidden]') return this.hidden;
    const match = selector.match(/^\[data-([a-z-]+)(?:="([^"]*)")?\]$/);
    if (!match) return false;
    const key = match[1].replace(/-([a-z])/g, (_, c) => c.toUpperCase());
    return key in this.dataset && (match[2] == null || this.dataset[key] === match[2]);
  }
  querySelectorAll(selector) { const out = []; for (const child of this.children) { if (child.matches(selector)) out.push(child); out.push(...child.querySelectorAll(selector)); } return out; }
  querySelector(selector) { return this.querySelectorAll(selector)[0] || null; }
  closest(selector) { for (let n = this; n; n = n.parentElement) if (n.matches(selector)) return n; return null; }
  get isConnected() { return this._connected || !!this.parentElement?.isConnected; }
  get options() { return this.children; }
}
function harness(fetch, nodeMode = false) {
  const root = new Element('div'); root._connected = true;
  const roles = ['search', 'node-label', 'node', 'list', 'message', 'count', 'page', 'prev', 'next', 'limit', 'refresh'];
  const controls = {};
  for (const role of roles) { const n = new Element(role === 'node' ? 'select' : 'div'); n.dataset['history' + role.replace(/(^|-)([a-z])/g, (_, p, c) => c.toUpperCase())] = ''; root.append(n); controls[role] = n; }
  for (const status of ['all', 'firing', 'resolved', 'closed']) { const n = new Element('button'); n.dataset.historyStatus = status; root.append(n); controls[status] = n; }
  const parent = new Element('details'); parent.open = !nodeMode; if (nodeMode) parent.append(root);
  const callbacks = {}; const timers = [];
  const document = { hidden: false, createElement: tag => new Element(tag), getElementById: () => null, addEventListener: (event, cb) => { callbacks[event] = cb; }, removeEventListener() {} };
  const window = { addEventListener: (event, cb) => { callbacks[event] = cb; }, confirm: () => true };
  const context = { window, document, fetch, AbortController, URLSearchParams, Date, Number, Intl, CSS: { escape: s => s }, Option: class extends Element { constructor(text, value) { super('option'); this.textContent = text; this.value = value; } }, setInterval: cb => { callbacks.interval = cb; return 1; }, clearInterval() {}, setTimeout: cb => { timers.push(cb); return timers.length; }, clearTimeout() {} };
  vm.runInNewContext(fs.readFileSync('web/static/js/incident_history.js', 'utf8'), context);
  const view = window.CertainStatsIncidentHistory.mount(root, { panelPath: '/panel', nodeMode });
  return { root, controls, parent, view, callbacks, timers, format: window.CertainStatsIncidentHistory };
}
const flush = () => new Promise(resolve => setImmediate(resolve));
const response = data => ({ ok: true, json: async () => data });
const incident = overrides => ({ history_id: 'incident', agent_id: 'node', agent_nickname: 'Node', alert_nickname: 'CPU high', triggered_at: '2026-10-01T00:00:00Z', trigger: { type: 'cpu_usage', operator: '>', threshold: 90 }, trigger_value: 95, notified_status: 'failed', firing_delivery: 'failed', ...overrides });
const page = rows => ({ data: rows, page: 1, limit: 25, total: rows.length, total_pages: rows.length ? 1 : 0 });

test('incident filters query the server and obsolete responses cannot replace newer rows', async () => {
  const requests = []; const resolvers = [];
  const h = harness((url, options) => { if (url.includes('/summary')) return Promise.resolve(response({ firing: 0, nodes: [] })); requests.push({ url, options }); return new Promise(resolve => resolvers.push(resolve)); });
  h.controls.resolved.dispatch('click');
  assert.equal(requests[0].options.signal.aborted, true);
  assert.match(requests[1].url, /status=resolved/);
  resolvers[1](response(page([incident({ alert_nickname: 'Newest', resolved_at: '2026-10-01T01:00:00Z' })]))); await flush();
  resolvers[0](response(page([incident({ alert_nickname: 'Stale' })]))); await flush();
  assert.match(h.controls.list.textContent, /Newest/); assert.doesNotMatch(h.controls.list.textContent, /Stale/);
  h.controls.search.value = 'beyond first page'; h.controls.search.dispatch('input'); h.timers.at(-1)();
  assert.match(requests[2].url, /q=beyond\+first\+page/); assert.match(requests[2].url, /page=1/); h.view.destroy();
});

test('HTTP failures retain records and show a retry instead of an empty success state', async () => {
  let fail = false;
  const h = harness(async url => url.includes('/summary') ? response({ firing: 0, nodes: [] }) : fail ? { ok: false, json: async () => ({ message: 'Unavailable' }) } : response(page([incident()])));
  await flush(); fail = true; await h.view.refresh();
  assert.match(h.controls.list.textContent, /CPU high/); assert.match(h.controls.message.textContent, /Could not load incident history.*Unavailable/);
  assert.equal(h.controls.message.children[1].textContent, 'Retry'); h.view.destroy();
});

test('node history loads lazily, resets on navigation, and cancels old node requests', async () => {
  const requests = []; const resolvers = [];
  const h = harness((url, options) => { requests.push({ url, options }); return new Promise(resolve => resolvers.push(resolve)); }, true);
  h.view.setNode('node-a'); assert.equal(requests.length, 0);
  h.parent.open = true; h.parent.dispatch('toggle'); assert.match(requests[0].url, /agent_id=node-a/);
  h.view.setNode('node-b'); assert.equal(requests[0].options.signal.aborted, true); assert.equal(h.parent.open, false);
  h.parent.open = true; h.parent.dispatch('toggle'); assert.match(requests[1].url, /agent_id=node-b/);
  resolvers[1](response(page([incident({ agent_id: 'node-b', agent_nickname: 'Second node' })]))); await flush();
  resolvers[0](response(page([incident({ agent_nickname: 'First node' })]))); await flush();
  assert.match(h.controls.list.textContent, /Second node/); assert.doesNotMatch(h.controls.list.textContent, /First node/); h.view.destroy();
});

test('expanded events preserve hostile strings as text and retries use the queued response', async () => {
  let retried = false;
  const hostile = '<img src=x onerror="alert(1)">';
  const h = harness(async (url, options) => {
    if (options.method === 'POST') { retried = true; return response({ status: 'queued', event_id: 'retry' }); }
    if (url.includes('/summary')) return response({ firing: 1, nodes: [] });
    if (url.includes('/events?')) return response({ data: [{ event_id: 'failed-event', kind: 'notification', phase: 'firing', status: retried ? 'pending' : 'failed', created_at: '2026-10-01T00:00:01Z', error_message: hostile, retry_available: !retried }], total: 1 });
    return response(page([incident({ alert_nickname: hostile, firing_delivery: retried ? 'queued' : 'failed' })]));
  });
  await flush(); const details = h.controls.list.children[0]; details.open = true; details.dispatch('toggle'); await flush();
  const events = details.querySelector('[data-events-id="incident"]');
  assert.match(events.textContent, /<img src=x/); assert.equal(events.querySelectorAll('img').length, 0);
  const content = events.children[0].children[1]; const button = content.children.at(-1); button.dispatch('click'); await flush(); await flush();
  assert.equal(retried, true); assert.match(h.controls.list.textContent, /Alert: Queued/); h.view.destroy();
});

test('duration formatting handles live, long, negative, and invalid intervals', () => {
  const h = harness(async () => response(page([])), true);
  assert.equal(h.format.duration('2026-10-01T00:00:00Z', '2026-10-03T01:02:00Z'), '2d 1h');
  assert.equal(h.format.duration('2026-10-01T00:00:01Z', '2026-10-01T00:00:00Z'), '0s');
  assert.equal(h.format.duration('invalid'), 'Unknown'); assert.equal(h.format.metric('agent_down', 0), 'Offline'); h.view.destroy();
});
