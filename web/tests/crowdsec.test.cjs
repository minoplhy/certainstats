const {test} = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');
const {JSDOM} = require('jsdom');

const BUNDLE = fs.readFileSync('web/static/admin/js/crowdsec.js', 'utf8');
const COLLECTORS = fs.readFileSync('web/static/admin/js/collectors.js', 'utf8');

// Go template actions are dropped, leaving the partials' markup.
function partial(name) {
  return fs.readFileSync('web/templates/partials/' + name, 'utf8').replace(/\{\{[^}]*\}\}/g, '');
}

function helpers() {
  const context = {window: {}, document: {getElementById: () => null, querySelector: () => null}, URLSearchParams};
  vm.runInNewContext(BUNDLE, context);
  return context.window.CertainStatsCrowdSec;
}

test('view round-trips through the query string without splitting comma values', () => {
  const cs = helpers();
  const v = cs.parseView('?hours=168&scenario=crowdsecurity/ssh-bf&not_country=CN&not_country=RU&machine=a,b&q=203.&sort=events&order=asc&page=3&limit=50&group=ip&group=action&group=asn&group=machine&ip_view=1.2.3.4');
  assert.equal(v.hours, 168);
  assert.deepEqual([...v.exclude.country], ['CN', 'RU']);
  assert.deepEqual([...v.include.machine], ['a,b']);
  assert.equal(v.sort, 'events');
  assert.equal(v.order, 'asc');
  assert.equal(v.page, 3);
  assert.equal(v.limit, 50);
  assert.deepEqual([...v.groups], ['ip', 'action', 'asn', 'machine']);
  assert.equal(v.ip, '1.2.3.4');
  assert.deepEqual(cs.parseView('?' + cs.toQuery(v)), v);
  assert.equal(cs.toQuery(cs.parseView('')).toString(), 'hours=24');
  const bad = cs.parseView('?sort=drop&limit=7&group=ip&page=-4');
  assert.equal(bad.sort, 'time');
  assert.equal(bad.limit, 25);
  assert.equal(bad.groups.length, 4);
  assert.equal(bad.page, 1);
});

test('filters move between include and exclude, and requests carry range and time zone', () => {
  const cs = helpers();
  const v = cs.parseView('?page=4');
  cs.addFilter(v, 'country', 'NL', false);
  cs.addFilter(v, 'country', 'NL', true);
  assert.equal(v.include.country, undefined);
  assert.deepEqual([...v.exclude.country], ['NL']);
  assert.equal(v.page, 1);
  cs.addFilter(v, 'bogus', 'x', false);
  assert.equal(v.include.bogus, undefined);
  const q = cs.apiParams(v, {start: 1000.6, end: 5000}, 420);
  assert.equal(q.get('start'), '1000');
  assert.equal(q.get('not_country'), 'NL');
  assert.equal(q.get('tz_offset'), '420');
  cs.removeFilter(v, 'country', 'NL', true);
  assert.equal(v.exclude.country, undefined);
  const now = 10 * 365 * 86400000;
  assert.deepEqual({...cs.resolveRange({...cs.parseView('?all=1')}, now, now - 86400000)}, {start: now - 86400000 - 3600000, end: now, custom: true});
  assert.equal(cs.resolveRange(cs.parseView('?hours=6'), now, 0).start, now - 6 * 3600000);
});

test('headline states the situation in words first', () => {
  const cs = helpers();
  const v = cs.parseView('?hours=24');
  const ok = [{name: 'edge', connected: true, types: [{type: 'crowdsec', enabled: true, status: 'ok'}]}];
  assert.equal(cs.headline({alerts: 412, topScenario: 'crowdsecurity/ssh-bf'}, v, ok).text, '412 alerts in the last 24 hours, most from ssh-bf.');
  assert.equal(cs.headline({alerts: 0}, cs.parseView('?hours=168'), ok).text, 'No alerts in the last 7 days.');
  const down = [{name: 'edge', connected: false, types: [{type: 'crowdsec', enabled: true}]}];
  assert.deepEqual({...cs.headline({alerts: 3}, v, down)}, {bad: true, text: 'edge is not reporting.'});
  assert.equal(cs.groupLabel('asn', {value: '3320', label: 'DTAG'}), 'AS3320 DTAG');
  assert.equal(cs.untilText(0), 'unknown expiry');
  assert.equal(cs.untilText(1000, 2000), 'expired');
});

