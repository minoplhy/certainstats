const {test} = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');

const ROLE_SELECTOR = /^\[data-network-(.+)\]$/;
const ROLES = [
  'add', 'search', 'node', 'node-filter', 'group', 'group-filter', 'group-message', 'state', 'refresh',
  'time-picker', 'chart-count', 'chart-message', 'latency', 'loss', 'legend',
  'count', 'message', 'list', 'prev', 'next', 'limit',
  'group-state', 'group-enabled', 'dialog', 'form', 'form-title', 'form-subtitle', 'form-message', 'protocol-hint', 'port', 'server',
  'agent-options', 'agent-count', 'agent-actions', 'agent-hint', 'agents-all', 'agents-none', 'enabled-pill', 'submit'
];
const HERO_ROLES = ['count', 'headline', 'enabled', 'paused', 'response', 'loss', 'loss-target', 'sync'];
const PROTOCOLS = ['', 'icmp', 'tcp', 'http', 'dns'];

class Element {
  constructor(tag = 'div') {
    this.tagName = tag;
    this.children = [];
    this.handlers = {};
    this.attrs = {};
    this.style = {};
    this.value = '';
    this._text = '';
    this.dataset = {};
    this.open = false;
    this.isConnected = true;
    const classes = new Set();
    this.classList = {
      add: name => classes.add(name),
      remove: name => classes.delete(name),
      contains: name => classes.has(name),
      toggle: (name, force) => (force ?? !classes.has(name)) ? classes.add(name) : classes.delete(name)
    };
  }
  set textContent(value) {
    this._text = String(value);
    this.children = [];
  }
  get textContent() {
    return this._text + this.children.map(node => node.textContent).join('');
  }
  append(...nodes) { this.children.push(...nodes); }
  replaceChildren(...nodes) {
    this.children = nodes;
    this._text = '';
  }
  get firstChild() { return this.children[0]; }
  setAttribute(key, value) { this.attrs[key] = value; }
  // Finds descendants by form field name, enough for input[name=...] selectors.
  querySelectorAll(selector) {
    const name = (selector.match(/name=([\w-]+)/) || [])[1];
    const found = [];
    const walk = node => {
      for (const child of node.children || []) {
        if (name && child.name === name) found.push(child);
        walk(child);
      }
    };
    walk(this);
    return found;
  }
  showModal() { this.open = true; }
  close() { this.open = false; }
  reset() {}
  focus() {}
  addEventListener(type, handler) { (this.handlers[type] ||= []).push(handler); }
  dispatch(type) {
    for (const handler of this.handlers[type] || []) handler({target: this, preventDefault() {}, stopPropagation() {}});
  }
  closest() { return new Element(); }
}

const monitor = (id = 'one', overrides = {}) => ({
  monitor_id: id,
  target: 'https://example.com',
  protocol: 'http',
  agent_id: 'node',
  agent_name: 'Node ' + id,
  interval_seconds: 60,
  enabled: true,
  sync: {desired_generation: 1, acknowledged_generation: 1},
  ...overrides
});
const response = data => ({ok: true, json: async () => data});
const points = [{timestamp: 1000, response_avg_ms: 12, loss_pct: 0}];
const flush = async () => {
  for (let i = 0; i < 4; i++) await new Promise(resolve => setImmediate(resolve));
};
const isHistory = url => url.includes('/history?');
const historyRequests = requests => requests.filter(request => isHistory(request.url));
// History URLs are /network-monitors/{id}/history?...; one monitor per request.
const historyMonitor = url => decodeURIComponent(url.match(/network-monitors\/([^/]+)\/history\?/)[1]);

function harness(fetch, nodeMode = false, withHero = false) {
  const controls = {};
  for (const role of ROLES) controls[role] = new Element();
  controls.limit.value = '25';
  controls.form.elements = {};
  for (const name of ['protocol', 'target', 'port', 'interval_seconds', 'dns_server', 'enabled']) controls.form.elements[name] = new Element('input');
  const protocolOptions = PROTOCOLS.map(value => {
    const option = new Element('button');
    option.dataset.networkProtocolOption = value;
    return option;
  });
  const formProtocolOptions = PROTOCOLS.slice(1).map(value => {
    const option = new Element('button');
    option.dataset.networkFormProtocol = value;
    return option;
  });
  const parent = new Element('details');
  parent.open = false;
  const root = {
    querySelector: selector => controls[selector.match(ROLE_SELECTOR)[1]],
    querySelectorAll: selector => ({
      '[data-network-protocol-option]': protocolOptions,
      '[data-network-form-protocol]': formProtocolOptions
    })[selector] || [],
    closest: selector => selector === 'details' ? parent : null
  };
  const heroControls = {};
  for (const role of HERO_ROLES) heroControls[role] = new Element();
  const heroRoot = {querySelector: selector => heroControls[selector.match(/^\[data-network-hero-(.+)\]$/)[1]]};

  const timers = [], chartCalls = [], sparklines = [], callbacks = {};
  const telemetry = {
    subscribeNetwork(listener) {
      callbacks.network = listener;
      return () => { callbacks.unsubscribed = true; };
    },
    getNetworkFeedState() { return callbacks.feed; },
    initCustomTimePicker(element, options) {
      let state = {hours: 6, customRange: null};
      callbacks.picker = {
        getState: () => state,
        setState: value => { state = value; },
        destroy() { callbacks.pickerDestroyed = true; },
        apply(value) {
          state = value;
          options.onApply(value);
        }
      };
      return callbacks.picker;
    }
  };
  const chart = {
    drawSparkline(canvas, points, options) { sparklines.push({canvas, points, options}); },
    renderMultiChart(id, options) {
      const instance = {
        id, options, updates: [], destroyed: false,
        updateSeries(...args) { this.updates.push(args); },
        destroy() { this.destroyed = true; }
      };
      chartCalls.push(instance);
      return instance;
    }
  };
  const window = {addEventListener() {}, CertainStatsTelemetry: telemetry, CertainStatsChart: chart};
  const document = {
    hidden: false,
    createElement: tag => new Element(tag),
    createTextNode: text => ({textContent: text}),
    addEventListener() {}
  };
  vm.runInNewContext(fs.readFileSync('web/static/js/network_monitors.js', 'utf8'), {
    window, document, fetch, AbortController, URLSearchParams, Date, Number, Math, Map, Set, Promise,
    setInterval: handler => { callbacks.interval = handler; return 1; },
    clearInterval() {},
    setTimeout: handler => { timers.push(handler); return timers.length; },
    clearTimeout() {}
  });
  const view = window.CertainStatsNetworkMonitors.mount(root, {panelPath: '/panel', nodeMode, hero: withHero ? heroRoot : undefined});
  const protocol = value => protocolOptions[PROTOCOLS.indexOf(value)].dispatch('click');
  return {controls, heroControls, protocolOptions, formProtocolOptions, protocol, parent, view, timers, chartCalls, sparklines, callbacks};
}

