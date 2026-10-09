const {test} = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');

const ROLE_SELECTOR = /^\[data-network-(.+)\]$/;
const ROLES = [
  'add', 'search', 'node', 'node-filter', 'state', 'refresh',
  'time-picker', 'chart-count', 'chart-message', 'latency', 'loss', 'legend',
  'count', 'message', 'list', 'prev', 'next', 'limit',
  'dialog', 'form', 'form-title', 'form-subtitle', 'form-message', 'protocol-hint', 'port', 'server',
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
  assert.equal(requests.length, before + 1);

  h.callbacks.feed = {connected: true, lastPulse: Date.now() - 46000, generation: 0};
  h.callbacks.interval();
  await flush();
  assert.equal(listReads(), 3);
  h.view.destroy();
});

const rowsOf = h => h.controls.list.children.filter(child => child.className === 'network-row');
const runTimers = async h => {
  while (h.timers.length) h.timers.shift()();
  await flush();
};

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
  for (const part of ['https://example.com', 'TCP :443 · every 60s', 'Node ok', '12.5 ms', '0.00%', '2m ago', 'Healthy', 'Edit', 'Pause', 'Delete']) {
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