test('collector type states name the missing step', () => {
  const context = {window: {}, document: {readyState: 'complete', querySelector: () => null, body: null}};
  vm.runInNewContext(COLLECTORS, context);
  const {typeState} = context.window.CertainStatsCollectors;
  const t = {type: 'crowdsec', label: 'CrowdSec', enabled: true};
  assert.equal(typeState({connected: false, first_connected_at: 0}, t).text, 'Waiting for first connection');
  assert.equal(typeState({connected: true}, {...t, reported_on: false}).text, 'Connected, CrowdSec not switched on (COLLECT_CROWDSEC=true)');
  assert.equal(typeState({connected: true}, {...t, reported_on: true, status: 'error', error: 'cannot reach the Local API'}).text, 'Connected, cannot reach the Local API');
  assert.equal(typeState({connected: true}, {...t, reported_on: true, status: 'ok'}).level, 'ok');
  assert.equal(typeState({connected: true}, {...t, enabled: false}).text, 'Disabled');
});

function page(url) {
  const html = '<body data-panel-path="/panel"><div class="cs-page" data-cs-page>' + partial('crowdsec/hero.html') + '<div data-cs-main>' +
    partial('crowdsec/filters.html') + partial('crowdsec/panels.html') + partial('crowdsec/table.html') + '</div>' + partial('crowdsec/ip_view.html') +
    '</div><script type="application/json" id="page-config" data-module="CertainStatsCrowdSec">{}</script></body>';
  const dom = new JSDOM(html, {url, runScripts: 'outside-only'});
  const w = dom.window;
  const requests = [];
  const alerts = [
    {id: 1, collector_id: 'col_a', collector: 'edge', machine: 'web-01', scenario: 'crowdsecurity/ssh-bf', ip: '203.0.113.7', country: 'NL', asn: '64500', as_name: 'Ex', action: 'ban', events: 6, started_at: Date.now() - 60000},
    {id: 2, collector_id: 'col_a', collector: 'edge', machine: 'web-02', scenario: 'crowdsecurity/http-probing', ip: '198.51.100.20', country: 'US', action: 'none', events: 11, simulated: true, started_at: Date.now() - 120000}
  ];
  w.fetch = async input => {
    const u = new URL(input, url);
    requests.push(u);
    const path = u.pathname.replace('/panel/api/collectors/crowdsec/', '');
    let body;
    if (path === 'summary') body = {source: 'raw', alerts: 2, events: 17, unique_ips: 2, active: {ban: 1}, raw_since: 1, rollup_since: 1};
    else if (path === 'timeline') body = {source: 'raw', width_ms: 300000, points: [[Date.now() - 300000, 2, 3]]};
    else if (path.startsWith('groups/')) {
      const field = path.slice(7);
      body = {field, total: 2, items: field === 'scenario' ? [{value: 'crowdsecurity/ssh-bf', alerts: 1, events: 6}, {value: 'crowdsecurity/http-probing', alerts: 1, events: 11}] : [{value: field === 'country' ? 'NL' : 'x', alerts: 2, events: 17}]};
    } else if (path === 'alerts') body = {items: alerts, total: 2, page: 1, limit: 25};
    else if (path.startsWith('alerts/')) body = {...alerts[0], decisions: [{type: 'ban', origin: 'crowdsec', scope: 'Ip', value: '203.0.113.7', duration: '4h', until: Date.now() + 3600000}]};
    else if (path.startsWith('ips/')) body = {ip: '203.0.113.7', first_seen: Date.now() - 86400000, last_seen: Date.now(), alerts: 1, events: 6, country: 'NL', asn: '64500', as_name: 'Ex', scenarios: [{value: 'crowdsecurity/ssh-bf', alerts: 1}], machines: [{value: 'web-01', alerts: 1}], decisions: []};
    else return {ok: false, status: 404, json: async () => ({error: 'not found'})};
    return {ok: true, status: 200, json: async () => body};
  };
  const charts = [];
  w.CertainStatsChart = {renderMultiChart: (id, opts) => { const c = {id, opts, updates: 0, updateSeries() { this.updates++; }}; charts.push(c); return c; }};
  const pickers = [];
  w.CertainStatsTelemetry = {initCustomTimePicker: (el, opts) => {
    const p = {opts, states: [], cur: {hours: opts.activeHours, customRange: null, all: !!opts.allTime?.active},
      getState() { return this.cur; }, setState(s) { this.states.push(s); this.cur = {...this.cur, ...s}; }};
    pickers.push(p);
    return p;
  }, showToast() {}};
  const collectors = [{collector_id: 'col_a', name: 'edge', connected: true, types: [{type: 'crowdsec', label: 'CrowdSec', enabled: true, reported_on: true, status: 'ok'}]}];
  const opened = [];
  w.CertainStatsCollectors = {subscribe: fn => fn(collectors, []), refresh: async () => collectors, open: o => opened.push(o || {})};
  w.scrollTo = () => {};
  open.push(w);
  w.eval(BUNDLE);
  return {w, requests, charts, pickers, opened, q: sel => w.document.querySelector(sel)};
}