function backend(requests, items = [monitor()], total = items.length) {
  return async (url, opts) => {
    requests.push({url, opts});
    if (url.endsWith('/agents')) return response([]);
    if (url.includes('/network-monitors/targets?')) return response({items: [...new Set(items.map(item => item.target))].sort()});
    if (isHistory(url)) return response({monitor_id: historyMonitor(url), points, end: 2000});
    return response({items, total});
  };
}

test('graphs load by default with ten candidates, independent of list pagination', async () => {
  const requests = [], items = Array.from({length: 10}, (_, i) => monitor('node' + i));
  const h = harness(backend(requests, items, 12));
  await flush();
  assert.equal(h.chartCalls.length, 2);
  assert.equal(h.chartCalls[0].options.seriesList.length, 10);
  assert.match(h.controls['chart-count'].textContent, /Showing 10 of 12.*Narrow/);
  assert.ok(requests.some(r => r.url.includes('page=1&limit=10')));

  const histories = historyRequests(requests).length;
  h.controls.next.dispatch('click');
  await flush();
  assert.ok(requests.some(r => r.url.includes('page=2&limit=25')));
  assert.equal(historyRequests(requests).length, histories);
  h.view.destroy();
});

test('each graph series is its own history request, never a comma list', async () => {
  const requests = [], items = Array.from({length: 3}, (_, i) => monitor('node' + i));
  const h = harness(backend(requests, items));
  await flush();
  const urls = historyRequests(requests).map(r => r.url);
  assert.deepEqual(urls.map(historyMonitor).sort(), ['node0', 'node1', 'node2']);
  for (const url of urls) {
    assert.doesNotMatch(url, /monitor_ids|,|%2C/i);
    assert.match(url, /^\/panel\/api\/network-monitors\/[^/]+\/history\?hours=6$/);
  }
  h.view.destroy();
});

test('legend toggles both charts, preserves stable colors and zoom across refresh', async () => {
  const requests = [], h = harness(backend(requests));
  await flush();
  const color = h.chartCalls[0].options.seriesList[0].color;
  const toggle = () => h.controls.legend.children[0].children[1].dispatch('click');

  toggle();
  assert.equal(h.chartCalls[0].updates.at(-1)[0].length, 0);
  assert.equal(h.chartCalls[1].updates.at(-1)[0].length, 0);
  assert.match(h.controls['chart-message'].textContent, /All series are hidden/);

  h.callbacks.interval();
  await flush();
  assert.equal(h.chartCalls.length, 2);
  assert.equal(h.chartCalls[0].updates.at(-1)[0].length, 0);

  toggle();
  assert.equal(h.chartCalls[0].updates.at(-1)[0][0].color, color);
  h.chartCalls[0].options.onZoom(1100, 1900);
  await flush();
  h.callbacks.interval();
  await flush();
  assert.match(historyRequests(requests).at(-1).url, /start=1100&end=1900/);
  assert.equal(h.chartCalls[1].updates.at(-1)[4].start, 1100);
  h.view.destroy();
});

test('shared filters reset toggles and preserve zoom, and search is debounced', async () => {
  const requests = [], h = harness(backend(requests));
  await flush();
  h.controls.legend.children[0].children[1].dispatch('click');
  h.chartCalls[0].options.onZoom(1100, 1900);
  await flush();

  const before = requests.length;
  h.controls.search.value = 'EXAMPLE.COM';
  h.controls.search.dispatch('input');
  assert.equal(requests.length, before);
  h.timers.at(-1)();
  await flush();
  assert.ok(requests.some(r => r.url.includes('target=EXAMPLE.COM')));
  assert.match(historyRequests(requests).at(-1).url, /start=1100&end=1900/);
  assert.equal(h.chartCalls[0].updates.at(-1)[0].length, 1);

  h.protocol('tcp');
  await flush();
  assert.ok(requests.filter(r => r.url.includes('target=EXAMPLE.COM&protocol=tcp')).length >= 2);
  assert.equal(h.protocolOptions[2].attrs['aria-pressed'], 'true');
  assert.equal(h.protocolOptions[0].attrs['aria-pressed'], 'false');
  h.view.destroy();
});

