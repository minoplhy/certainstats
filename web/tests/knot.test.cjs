const {test, after} = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');
const {JSDOM} = require('jsdom');

const BUNDLE = fs.readFileSync('web/static/admin/js/knot.js', 'utf8');

// Shared defines (the filter menu) are expanded where a partial uses them.
const SHARED = {};
for (const [, name, body] of fs.readFileSync('web/templates/partials/filters/menu.html', 'utf8').matchAll(/\{\{define "([^"]+)"\}\}([\s\S]*?)\{\{end\}\}/g)) SHARED[name] = body;

// filterMenu is the real shared filter menu, loaded once from admin_core in a window of its own;
// it builds nodes in the document of the element it is given.
let realMenu = null;
function filterMenu() {
  if (!realMenu) {
    const dom = new JSDOM('<body></body>', {url: 'http://x/', runScripts: 'outside-only'});
    dom.window.CertainStatsChart = {};
    for (const file of ['shared/js/telemetry', 'admin/js/admin_core']) dom.window.eval(fs.readFileSync('web/static/' + file + '.js', 'utf8'));
    const t = dom.window.CertainStatsTelemetry;
    realMenu = {createFilterMenu: t.createFilterMenu, renderFilterChips: t.renderFilterChips};
  }
  return realMenu;
}

function partial(name) {
  return fs.readFileSync('web/templates/partials/' + name, 'utf8')
    .replace(/\{\{template "([^"]+)" \.\}\}/g, (m, n) => SHARED[n] ?? m)
    .replace(/\{\{[^}]*\}\}/g, '');
}

function helpers() {
  const context = {window: {}, document: {getElementById: () => null, querySelector: () => null}, URLSearchParams};
  vm.runInNewContext(BUNDLE, context);
  return context.window.CertainStatsKnot;
}

test('view round-trips through the URL with the shared range keys and one value per filter', () => {
  const kn = helpers();
  const v = kn.parseView('?server=col_a&hours=168&zone=Example.COM&qname=www.example.com.&qtype=aaaa&rcode=nxdomain');
  assert.deepEqual({...v}, {server: 'col_a', hours: 168, start: null, end: null, zone: 'example.com.', qname: 'www.example.com.', qtype: 'AAAA', rcode: 'NXDOMAIN'});
  assert.deepEqual({...kn.parseView('?' + kn.toQuery(v))}, {...v});
  // Hours outside the picker's quick ranges fall back; old range= links still open the same window.
  assert.equal(kn.parseView('?hours=5').hours, 24);
  assert.equal(kn.parseView('', 720).hours, 720);
  assert.equal(kn.parseView('?range=7d').hours, 168);
  assert.equal(kn.toQuery(kn.parseView('')).toString(), 'hours=24');
  // The API calls the server filter "collector"; counters drop what they cannot filter.
  assert.equal(kn.apiParams(v, {dim: 'qname'}), 'hours=168&collector=col_a&zone=example.com.&qname=www.example.com.&qtype=AAAA&rcode=NXDOMAIN&dim=qname');
  assert.equal(kn.counterParams(v, {group: 'rcode'}), 'hours=168&collector=col_a&zone=example.com.&group=rcode');
  // A custom or zoomed range is sent as start and end instead of hours.
  kn.setCustom(v, 1000.7, 5000.2);
  assert.equal(kn.toQuery(v).get('start'), '1000');
  assert.equal(kn.toQuery(v).get('hours'), null);
  assert.equal(kn.counterParams(v, {group: 'rcode'}), 'start=1000&end=5000&collector=col_a&zone=example.com.&group=rcode');
  // A custom URL carries no hours; the page keeps the quick range it had.
  assert.deepEqual({...kn.parseView('?' + kn.toQuery(v), 168)}, {...v});
  kn.setCustom(v, null, null);
  assert.equal(kn.toQuery(v).get('hours'), '168');
  assert.equal(kn.dnstapOnly(v), true);
  assert.equal(kn.dnstapOnly(kn.parseView('?zone=a')), false);
  assert.equal(kn.normalize('zone', '(none)'), '(none)');
  kn.removeFilter(v, 'qname');
  assert.equal(v.qname, '');
});

test('numbers, times and headlines read plainly', () => {
  const kn = helpers();
  assert.equal(kn.formatCount(482), '482');
  assert.equal(kn.formatCount(48210), '48k');
  assert.equal(kn.formatCount(4821), '4.8k');
  assert.equal(kn.formatCount(12400000), '12.4M');
  assert.equal(kn.formatRate(0.25), '0.25');
  assert.equal(kn.formatShare(1, 2000), '<0.1%');
  assert.equal(kn.formatShare(26, 1000), '2.6%');
  assert.equal(kn.formatBytes(1500), '1.5 kB');
  assert.equal(kn.until(1000 + 3 * 3600000 + 20 * 60000, 1000), 'in 3h 20m');
  assert.equal(kn.until(1000, 1000 + 5 * 60000), 'overdue 5m');
  assert.equal(kn.zoneName('example.com.'), 'example.com');
  assert.equal(kn.zoneName('.'), '.');
  const view = {hours: 24};
  assert.equal(kn.headline({servers: []}, null, view), 'No Knot DNS servers yet');
  assert.equal(kn.headline({servers: [{}], checks: []}, {collecting: true}, view), 'Collecting, first numbers in about a minute');
  assert.equal(kn.headline({servers: [{}], checks: []}, {requests: 482000}, view), '482k queries in the last 24 hours, all clear');
  assert.equal(kn.headline({servers: [{}], checks: [{level: 'bad'}, {level: 'warn'}]}, {requests: 1}, view), '1 issue needs attention');
  assert.equal(kn.headline({servers: [{}], checks: [{level: 'warn'}]}, {requests: 10}, view), '10 queries in the last 24 hours, 1 thing to look at');
  assert.equal(kn.headline({servers: [{}], checks: []}, {requests: 10}, {hours: 1}), '10 queries in the last hour, all clear');
  assert.equal(kn.headline({servers: [{}], checks: []}, {requests: 10}, {hours: 168}), '10 queries in the last 7 days, all clear');
  assert.match(kn.headline({servers: [{}], checks: []}, {requests: 10}, {hours: 24, start: 1, end: 2}), /^10 queries between .+ and .+, all clear$/);
});

test('zone status and chart colours', () => {
  const kn = helpers();
  const now = 10 * 86400000;
  const zone = (servers, extra) => ({zone: 'a.', servers, drift: false, expire_at: 0, ...extra});
  assert.equal(kn.zoneStatus(zone([{role: 'secondary', expire_at: now - 1}]), now).level, 'bad');
  assert.equal(kn.zoneStatus(zone([{role: 'primary'}, {role: 'secondary', behind: true}], {drift: true}), now).text, '1 behind');
  assert.equal(kn.zoneStatus(zone([{role: 'secondary'}], {expire_at: now + 3600000}), now).text, 'Expiring');
  assert.equal(kn.zoneStatus(zone([{role: 'primary'}]), now).text, 'In sync');
  assert.equal(kn.roles(zone([{role: 'primary'}, {role: 'secondary'}, {role: 'secondary'}])), '1 primary, 2 secondaries');
  const series = kn.seriesFor([{name: 'udp4', points: [[1, 2]]}, {name: 'tcp4', points: []}, {name: 'other', points: []}]);
  assert.equal(JSON.stringify(series.map(s => s.color)), JSON.stringify(['--s1', '--s2', '--s5']));
  assert.equal(JSON.stringify(series[0].data), JSON.stringify([{timestamp: 1, value: 2}]));
  assert.equal(kn.sizeLabel('512'), 'up to 512 B');
  assert.equal(kn.sizeLabel('0'), 'larger');
});

const open = [];
after(() => { for (const w of open) w.close(); });

function page(url, fixtures = {}) {
  const html = '<body data-panel-path="/panel"><div class="kn-page" data-kn-page>' + partial('knot/hero.html') + '<div data-kn-main hidden>' +
    partial('knot/toolbar.html') + partial('knot/checks.html') + partial('knot/panels.html') + '</div></div>' +
    '<script type="application/json" id="page-config" data-module="CertainStatsKnot">{}</script></body>';
  const dom = new JSDOM(html, {url, runScripts: 'outside-only'});
  const w = dom.window;
  const requests = [];
  const now = Date.now();
  const servers = fixtures.servers || [
    {collector_id: 'col_a', name: 'ns1', enabled: true, connected: true, reported_on: true, status: 'ok', version: '3.4.2', mod_stats: true,
      dnstap: {socket: '/run/certainstats/dnstap.sock', listening: true, clients: false}, last_dnstap_ms: now},
    {collector_id: 'col_b', name: 'ns2', enabled: true, connected: true, reported_on: true, status: 'ok', mod_stats: true}
  ];
  w.fetch = async input => {
    const u = new URL(input, url);
    const path = u.pathname.replace('/panel/api/collectors/knot/', '');
    requests.push(u);
    let body;
    if (path === '') body = {servers, checks: fixtures.checks || [], zone_count: 1, now};
    else if (path === 'totals') body = fixtures.totals || {start: now - 86400000, end: now, from: now - 86400000, collecting: false, requests: 482000, qps: 5.6, responses: 482000, nxdomain: 12300, servfail: 0, refused: 0, transfers: 41};
    else if (path === 'series') body = {group: u.searchParams.get('group'), start: now - 86400000, end: now, step: 600000, collecting: false,
      series: [{name: 'udp4', total: 400000, points: [[now - 600000, 5]]}, {name: 'other', total: 1, points: [[now - 600000, 0]]}]};
    else if (path === 'queries') body = {start: now - 86400000, end: now, step: 600000, breakdown: {dim: u.searchParams.get('dim'), total: 90, exact: true,
      rows: [{key: u.searchParams.get('dim') === 'qname' ? 'www.example.com.' : 'AAAA', count: 60}], other: 30, approximate: true, series: [[now - 600000, 90]]}};
    else if (path === 'zones') body = {zones: [{zone: 'example.com.', primary_serial: 7, drift: true, expire_at: now + 3600000,
      servers: [{collector_id: 'col_a', server: 'ns1', role: 'primary', serial: 7, has_serial: true}, {collector_id: 'col_b', server: 'ns2', role: 'secondary', serial: 6, has_serial: true, behind: true, expire_at: now + 3600000}]}]};
    else if (path.startsWith('zones/')) body = {zone_stats: false, zone: {zone: 'example.com.', primary_serial: 7, drift: true,
      servers: [{collector_id: 'col_a', server: 'ns1', role: 'primary', serial: 7, has_serial: true}]}};
    else return {ok: false, status: 404, json: async () => ({error: 'not found'})};
    return {ok: true, status: 200, json: async () => body};
  };
  const menu = filterMenu();
  const charts = [];
  w.CertainStatsChart = {renderMultiChart: (id, opts) => { const c = {id, opts, updates: 0, updateSeries() { this.updates++; }}; charts.push(c); return c; }};
  const pickers = [];
  w.CertainStatsTelemetry = {initCustomTimePicker: (el, opts) => {
    let st = {hours: opts.activeHours, customRange: null};
    const p = {el, opts, sets: [], getState: () => ({...st}), setState(next) { st = {...next}; this.sets.push(next); }};
    pickers.push(p);
    return p;
  }, ...menu};
  w.scrollTo = () => {};
  open.push(w);
  w.eval(BUNDLE);
  return {w, requests, charts, pickers, q: sel => w.document.querySelector(sel), qa: sel => [...w.document.querySelectorAll(sel)]};
}

const settle = () => new Promise(r => setTimeout(r, 30));

test('the page loads counters, dnstap and zones, one resource per request', async () => {
  const p = page('http://x/panel/knot?hours=24');
  await settle();
  assert.equal(p.q('[data-kn-main]').hidden, false);
  assert.equal(p.q('[data-kn-empty]').hidden, true);
  assert.equal(p.q('[data-kn-headline]').textContent, '482k queries in the last 24 hours, all clear');
  assert.equal(p.q('[data-kn-stat-nxdomain]').textContent, '2.6%');
  assert.equal(p.q('[data-kn-server-count]').textContent, '2 servers, 2 connected');
  assert.deepEqual(p.qa('[data-kn-server] option').map(o => o.textContent), ['All servers', 'ns1', 'ns2']);
  // Five counter groups, six dnstap breakdowns minus clients (off), each its own GET with single values.
  const groups = p.requests.filter(u => u.pathname.endsWith('/series')).map(u => u.searchParams.get('group')).sort();
  assert.deepEqual(groups, ['bytes', 'operation', 'protocol', 'qtype', 'rcode']);
  const dims = p.requests.filter(u => u.pathname.endsWith('/queries')).map(u => u.searchParams.get('dim')).sort();
  assert.deepEqual(dims, ['qname', 'qtype', 'rcode', 'size', 'zone']);
  for (const u of p.requests) for (const [, v] of u.searchParams) assert.ok(!v.includes(','), 'comma list in ' + u);
  assert.ok(p.charts.some(c => c.id === 'kn-chart-protocol'));
  assert.ok(p.charts.some(c => c.id === 'kn-chart-dnstap'));
  assert.equal(p.q('[data-kn-dnstap]').hidden, false);
  assert.match(p.q('[data-kn-breakdowns]').textContent, /lower bounds/);
  const row = p.q('.kn-zones-row');
  assert.match(row.textContent, /example\.com/);
  assert.match(row.textContent, /1 behind/);
});

test('clicking a name filters by it, and counters say what they cannot split', async () => {
  const p = page('http://x/panel/knot');
  await settle();
  const name = [...p.w.document.querySelectorAll('.cs-rank-value')].find(b => b.textContent === 'www.example.com');
  name.click();
  await settle();
  assert.match(p.w.location.search, /qname=www\.example\.com\./);
  assert.equal(p.q('[data-kn-source-note]').hidden, false);
  assert.match(p.q('[data-kn-source-note]').textContent, /cannot filter by name/);
  const last = p.requests.filter(u => u.pathname.endsWith('/series')).pop();
  assert.equal(last.searchParams.get('qname'), null, 'counters never receive a name filter');
  assert.match(p.q('[data-filter-chips]').textContent, /Name/);
  p.q('[data-filter-chips] .filter-chip-remove').click();
  await settle();
  assert.doesNotMatch(p.w.location.search, /qname/);
});

test('a zone opens its view and explains when per-zone counters are off', async () => {
  const p = page('http://x/panel/knot');
  await settle();
  p.q('.kn-zones-row .kn-link').click();
  await settle();
  assert.match(p.w.location.search, /zone=example\.com\./);
  assert.equal(p.q('[data-kn-zone-view]').hidden, false);
  assert.equal(p.q('[data-kn-zone-title]').textContent, 'example.com');
  assert.match(p.q('[data-kn-source-note]').textContent, /KNOT_ZONE_STATS=true/);
  assert.match(p.q('[data-kn-chart-message="rcode"]').textContent, /Per-zone counters are off/);
  assert.equal(p.requests.filter(u => u.pathname.endsWith('/series') && u.searchParams.get('zone')).length, 0, 'no counter request for a zone without zone stats');
});

test('an empty install shows the add prompt; a fresh one says it is collecting', async () => {
  const empty = page('http://x/panel/knot', {servers: []});
  await settle();
  assert.equal(empty.q('[data-kn-empty]').hidden, false);
  assert.equal(empty.q('[data-kn-main]').hidden, true);
  const fresh = page('http://x/panel/knot', {totals: {collecting: true, requests: 0}});
  await settle();
  assert.equal(fresh.q('[data-kn-headline]').textContent, 'Collecting, first numbers in about a minute');
  assert.equal(fresh.q('[data-kn-stat-requests]').textContent, '–');
  assert.match(fresh.q('[data-kn-stat-note]').textContent, /first reading/);
});

test('the shared picker sets the range, and brushing a chart zooms the whole page', async () => {
  const p = page('http://x/panel/knot?range=7d');
  await settle();
  assert.equal(p.pickers.length, 1);
  const picker = p.pickers[0];
  assert.ok(picker.el.matches('[data-kn-time-picker]'));
  assert.equal(picker.opts.storageKey, 'certainstats_knot_hours');
  assert.equal(picker.opts.compact, true);
  assert.equal(picker.opts.activeHours, 168, 'an old range=7d link opens seven days');
  assert.ok(p.requests.filter(u => u.pathname.endsWith('/series')).every(u => u.searchParams.get('hours') === '168' && !u.searchParams.has('range')));

  picker.opts.onApply({hours: 6, customRange: null});
  await settle();
  assert.match(p.w.location.search, /hours=6/);
  assert.equal(p.requests.filter(u => u.pathname.endsWith('/totals')).pop().searchParams.get('hours'), '6');

  // Every chart gets the zoom handler; brushing one moves all requests to start/end.
  const chart = p.charts.find(c => c.id === 'kn-chart-rcode');
  assert.equal(typeof chart.opts.onZoom, 'function');
  assert.equal(typeof p.charts.find(c => c.id === 'kn-chart-dnstap').opts.onZoom, 'function');
  const now = Date.now();
  chart.opts.onZoom(now - 7200000.5, now - 3600000.5);
  await settle();
  const search = new URLSearchParams(p.w.location.search);
  assert.equal(search.get('start'), String(Math.floor(now - 7200000.5)));
  assert.equal(search.get('hours'), null);
  assert.equal(JSON.stringify(picker.sets.pop().customRange), JSON.stringify({start: Math.floor(now - 7200000.5), end: Math.floor(now - 3600000.5)}));
  const last = p.requests.filter(u => u.pathname.endsWith('/queries')).pop();
  assert.equal(last.searchParams.get('start'), String(Math.floor(now - 7200000.5)));
  assert.match(p.q('[data-kn-headline]').textContent, /queries between /);

  // Reset in the picker returns to the quick range.
  picker.opts.onApply({hours: 6, customRange: null});
  await settle();
  assert.equal(new URLSearchParams(p.w.location.search).get('hours'), '6');
  assert.equal(new URLSearchParams(p.w.location.search).get('start'), null);
});

test('the shared filter menu adds a Knot filter, with "is" only', async () => {
  const p = page('http://x/panel/knot');
  await settle();
  const toggle = p.q('[data-filter-toggle]'), pop = p.q('[data-filter-popover]');
  assert.deepEqual(p.qa('[data-filter-field] option').map(o => o.value), ['zone', 'qname', 'qtype', 'rcode']);
  assert.equal(p.q('[data-filter-match]').hidden, true, 'Knot filters cannot exclude');
  toggle.click();
  assert.equal(pop.hidden, false);
  p.q('[data-filter-field]').value = 'qtype';
  p.q('[data-filter-field]').dispatchEvent(new p.w.Event('change'));
  assert.equal(p.q('[data-filter-value]').placeholder, 'e.g. AAAA');
  p.q('[data-filter-value]').value = 'aaaa';
  pop.dispatchEvent(new p.w.Event('submit', {cancelable: true}));
  await settle();
  assert.equal(pop.hidden, true);
  assert.match(p.w.location.search, /qtype=AAAA/);
  assert.match(p.q('[data-filter-chips]').textContent, /Query type\s*is\s*AAAA/);
  p.q('[data-filter-chips] .filter-chip-remove').click();
  await settle();
  assert.doesNotMatch(p.w.location.search, /qtype/);
  assert.equal(p.q('[data-filter-chips]').hidden, true);
});