const settle = () => new Promise(resolve => setTimeout(resolve, 20));
const open = [];
require('node:test').after(() => { for (const w of open) w.close(); });

test('page loads every panel, and clicking a value filters in place', async () => {
  const h = page('http://x/panel/crowdsec?hours=24');
  await settle();
  assert.equal(h.q('[data-cs-headline]').textContent, '2 alerts in the last 24 hours, most from ssh-bf.');
  assert.equal(h.q('[data-cs-stat-bans]').textContent, '1');
  assert.equal(h.q('[data-cs-stat-collectors]').textContent, '1 of 1');
  assert.equal(h.q('[data-cs-empty]').hidden, true);
  assert.equal(h.w.document.querySelectorAll('.cs-row').length, 2);
  assert.equal(h.charts.length, 1);
  const groups = h.requests.filter(u => u.pathname.includes('/groups/') && u.searchParams.get('limit') === '8').map(u => u.pathname.split('/').pop());
  assert.deepEqual(groups.sort(), ['asn', 'country', 'machine', 'scenario'], 'one request per breakdown field');
  for (const u of h.requests) {
    assert.ok(u.searchParams.get('start') || u.pathname.includes('/alerts/') || u.pathname.includes('/ips/'), 'every data read is bounded: ' + u);
    for (const values of u.searchParams.values()) assert.ok(!values.includes(',') || u.searchParams.has('q'), 'no comma lists');
  }

  const nl = [...h.w.document.querySelectorAll('.cs-rank-value')].find(b => b.textContent === 'NL');
  nl.click();
  await settle();
  assert.equal(new URL(h.w.location.href).searchParams.get('country'), 'NL');
  assert.match(h.q('[data-cs-chips]').textContent, /Country\s*is\s*NL/);
  assert.ok(h.requests.at(-1).searchParams.get('country') === 'NL');

  h.q('.cs-chip-remove').click();
  await settle();
  assert.equal(new URL(h.w.location.href).searchParams.get('country'), null);
});

test('sorting, expanding a row and the IP view', async () => {
  const h = page('http://x/panel/crowdsec');
  await settle();
  const events = [...h.w.document.querySelectorAll('.cs-sort')].find(b => b.textContent.startsWith('Events'));
  events.click();
  await settle();
  assert.equal(new URL(h.w.location.href).searchParams.get('sort'), 'events');
  assert.equal(events.isConnected ? 'stale' : 'redrawn', 'redrawn');
  assert.equal(h.q('.cs-head-cell.cs-col-events').getAttribute('aria-sort'), 'descending');

  const row = h.q('.cs-row');
  row.open = true;
  row.dispatchEvent(new h.w.Event('toggle'));
  await settle();
  assert.match(row.textContent, /Decisions/);
  assert.match(row.textContent, /ban/);

  h.q('.cs-ip-link').click();
  await settle();
  assert.equal(new URL(h.w.location.href).searchParams.get('ip_view'), '203.0.113.7');
  assert.equal(h.q('[data-cs-ip]').hidden, false);
  assert.equal(h.q('[data-cs-main]').hidden, true);
  assert.equal(h.q('[data-cs-ip-alerts]').textContent, '1');
  h.q('[data-cs-ip-filter]').click();
  await settle();
  const params = new URL(h.w.location.href).searchParams;
  assert.equal(params.get('ip'), '203.0.113.7');
  assert.equal(params.get('ip_view'), null);
  assert.equal(h.q('[data-cs-ip]').hidden, true);
});