test('stale history cannot replace a newer filter result; failures keep plots and offer retry', async () => {
  let fail = false, hold = false, resolveOld;
  const requests = [], fetch = backend(requests);
  const h = harness(async (url, opts) => {
    if (isHistory(url) && hold) {
      hold = false;
      return new Promise(resolve => { resolveOld = resolve; });
    }
    if (isHistory(url) && fail) return {ok: false, json: async () => ({error: 'Unavailable'})};
    return fetch(url, opts);
  });
  await flush();

  hold = true;
  h.controls.refresh.dispatch('click');
  await flush();
  h.protocol('http');
  await flush();
  const updates = h.chartCalls[0].updates.length;
  resolveOld(response({monitor_id: 'one', points: [], end: 0}));
  await flush();
  assert.equal(h.chartCalls[0].updates.length, updates);

  fail = true;
  h.controls.refresh.dispatch('click');
  await flush();
  assert.match(h.controls['chart-message'].textContent, /Unavailable.*Retry/);
  assert.equal(h.chartCalls[0].destroyed, false);

  fail = false;
  h.controls['chart-message'].children[1].dispatch('click');
  await flush();
  assert.equal(h.controls['chart-message'].textContent, '');
  h.view.destroy();
});

test('one failed series still plots the others and offers retry', async () => {
  const requests = [], items = [monitor('good'), monitor('bad')];
  const fetch = backend(requests, items);
  const h = harness(async (url, opts) => {
    if (isHistory(url) && historyMonitor(url) === 'bad') return {ok: false, json: async () => ({error: 'Unavailable'})};
    return fetch(url, opts);
  });
  await flush();
  assert.equal(h.chartCalls[0].options.seriesList.length, 1);
  assert.match(h.controls['chart-message'].textContent, /Unavailable.*Retry/);
  h.view.destroy();
});

test('node view is lazy and old node responses cannot repopulate a new node', async () => {
  const requests = [], resolvers = [];
  const h = harness((url, opts) => {
    requests.push({url, opts});
    return new Promise(resolve => resolvers.push(resolve));
  }, true);

  h.view.setNode('first');
  assert.equal(requests.length, 0);
  h.parent.open = true;
  h.parent.dispatch('toggle');
  assert.equal(requests.length, 2);

  h.view.setNode('second');
  assert.equal(requests[0].opts.signal.aborted, true);
  assert.equal(requests[1].opts.signal.aborted, true);
  resolvers.forEach(resolve => resolve(response({items: [monitor()], total: 1})));
  await flush();
  assert.equal(h.controls.list.textContent, '');
  assert.equal(h.controls.legend.textContent, '');

  h.parent.open = true;
  h.parent.dispatch('toggle');
  assert.ok(requests.at(-1).url.includes('agent_id=second'));
  h.view.destroy();
});

test('empty matches and missing samples have explicit graph states', async () => {
  const empty = harness(backend([], []));
  await flush();
  assert.match(empty.controls['chart-message'].textContent, /No monitors match/);
  assert.equal(empty.chartCalls.length, 2);
  empty.view.destroy();

  const h = harness(async url => {
    if (url.endsWith('/agents')) return response([]);
    if (isHistory(url)) return response({monitor_id: 'one', points: [{timestamp: 1000, response_avg_ms: null, loss_pct: null}], end: 2000});
    return response({items: [monitor()], total: 1});
  });
  await flush();
  assert.match(h.controls['chart-message'].textContent, /No history/);
  assert.equal(h.chartCalls[0].options.seriesList[0].data[0].value, null);
  h.view.destroy();
});

test('picker reset clears zoom and cleanup destroys picker', async () => {
  const requests = [], h = harness(backend(requests));
  await flush();
  h.callbacks.picker.apply({hours: 12, customRange: {start: 1000, end: 2000}});
  await flush();
  h.chartCalls[0].options.onZoom(1200, 1800);
  await flush();
  assert.equal(h.callbacks.picker.getState().customRange.start, 1200);

  h.callbacks.picker.apply({hours: 12, customRange: null});
  await flush();
  assert.match(requests.at(-1).url, /hours=12/);
  h.callbacks.picker.apply({hours: 24, customRange: null});
  await flush();
  h.chartCalls[0].options.onZoom(1200, 1800);
  await flush();
  h.callbacks.picker.apply({hours: 24, customRange: null});
  await flush();
  assert.match(requests.at(-1).url, /hours=24/);

  h.view.destroy();
  assert.equal(h.callbacks.pickerDestroyed, true);
});

test('live readings update rows without history reads and unsubscribe on destruction', async () => {
  const requests = [], h = harness(backend(requests));
  await flush();
  const before = requests.length;
  h.callbacks.network({
    one: {
      agent_id: 'node', enabled: true, state: 'active',
      sync: {desired_generation: 1, acknowledged_generation: 1},
      latest: {
        success_count: 1, response_avg_ms: 25, response_avg_1h_ms: 20,
        response_min_1h_ms: 10, response_max_1h_ms: 30, loss_1h_pct: 0, last_probe_at: 1000
      }
    }
  });
  assert.match(h.controls.list.textContent, /25 ms/);
  assert.equal(requests.length, before);

  h.callbacks.feed = {connected: true, lastPulse: Date.now(), generation: 0};
  h.callbacks.interval();
  await flush();
  const newRequests = requests.slice(before);
  assert.ok(newRequests.some(r => isHistory(r.url)));
  assert.equal(newRequests.filter(r => r.url.includes('limit=25')).length, 0);

  h.view.destroy();
  assert.equal(h.callbacks.unsubscribed, true);
});

