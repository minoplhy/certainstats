import { state } from './state.js';
import { begin, get, isAbort } from './api.js';
import { apiParams, writeUrl } from './view.js';
import { drawChart, countMax } from './charts.js';
import { el, formatCount, zoneName } from './format.js';
import { copyText, copyIcon } from '../../lib/clipboard.js';

// Each tab is one dimension; clicking a row filters by it where a filter exists.
export const CARDS = [
  {dim: 'qname', title: 'Names', filter: 'qname', limit: 25},
  {dim: 'qtype', title: 'Types', filter: 'qtype'},
  {dim: 'rcode', title: 'Codes', filter: 'rcode'},
  {dim: 'zone', title: 'Zones', filter: 'zone'},
  {dim: 'size', title: 'Sizes'},
  {dim: 'client', title: 'Clients', clients: true, limit: 25}
];

export function sizeLabel(key) {
  const n = Number(key);
  return n ? 'up to ' + n + ' B' : 'larger';
}

function rows(slot, spec, b, onFilter) {
  const max = Math.max(1, ...b.rows.map(r => r.count));
  slot.list.replaceChildren(...b.rows.map(r => {
    const li = el('li', 'cs-rank-row');
    const label = spec.dim === 'size' ? sizeLabel(r.key) : spec.dim === 'zone' || spec.dim === 'qname' ? zoneName(r.key) : r.key || '(unknown)';
    const value = el('button', 'cs-rank-value', label);
    value.type = 'button';
    value.title = r.key;
    // Client networks have no filter; clicking one copies it instead.
    const copyable = spec.dim === 'client' && r.key;
    if (spec.filter && r.key) value.addEventListener('click', () => onFilter(spec.filter, r.key));
    else if (copyable) {
      value.classList.add('mono');
      value.title = 'Copy ' + r.key;
      value.addEventListener('click', () => copyText(r.key, 'Network'));
    } else value.disabled = true;
    const meter = el('meter', 'cs-rank-meter');
    meter.min = 0;
    meter.max = max;
    meter.value = r.count;
    li.append(value);
    if (copyable) li.append(copyIcon(li.ownerDocument, 'Copy ' + r.key, () => copyText(r.key, 'Network')));
    li.append(el('span', 'cs-rank-count mono', formatCount(r.count)), meter);
    return li;
  }));
  slot.status.textContent = b.rows.length ? '' : 'Nothing in this range.';
  const notes = [];
  if (b.other) notes.push(formatCount(b.other) + ' more not listed');
  if (spec.dim === 'qname' && b.approximate) notes.push('names below the per-minute cap are folded away, so counts are lower bounds');
  if (b.ignored?.length) notes.push('not split by ' + b.ignored.join(', '));
  slot.foot.textContent = notes.join(' · ');
}

// specs are the tabs this page offers: Clients only when a server counts them.
function specs() {
  const clients = (state.overview?.servers || []).some(s => s.dnstap?.clients);
  return CARDS.filter(c => !c.clients || clients);
}

function slot() {
  const ui = state.ui;
  return {status: ui.topStatus, list: ui.topList, foot: ui.topFoot};
}

// tabs draws the top list's tabs, rebuilding them only when the set changes.
function tabs(list) {
  const box = state.ui.topTabs;
  const want = list.map(c => c.dim).join(' ');
  if (box.dataset.dims !== want) {
    box.dataset.dims = want;
    box.replaceChildren(...list.map(spec => {
      const tab = el('button', '', spec.title);
      tab.type = 'button';
      tab.setAttribute('role', 'tab');
      tab.dataset.knTopTab = spec.dim;
      tab.addEventListener('click', () => switchTop(spec.dim));
      return tab;
    }));
  }
  for (const tab of box.children) {
    const on = tab.dataset.knTopTab === state.view.top;
    tab.setAttribute('aria-selected', String(on));
    tab.tabIndex = on ? 0 : -1;
  }
}

// readTop fetches the view's dimension. Every reply carries the matching total
// and its series, so the chart is drawn from the same request.
async function readTop(signal) {
  const spec = CARDS.find(c => c.dim === state.view.top);
  const s = slot();
  try {
    const data = await get('queries?' + apiParams(state.view, {dim: spec.dim, limit: spec.limit || 12}), signal);
    if (spec.dim !== state.view.top) return;
    rows(s, spec, data.breakdown, state.onFilter);
    drawSeries(data);
  } catch (e) {
    if (!isAbort(e)) s.status.textContent = 'Could not load: ' + e.message;
  }
}

function switchTop(dim) {
  if (dim === state.view.top) return;
  state.view.top = dim;
  writeUrl(state.view);
  tabs(specs());
  return readTop(begin('top'));
}

// loadDnstap shows the dnstap section when any server sends dnstap, with one request.
export async function loadDnstap(signal, onFilter) {
  const ui = state.ui;
  const servers = state.overview?.servers || [];
  const on = servers.some(s => s.dnstap || s.last_dnstap_ms);
  ui.dnstap.hidden = !on;
  if (!on) return;
  state.onFilter = onFilter;
  const list = specs();
  if (!list.some(c => c.dim === state.view.top)) state.view.top = list[0].dim;
  tabs(list);
  await readTop(signal);
}

function drawSeries(data) {
  const ui = state.ui;
  const b = data.breakdown;
  const series = [{label: 'Queries', color: '--s1', fill: true, data: b.series.map(p => ({timestamp: p[0], value: p[1]}))}];
  drawChart('dnstap', 'kn-chart-dnstap', series, data, v => formatCount(Math.round(v)), countMax(series));
  ui.dnstapTotal.textContent = formatCount(b.total);
  ui.dnstapMessage.textContent = b.total ? '' : 'No dnstap traffic matches in this range.';
  const notes = [b.exact ? 'Totals are exact' : 'Filtered by name, so totals are lower bounds'];
  if (b.queries_only) notes.push('some minutes had no responses logged, so response codes are unknown there (turn on log-responses)');
  ui.dnstapNote.textContent = notes.join(' · ');
}