test('brushing the timeline sets a custom range and All asks for coverage first', async () => {
  const h = page('http://x/panel/crowdsec');
  await settle();
  h.charts[0].opts.onZoom(1000, 5000);
  await settle();
  const params = new URL(h.w.location.href).searchParams;
  assert.equal(params.get('start'), '1000');
  assert.equal(params.get('end'), '5000');
  assert.deepEqual({...h.pickers[0].states.at(-1).customRange}, {start: 1000, end: 5000});
  assert.equal(h.pickers[0].opts.compact, true);
  h.pickers[0].opts.allTime.onSelect();
  await settle();
  assert.equal(new URL(h.w.location.href).searchParams.get('all'), '1');
  assert.equal(h.pickers[0].getState().all, true);
  assert.equal(h.q('[data-cs-headline]').textContent, '2 alerts in all time, most from ssh-bf.');
});

test('the timeline draws alerts and decisions', async () => {
  const h = page('http://x/panel/crowdsec');
  await settle();
  const series = h.charts[0].opts.seriesList;
  assert.deepEqual(Array.from(series, s => s.label), ['Alerts', 'Decisions']);
  assert.equal(series[0].data[0].value, 2);
  assert.equal(series[1].data[0].value, 3);
  assert.equal(h.q('[data-cs-timeline-alerts]').textContent, '2');
  assert.equal(h.q('[data-cs-timeline-decisions]').textContent, '3');
});

test('the + Filter popover adds include and exclude filters', async () => {
  const h = page('http://x/panel/crowdsec');
  await settle();
  const toggle = h.q('[data-cs-filter-toggle]'), pop = h.q('[data-cs-filter-popover]');
  assert.equal(pop.hidden, true);
  toggle.click();
  assert.equal(pop.hidden, false);
  assert.equal(toggle.getAttribute('aria-expanded'), 'true');
  h.q('[data-cs-filter-field]').value = 'scenario';
  h.q('[data-cs-filter-value]').value = 'crowdsecurity/ssh-bf';
  pop.dispatchEvent(new h.w.Event('submit', {cancelable: true}));
  await settle();
  assert.equal(pop.hidden, true);
  assert.equal(new URL(h.w.location.href).searchParams.get('scenario'), 'crowdsecurity/ssh-bf');

  toggle.click();
  h.q('[data-cs-filter-field]').value = 'country';
  pop.querySelector('input[name="cs-filter-op"][value="not"]').checked = true;
  h.q('[data-cs-filter-value]').value = 'CN';
  pop.dispatchEvent(new h.w.Event('submit', {cancelable: true}));
  await settle();
  assert.equal(new URL(h.w.location.href).searchParams.get('not_country'), 'CN');
  assert.match(h.q('[data-cs-chips]').textContent, /Country\s*is not\s*CN/);

  toggle.click();
  pop.dispatchEvent(new h.w.KeyboardEvent('keydown', {key: 'Escape', bubbles: true}));
  assert.equal(pop.hidden, true);
});