test('inactive network view aborts work, pauses reads, and retains picker state', async () => {
  const requests = [], h = harness(backend(requests));
  await flush();
  h.chartCalls[0].options.onZoom(1100, 1900);
  await flush();

  h.view.deactivate();
  const before = requests.length;
  h.callbacks.interval();
  h.view.refresh();
  await flush();
  assert.equal(requests.length, before);

  h.view.activate();
  await flush();
  assert.match(historyRequests(requests).at(-1).url, /start=1100&end=1900/);
  h.view.destroy();
});

test('live membership changes reconcile rows once and stale feeds poll as fallback', async () => {
  const requests = [], h = harness(backend(requests));
  await flush();
  const listReads = () => requests.filter(r => r.url.includes('limit=25')).length;

  h.callbacks.network({one: {enabled: true}});
  const before = requests.length;
  h.callbacks.network({});
  await flush();
  assert.equal(requests.slice(before).filter(r => r.url.includes('limit=25')).length, 1);
  assert.equal(historyRequests(requests.slice(before)).length, 0);

  h.callbacks.network({});
  await flush();
  assert.equal(requests.length, before + 2);

  h.callbacks.feed = {connected: true, lastPulse: Date.now() - 46000, generation: 0};
  h.callbacks.interval();
  await flush();
  assert.equal(listReads(), 3);
  h.view.destroy();
});

const groupsOf = h => h.controls.list.children.filter(child => child.className === 'network-target-group');
const groupTarget = group => group.children[0].children[0].children[1].textContent;
const rowsOf = h => h.controls.list.children.flatMap(child => child.className === 'network-target-group' ? child.children : [child])
  .filter(child => child.className === 'network-row');
const runTimers = async h => {
  while (h.timers.length) h.timers.shift()();
  await flush();
};

test('group dropdown filters rows and graphs exactly, resets pagination, and retains empty selections', async () => {
  const requests = [];
  let available = ['https://example.com', 'https://example.com/health', 'off-page.example'];
  const items = [monitor('root'), monitor('health', {target: 'https://example.com/health'})];
  const fetch = async (url, opts) => {
    requests.push({url, opts});
    if (url.endsWith('/agents')) return response([]);
    if (url.includes('/network-monitors/targets?')) return response({items: available});
    if (isHistory(url)) return response({monitor_id: historyMonitor(url), points, end: 2000});
    const params = new URLSearchParams(url.split('?')[1]);
    const selected = params.get('target_exact');
    const matches = items.filter(item => !selected || item.target === selected);
    return response({items: matches, total: selected ? matches.length : 26});
  };
  const h = harness(fetch);
  await flush();
  assert.deepEqual(h.controls.group.children.map(option => option.value), ['', ...available]);
  h.controls.next.dispatch('click');
  await flush();
  h.callbacks.picker.apply({hours: 12, customRange: {start: 100, end: 200}});
  await flush();
  h.controls.group.value = 'https://example.com/health';
  h.controls.group.dispatch('change');
  await flush();
  assert.equal(rowsOf(h).length, 1);
  assert.match(rowsOf(h)[0].textContent, /Node health/);
  assert.equal(h.controls.count.textContent, '1–1 of 1');
  assert.equal(h.chartCalls[0].updates.at(-1)[0].length, 1);
  assert.match(h.chartCalls[0].updates.at(-1)[0][0].label, /Node health/);
  assert.deepEqual(h.callbacks.picker.getState(), {hours: 12, customRange: {start: 100, end: 200}});
  const listRequest = requests.filter(r => r.url.includes('limit=25')).at(-1);
  assert.equal(new URLSearchParams(listRequest.url.split('?')[1]).get('page'), '1');
  for (const request of requests.filter(r => r.url.includes('/targets?'))) {
    const params = new URLSearchParams(request.url.split('?')[1]);
    assert.equal(params.has('target'), false);
    assert.equal(params.has('target_exact'), false);
  }

  h.controls.group.value = 'off-page.example';
  available = ['https://example.com'];
  h.controls.group.dispatch('change');
  await flush();
  assert.equal(h.controls.group.value, 'off-page.example');
  assert.ok(h.controls.group.children.some(option => option.value === 'off-page.example'));
  assert.match(h.controls.list.textContent, /No network monitors match/);
  h.controls.group.value = '';
  h.controls.group.dispatch('change');
  await flush();
  assert.equal(rowsOf(h).length, 2);
  assert.equal(new URLSearchParams(requests.filter(r => r.url.includes('limit=25')).at(-1).url.split('?')[1]).has('target_exact'), false);
  h.view.destroy();
});

test('dropdown ignores stale responses and retains options on failure with retry', async () => {
  let resolveOld, fail = false, calls = 0;
  const requests = [];
  const fetch = backend(requests);
  const h = harness((url, opts) => {
    if (!url.includes('/targets?')) return fetch(url, opts);
    requests.push({url, opts});
    if (++calls === 1) return new Promise(resolve => { resolveOld = resolve; });
    if (fail) return Promise.resolve({ok: false, json: async () => ({error: 'Groups unavailable'})});
    return Promise.resolve(response({items: ['current.example']}));
  });
  await flush();
  h.controls.node.value = 'node';
  h.controls.node.dispatch('change');
  await flush();
  resolveOld(response({items: ['old.example']}));
  await flush();
  assert.deepEqual(h.controls.group.children.map(option => option.value), ['', 'current.example']);
  assert.equal(requests.find(r => r.url.includes('/targets?')).opts.signal.aborted, true);
  const last = requests.filter(r => r.url.includes('/targets?')).at(-1);
  assert.equal(new URLSearchParams(last.url.split('?')[1]).get('agent_id'), 'node');
  fail = true;
  h.controls.refresh.dispatch('click');
  await flush();
  assert.match(h.controls['group-message'].textContent, /Groups unavailable.*Retry/);
  assert.deepEqual(h.controls.group.children.map(option => option.value), ['', 'current.example']);
  fail = false;
  h.controls['group-message'].children[1].dispatch('click');
  await flush();
  assert.equal(h.controls['group-message'].textContent, '');
  h.view.destroy();
});

test('groups collapse independently and retain choices across live updates, filters, pagination and navigation', async () => {
  const items = [monitor('a'), monitor('b', {target: 'other.example'})];
  const h = harness(backend([], items, 26));
  await flush();
  assert.ok(groupsOf(h).every(group => group.open));
  assert.match(groupsOf(h)[0].children[0].textContent, /1 monitor/);
  groupsOf(h)[0].open = false;
  groupsOf(h)[0].dispatch('toggle');
  rowsOf(h)[1].open = true;
  rowsOf(h)[1].dispatch('toggle');
  h.callbacks.network({a: {...items[0], state: 'active'}, b: {...items[1], state: 'active'}});
  assert.equal(groupsOf(h)[0].open, false);
  assert.equal(groupsOf(h)[1].open, true);
  assert.equal(rowsOf(h)[1].open, true);
  h.controls.next.dispatch('click');
  await flush();
  assert.equal(groupsOf(h)[0].open, false);
  h.protocol('http');
  await flush();
  assert.equal(groupsOf(h)[0].open, false);
  h.view.deactivate();
  h.view.activate();
  await flush();
  assert.equal(groupsOf(h)[0].open, false);
  groupsOf(h)[0].open = true;
  groupsOf(h)[0].dispatch('toggle');
  h.controls.refresh.dispatch('click');
  await flush();
  assert.equal(groupsOf(h)[0].open, true);
  h.view.destroy();
});

test('target groups collect interleaved checks in first-seen order, keeping each configuration visible', async () => {
  const items = [
    monitor('a', {target: 'example.com', protocol: 'tcp', port: 443}),
    monitor('b', {target: 'https://example.com/health'}),
    monitor('c', {target: 'example.com', protocol: 'tcp', port: 80, interval_seconds: 300, enabled: false}),
    monitor('d', {target: 'https://example.com/login'}),
    monitor('e', {target: 'example.com', protocol: 'dns', dns_server: '1.1.1.1'}),
    monitor('f', {target: 'example.com', protocol: 'dns', dns_server: '8.8.8.8'})
  ];
  const h = harness(backend([], items));
  await flush();
  const groups = groupsOf(h);
  assert.deepEqual(groups.map(groupTarget), [
    'example.com', 'https://example.com/health', 'https://example.com/login'
  ]);
  assert.deepEqual(groups[0].children.slice(1).map(row => row.children[0].children[0].textContent), [
    'Node aTCP :443 · every 60s', 'Node cTCP :80 · every 300s',
    'Node eDNS · every 60s · DNS 1.1.1.1', 'Node fDNS · every 60s · DNS 8.8.8.8'
  ]);
  assert.match(groups[0].children[2].textContent, /Resume/);
  assert.equal(rowsOf(h).length, items.length);
  assert.equal(h.controls.count.textContent, '1–6 of 6');
  h.view.destroy();
});

test('target grouping follows filtered page results without loading other pages', async () => {
  const requests = [];
  const fetch = async (url, opts) => {
    requests.push({url, opts});
    if (url.endsWith('/agents')) return response([]);
    if (isHistory(url)) return response({monitor_id: historyMonitor(url), points, end: 2000});
    const params = new URLSearchParams(url.split('?')[1]);
    const filtered = params.get('protocol') === 'tcp';
    const items = filtered ? [monitor('tcp', {protocol: 'tcp', port: 443})]
      : params.get('page') === '2' ? [monitor('second')]
      : [monitor('first'), monitor('other', {target: 'other.example'})];
    return response({items, total: filtered ? 1 : 26});
  };
  const h = harness(fetch);
  await flush();
  assert.equal(groupsOf(h).length, 2);
  assert.equal(h.controls.count.textContent, '1–25 of 26');
  h.controls.next.dispatch('click');
  await flush();
  assert.equal(groupsOf(h).length, 1);
  assert.equal(rowsOf(h).length, 1);
  assert.match(rowsOf(h)[0].textContent, /Node second/);
  assert.equal(h.controls.count.textContent, '26–26 of 26');
  h.protocol('tcp');
  await flush();
  assert.equal(rowsOf(h).length, 1);
  assert.match(rowsOf(h)[0].textContent, /Node tcp/);
  assert.equal(h.controls.count.textContent, '1–1 of 1');
  assert.ok(!requests.some(r => r.url.includes('page=3')));
  h.view.destroy();
});

test('node-detail monitor lists remain flat and show their targets', async () => {
  const h = harness(backend([], [monitor('a'), monitor('b')]), true);
  h.view.setNode('node');
  h.parent.open = true;
  h.parent.dispatch('toggle');
  await flush();
  assert.equal(groupsOf(h).length, 0);
  assert.equal(rowsOf(h).length, 2);
  assert.match(rowsOf(h)[0].textContent, /https:\/\/example.com/);
  h.view.destroy();
});