test('the Collectors page lists collectors and ?add= opens the add form with that type', async () => {
  const page = fs.readFileSync('web/templates/collectors.html', 'utf8').replace(/\{\{[^}]*\}\}/g, '');
  const html = '<body data-panel-path="/panel">' + page + partial('collectors/add_modal.html') + '</body>';
  const dom = new JSDOM(html, {url: 'http://x/panel/collectors?add=crowdsec', runScripts: 'outside-only'});
  const w = dom.window;
  open.push(w);
  const posted = [], patched = [];
  w.fetch = async (input, init = {}) => {
    const u = new URL(input, 'http://x');
    if (init.method === 'PATCH') { patched.push([u.pathname, JSON.parse(init.body)]); return {ok: true, status: 200, json: async () => ({})}; }
    if (u.pathname === '/panel/api/collectors' && init.method === 'POST') {
      posted.push(JSON.parse(init.body));
      return {ok: true, status: 200, json: async () => ({collector_id: 'col_new', name: 'new'})};
    }
    if (u.pathname === '/panel/api/collectors') return {ok: true, status: 200, json: async () => ({
      items: [{collector_id: 'col_a', name: 'edge', connected: true, last_seen_at: Date.now(), types: [{type: 'crowdsec', label: 'CrowdSec', enabled: true, reported_on: true, status: 'ok'}]}],
      types: [{type: 'crowdsec', label: 'CrowdSec'}, {type: 'other', label: 'Other'}]
    })};
    return {ok: false, status: 404, json: async () => ({error: 'not found'})};
  };
  const modals = [];
  w.CertainStatsModal = {open: d => modals.push(['open', d.id]), close: d => modals.push(['close', d.id]), confirm: async () => false};
  w.setInterval = () => 0;
  w.eval(COLLECTORS);
  await settle();
  // Laid out like the Management page: an expandable row per collector.
  const item = w.document.querySelector('details.mgmt-item');
  assert.ok(w.document.querySelector('[data-collectors-list]').classList.contains('mgmt-list'));
  assert.equal(w.document.querySelectorAll('details.mgmt-item').length, 1);
  assert.equal(item.querySelector('.mgmt-name').textContent, 'edge');
  assert.match(item.querySelector('summary').textContent, /Connected/);
  assert.match(item.querySelector('summary').textContent, /CrowdSec/);
  const labels = [...item.querySelectorAll('.cred-row dt')].map(d => d.textContent);
  assert.deepEqual(labels, ['Name', 'Collector ID', 'Node', 'Version', 'Last seen', 'CrowdSec']);
  const actions = [...item.querySelectorAll('.mgmt-actions button')].map(b => b.textContent);
  assert.deepEqual(actions, ['Install', 'Reset token', 'Remove collector']);

  // Search hides rows that don't match.
  const search = w.document.querySelector('[data-collectors-search]');
  search.value = 'nomatch';
  search.dispatchEvent(new w.Event('input'));
  assert.equal(item.hidden, true);
  assert.equal(w.document.querySelector('.mgmt-no-match').hidden, false);
  search.value = 'edg';
  search.dispatchEvent(new w.Event('input'));
  assert.equal(item.hidden, false);

  // Rename works as on Management: Rename opens the inline editor; Escape cancels, Enter saves.
  const renameBtn = [...item.querySelectorAll('.cred-row button')].find(b => b.textContent === 'Rename');
  renameBtn.click();
  const edit = item.querySelector('.inline-edit'), input = edit.querySelector('input');
  assert.equal(edit.hidden, false);
  assert.equal(input.value, 'edge');
  input.dispatchEvent(new w.KeyboardEvent('keydown', {key: 'Escape', bubbles: true}));
  assert.equal(edit.hidden, true);
  renameBtn.click();
  input.value = 'edge-2';
  input.dispatchEvent(new w.KeyboardEvent('keydown', {key: 'Enter', bubbles: true}));
  await settle();
  assert.deepEqual(patched, [['/panel/api/collectors/col_a', {name: 'edge-2'}]]);
  assert.match(w.document.querySelector('[data-collectors-count]').textContent, /1 collector · 1 connected/);
  assert.equal(w.location.search, '', 'the add parameter is cleared');
  assert.deepEqual(modals[0], ['open', 'collectors-add-modal']);
  const ticked = [...w.document.querySelectorAll('[data-collectors-types] input:checked')].map(i => i.value);
  assert.deepEqual(ticked, ['crowdsec']);
  w.document.querySelector('[data-collectors-name]').value = 'new';
  w.document.querySelector('[data-collectors-form]').dispatchEvent(new w.Event('submit', {cancelable: true}));
  await settle();
  assert.deepEqual(posted, [{name: 'new', agent_id: '', types: ['crowdsec']}]);
  assert.ok(modals.some(m => m[0] === 'close' && m[1] === 'collectors-add-modal'));
});