test('monitors render as list rows with every column and expandable details', async () => {
  const requests = [];
  const items = [
    monitor('ok', {state: 'active', protocol: 'tcp', port: 443, latest: {success_count: 4, attempt_count: 4, response_avg_ms: 12.5, loss_pct: 0, loss_1h_pct: 0, last_probe_at: Date.now() - 120000}}),
    monitor('down', {state: 'active', latest: {success_count: 0, attempt_count: 4, loss_pct: 100, loss_1h_pct: 100, last_probe_at: Date.now()}}),
    monitor('lag', {state: 'active', sync: {desired_generation: 2, acknowledged_generation: 1, error: 'Agent synchronization failed'}})
  ];
  const h = harness(backend(requests, items));
  await flush();
  assert.equal(h.controls.list.children[0].className, 'network-list-head');
  const rows = rowsOf(h);
  assert.equal(rows.length, 3);
  const text = rows[0].textContent;
  assert.equal(groupTarget(groupsOf(h)[0]), 'https://example.com');
  for (const part of ['TCP :443 · every 60s', 'Node ok', '12.5 ms', '0.00%', '2m ago', 'Healthy', 'Edit', 'Pause', 'Delete']) {
    assert.ok(text.includes(part), part + ' missing from ' + text);
  }
  assert.match(rows[1].textContent, /Failing/);
  assert.match(rows[2].textContent, /Sync pending/);

  rows[0].open = true;
  rows[0].dispatch('toggle');
  h.callbacks.network({ok: {...items[0], latest: {...items[0].latest, response_avg_ms: 30}}});
  assert.equal(rowsOf(h)[0].open, true, 'expanded row survives a live re-render');
  assert.match(rowsOf(h)[0].textContent, /30 ms/);

  const actions = rowsOf(h)[0].children[0].children.at(-1);
  actions.children[1].dispatch('click');
  await flush();
  const patch = requests.find(r => r.opts?.method === 'PATCH');
  assert.equal(patch.url, '/panel/api/network-monitors/ok');
  assert.equal(patch.opts.body, JSON.stringify({enabled: false}));
  h.view.destroy();
});

test('row sparklines are one 24h history request per monitor, cached across live re-renders', async () => {
  const requests = [], items = [monitor('a'), monitor('b')];
  const h = harness(backend(requests, items));
  await flush();
  const before = historyRequests(requests).length;
  await runTimers(h);
  const sparkUrls = historyRequests(requests).slice(before).map(r => r.url);
  assert.deepEqual(sparkUrls.sort(), [
    '/panel/api/network-monitors/a/history?hours=24',
    '/panel/api/network-monitors/b/history?hours=24'
  ]);
  assert.equal(h.sparklines.length, 2);
  assert.equal(h.sparklines[0].options.hours, 24);

  h.callbacks.network({a: {enabled: true, state: 'active'}, b: {enabled: true, state: 'active'}});
  await runTimers(h);
  assert.equal(historyRequests(requests).length, before + 2, 'live re-render reuses cached sparklines');
  assert.equal(h.sparklines.length, 4, 're-rendered rows are redrawn from the cache');
  h.view.destroy();
});

test('pending sparkline requests stop when the view is deactivated', async () => {
  const requests = [], h = harness(backend(requests, [monitor('a')]));
  await flush();
  const before = historyRequests(requests).length;
  h.view.deactivate();
  await runTimers(h);
  assert.equal(historyRequests(requests).length, before);
  h.view.destroy();
});

test('hero summarizes the live snapshot', async () => {
  const h = harness(backend([], [monitor('ok')]), false, true);
  await flush();
  assert.equal(h.heroControls.headline.textContent, 'Checking monitors…');
  assert.equal(h.heroControls.enabled.textContent, '–');

  h.callbacks.network({
    ok: {enabled: true, state: 'active', sync: {desired_generation: 1, acknowledged_generation: 1}, latest: {success_count: 3, response_avg_ms: 10, loss_pct: 0, loss_1h_pct: 0}},
    slow: {enabled: true, state: 'active', sync: {desired_generation: 1, acknowledged_generation: 1}, latest: {success_count: 3, response_avg_ms: 30, loss_pct: 0, loss_1h_pct: 4}},
    paused: {enabled: false, state: 'paused', sync: {desired_generation: 2, acknowledged_generation: 1}}
  });
  assert.equal(h.heroControls.headline.textContent, 'All 2 monitors are healthy.');
  assert.equal(h.heroControls.count.textContent, '3 monitors');
  assert.equal(h.heroControls.enabled.textContent, '2');
  assert.equal(h.heroControls.paused.textContent, '1 paused');
  assert.equal(h.heroControls.response.textContent, '20 ms');
  assert.equal(h.heroControls.loss.textContent, '4.00%');
  assert.equal(h.heroControls.sync.textContent, '1');

  h.callbacks.network({
    ok: {enabled: true, state: 'active', sync: {desired_generation: 1, acknowledged_generation: 1}, latest: {success_count: 0, loss_pct: 100, loss_1h_pct: 100}},
    gone: {enabled: true, state: 'offline', sync: {desired_generation: 1, acknowledged_generation: 1}},
    fine: {enabled: true, state: 'active', sync: {desired_generation: 1, acknowledged_generation: 1}, latest: {success_count: 1, response_avg_ms: 5, loss_pct: 0, loss_1h_pct: 0}}
  });
  assert.equal(h.heroControls.headline.textContent, '2 monitors need attention. The other 1 is healthy.');
  assert.equal(h.heroControls.headline.children[0].className, 'is-bad');
  assert.equal(h.heroControls['loss-target'].textContent, 'https://example.com');

  h.callbacks.network({});
  assert.equal(h.heroControls.headline.textContent, 'Add a monitor to start probing.');
  h.view.destroy();
});

test('an empty account shows the add-monitor empty state, filters show a no-match line', async () => {
  const h = harness(backend([], []));
  await flush();
  assert.match(h.controls.list.textContent, /No monitors yet/);
  h.controls.search.value = 'nothing';
  h.controls.search.dispatch('input');
  h.timers.at(-1)();
  await flush();
  assert.match(h.controls.list.textContent, /No network monitors match these filters/);
  h.view.destroy();
});

test('add dialog: protocol buttons, agent pills with reasons, select all, client checks and POST body', async () => {
  const requests = [];
  const capable = {agent_id: 'a1', nickname: 'Tokyo', agent_type: 'beszel', extensions: {runtime: {agent_version: '0.21.0', capabilities: {
    'network_monitor.configure': {state: 'supported'}, 'network_monitor.icmp': {state: 'supported'}, 'network_monitor.tcp': {state: 'supported'}
  }}}};
  const old = {agent_id: 'a2', nickname: 'Paris', agent_type: 'beszel', extensions: {runtime: {agent_version: '0.19.0', capabilities: {
    'network_monitor.configure': {state: 'unsupported', message: 'Beszel 0.20.0 or newer is required'}
  }}}};
  const fetch = backend(requests);
  const h = harness(async (url, opts) => {
    if (url.endsWith('/agents')) {
      requests.push({url, opts});
      return response([capable, old]);
    }
    if (opts?.method === 'POST') {
      requests.push({url, opts});
      return response({items: []});
    }
    return fetch(url, opts);
  });
  await flush();

  h.controls.add.dispatch('click');
  await flush();
  assert.equal(h.controls.dialog.open, true);
  const pills = h.controls['agent-options'].children;
  assert.equal(pills.length, 2);
  assert.match(pills[1].textContent, /Beszel 0.20.0 or newer is required/);
  assert.ok(pills[1].className.includes('is-disabled'));
  assert.equal(h.controls['agent-count'].textContent, '0 selected');

  h.controls['agents-all'].dispatch('click');
  assert.equal(h.controls['agent-count'].textContent, '1 selected');
  assert.equal(h.controls.submit.textContent, 'Add monitor');

  h.formProtocolOptions[1].dispatch('click');
  assert.equal(h.controls.form.elements.protocol.value, 'tcp');
  assert.equal(h.controls.port.hidden, false);
  assert.match(h.controls['protocol-hint'].textContent, /TCP connection/);

  h.controls.form.elements.interval_seconds.value = '60';
  h.controls.form.dispatch('submit');
  await flush();
  assert.match(h.controls['form-message'].textContent, /Enter a target/);
  assert.equal(requests.filter(r => r.opts?.method === 'POST').length, 0);

  h.controls.form.elements.target.value = 'db.example.com';
  h.controls.form.elements.port.value = '5432';
  h.controls.form.elements.enabled.checked = true;
  h.controls.form.dispatch('submit');
  await flush();
  const post = requests.find(r => r.opts?.method === 'POST');
  assert.deepEqual(JSON.parse(post.opts.body), {
    target: 'db.example.com', protocol: 'tcp', interval_seconds: 60, enabled: true, port: 5432, agent_ids: ['a1']
  });
  assert.equal(h.controls.dialog.open, false);
  h.view.destroy();
});

const groupTestAgent = (id, supported = true) => ({agent_id: id, nickname: id, agent_type: 'beszel', extensions: {runtime: {capabilities: Object.fromEntries(
  ['configure', 'icmp', 'tcp', 'http', 'dns', 'custom_dns_server'].map(feature => ['network_monitor.' + feature, {state: supported ? 'supported' : 'unsupported'}])
)}}});

test('group editor loads every prober, changes membership atomically, preserves pause states and renames the filter', async () => {
  const requests = [];
  let target = 'https://example.com';
  let members = [monitor('one', {agent_id: 'node'}), monitor('two', {agent_id: 'second', enabled: false})];
  const base = backend(requests, [members[0]], 30);
  const h = harness(async (url, opts) => {
    if (url.endsWith('/agents')) return response(['node', 'second', 'third', 'old'].map(id => groupTestAgent(id, id !== 'old')));
    if (url.includes('/network-monitors/group?')) { requests.push({url, opts}); return response({items: members, revision: 'revision-1'}); }
    if (url.endsWith('/network-monitors/group') && opts?.method === 'PATCH') {
      requests.push({url, opts});
      target = JSON.parse(opts.body).target;
      members = [monitor('two-new', {target, agent_id: 'second', enabled: false}), monitor('three', {target, agent_id: 'third'})];
      return response({items: members, revision: 'revision-2'});
    }
    if (url.includes('/targets?')) return response({items: [target]});
    return base(url, opts);
  });
  await flush();
  h.controls.group.value = target;
  h.controls.node.value = 'node';
  h.controls.group.dispatch('change');
  await flush();
  const group = groupsOf(h)[0];
  group.open = false;
  group.dispatch('toggle');
  group.children[0].children[1].dispatch('click');
  await flush();
  assert.equal(h.controls.dialog.open, true);
  assert.equal(h.controls['form-title'].textContent, 'Edit target group');
  assert.match(h.controls['form-subtitle'].textContent, /all 2 probers/);
  assert.equal(h.controls['enabled-pill'].hidden, true);
  assert.equal(h.controls['group-state'].hidden, false);
  const inputs = h.controls['agent-options'].querySelectorAll('input[name=agent_ids]');
  assert.deepEqual(inputs.filter(i => i.checked).map(i => i.value), ['node', 'second']);
  assert.equal(inputs.find(i => i.value === 'old').disabled, true);
  inputs.find(i => i.value === 'node').checked = false;
  inputs.find(i => i.value === 'third').checked = true;
  inputs.find(i => i.value === 'third').dispatch('change');
  assert.match(h.controls['agent-hint'].textContent, /1 retained, 1 added, 1 removed/);
  h.controls.form.elements.target.value = 'https://renamed.example.com';
  h.controls.form.elements.interval_seconds.value = '300';
  h.controls.form.dispatch('submit');
  await flush();
  const patch = requests.find(r => r.opts?.method === 'PATCH');
  assert.deepEqual(JSON.parse(patch.opts.body), {target: 'https://renamed.example.com', protocol: 'http', interval_seconds: 300,
    original_target: 'https://example.com', revision: 'revision-1', agent_ids: ['second', 'third']});
  assert.equal(h.controls.dialog.open, false);
  assert.equal(h.controls.group.value, target);
  assert.ok(requests.some(r => r.url.includes('target_exact=https%3A%2F%2Frenamed.example.com')));
  assert.equal(requests.find(r => r.url.includes('/group?')).url, '/panel/api/network-monitors/group?target=https%3A%2F%2Fexample.com');
  h.view.destroy();
});

test('node-detail Edit opens the whole group and offers reload after a stale save', async () => {
  const requests = [], item = monitor('one');
  const base = backend(requests, [item]);
  const h = harness(async (url, opts) => {
    if (url.endsWith('/agents')) return response(['node', 'second'].map(id => groupTestAgent(id)));
    if (url.includes('/group?')) return response({items: [item, monitor('two', {agent_id: 'second'})], revision: 'old'});
    if (opts?.method === 'PATCH') { requests.push({url, opts}); return {ok: false, status: 409, json: async () => ({error: 'Group changed; reload before saving'})}; }
    return base(url, opts);
  }, true);
  h.view.setNode('node');
  h.parent.open = true;
  h.parent.dispatch('toggle');
  await flush();
  const actions = rowsOf(h)[0].children[0].children.at(-1);
  actions.children.find(child => child.textContent === 'Edit').dispatch('click');
  await flush();
  assert.equal(h.controls['agent-options'].querySelectorAll('input[name=agent_ids]').length, 2);
  h.controls['group-enabled'].value = 'false';
  h.controls.form.dispatch('submit');
  await flush();
  assert.equal(JSON.parse(requests.find(r => r.opts?.method === 'PATCH').opts.body).enabled, false);
  assert.equal(h.controls.dialog.open, true);
  assert.match(h.controls['form-message'].textContent, /reload/i);
  h.controls['form-message'].children[0].dispatch('click');
  await flush();
  assert.equal(h.controls['form-message'].textContent, '');
  h.view.destroy();
});

test('adding a prober inherits existing target settings and prevents duplicate agent selection', async () => {
  const requests = [], item = monitor('one', {target: 'db.example.com', protocol: 'tcp', port: 5432, interval_seconds: 300});
  const base = backend(requests, [item]);
  const h = harness(async (url, opts) => {
    if (url.endsWith('/agents')) return response(['node', 'second'].map(id => groupTestAgent(id)));
    if (url.endsWith('/network-monitors/targets')) return response({items: ['db.example.com']});
    if (url.includes('/group?')) return response({items: [item], revision: 'r'});
    if (opts?.method === 'POST') {requests.push({url, opts}); return response({items: []});}
    return base(url, opts);
  });
  await flush();
  h.controls.add.dispatch('click');
  await flush();
  h.controls.form.elements.target.value = 'DB.EXAMPLE.COM';
  h.controls.form.elements.target.dispatch('change');
  await flush();
  assert.equal(h.controls.form.elements.protocol.value, 'tcp');
  assert.equal(h.controls.form.elements.port.value, 5432);
  assert.equal(h.controls.form.elements.interval_seconds.value, 300);
  assert.equal(h.controls.form.elements.interval_seconds.disabled, true);
  assert.ok(h.formProtocolOptions.every(option => option.disabled));
  let inputs = h.controls['agent-options'].querySelectorAll('input[name=agent_ids]');
  assert.equal(inputs[0].disabled, true);
  assert.equal(inputs[0].checked, false);
  h.controls['agents-all'].dispatch('click');
  h.controls.form.elements.enabled.checked = true;
  h.controls.form.dispatch('submit');
  await flush();
  assert.deepEqual(JSON.parse(requests.find(r => r.opts?.method === 'POST').opts.body).agent_ids, ['second']);
  h.controls.add.dispatch('click');
  await flush();
  h.controls.form.elements.target.value = 'new.example.com';
  h.controls.form.elements.target.dispatch('change');
  await flush();
  assert.equal(h.controls.form.elements.interval_seconds.disabled, false);
  h.view.destroy();
});

test('target inheritance ignores stale responses and clearing the target releases a pending lookup', async () => {
  const requests = [], deferred = [];
  const base = backend(requests, []);
  const h = harness(async (url, opts) => {
    if (url.endsWith('/agents')) return response([groupTestAgent('node')]);
    if (url.endsWith('/network-monitors/targets')) return new Promise(resolve => deferred.push(resolve));
    return base(url, opts);
  });
  await flush();
  h.controls.add.dispatch('click');
  await flush();
  h.controls.form.elements.target.value = 'old.example.com';
  h.controls.form.elements.target.dispatch('change');
  h.controls.form.elements.target.value = 'new.example.com';
  h.controls.form.elements.target.dispatch('change');
  deferred[1](response({items: []}));
  await flush();
  deferred[0](response({items: ['old.example.com']}));
  await flush();
  assert.equal(h.controls.form.elements.target.value, 'new.example.com');
  assert.equal(h.controls.form.elements.interval_seconds.disabled, false);
  h.controls.form.elements.target.value = 'pending.example.com';
  h.controls.form.elements.target.dispatch('change');
  h.controls.form.elements.target.value = '';
  h.controls.form.elements.target.dispatch('change');
  h.controls.form.dispatch('submit');
  await flush();
  assert.match(h.controls['form-message'].textContent, /Enter a target/);
  deferred[2](response({items: []}));
  await flush();
  h.view.destroy();
});
