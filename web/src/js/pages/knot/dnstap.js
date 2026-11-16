import { state } from './state.js';
import { get, isAbort } from './api.js';
import { apiParams } from './view.js';
import { drawChart } from './charts.js';
import { el, formatCount, zoneName } from './format.js';

// Each card is one dimension; clicking a row filters by it where a filter exists.
export const CARDS = [
  {dim: 'qname', title: 'Top names', filter: 'qname'},
  {dim: 'qtype', title: 'Query types', filter: 'qtype'},
  {dim: 'rcode', title: 'Response codes', filter: 'rcode'},
  {dim: 'zone', title: 'Zones', filter: 'zone'},
  {dim: 'size', title: 'Response sizes'},
  {dim: 'client', title: 'Client networks', clients: true}
];

export function sizeLabel(key) {
  const n = Number(key);
  return n ? 'up to ' + n + ' B' : 'larger';
}

function card(spec) {
  const article = el('article', 'chart-card cs-rank-card');
  const head = el('div', 'chart-header-row');
  head.append(el('h3', 'chart-header-title', spec.title));
  const status = el('p', 'cs-status');
  status.setAttribute('role', 'status');
  const list = el('ol', 'cs-rank');
  const foot = el('p', 'kn-note');
  article.append(head, status, list, foot);
  return {article, status, list, foot};
}

function rows(slot, spec, b, onFilter) {
  const max = Math.max(1, ...b.rows.map(r => r.count));
  slot.list.replaceChildren(...b.rows.map(r => {
    const li = el('li', 'cs-rank-row');
    const label = spec.dim === 'size' ? sizeLabel(r.key) : spec.dim === 'zone' || spec.dim === 'qname' ? zoneName(r.key) : r.key || '(unknown)';
    const value = el('button', 'cs-rank-value', label);
    value.type = 'button';
    value.title = r.key;
    if (spec.filter && r.key) value.addEventListener('click', () => onFilter(spec.filter, r.key));
    else value.disabled = true;
    const meter = el('meter', 'cs-rank-meter');
    meter.min = 0;
    meter.max = max;
    meter.value = r.count;
    li.append(value, el('span', 'cs-rank-count mono', formatCount(r.count)), meter);
    return li;
  }));
  slot.status.textContent = b.rows.length ? '' : 'Nothing in this range.';
  const notes = [];
  if (b.other) notes.push(formatCount(b.other) + ' more not listed');
  if (spec.dim === 'qname' && b.approximate) notes.push('names below the per-minute cap are folded away, so counts are lower bounds');
  if (b.ignored?.length) notes.push('not split by ' + b.ignored.join(', '));
  slot.foot.textContent = notes.join(' · ');
}

// loadDnstap reads one breakdown per request and draws the matching-queries chart from the first.
export async function loadDnstap(signal, onFilter) {
  const ui = state.ui;
  const servers = state.overview?.servers || [];
  const on = servers.some(s => s.dnstap || s.last_dnstap_ms);
  ui.dnstap.hidden = !on;
  if (!on) return;
  const clients = servers.some(s => s.dnstap?.clients);
  const specs = CARDS.filter(c => !c.clients || clients);
  if (!state.cards || state.cards.length !== specs.length) {
    state.cards = specs.map(card);
    ui.breakdowns.replaceChildren(...state.cards.map(c => c.article));
  }
  const v = state.view;
  let first = true;
  await Promise.all(specs.map(async (spec, i) => {
    const slot = state.cards[i];
    try {
      const data = await get('queries?' + apiParams(v, {dim: spec.dim, limit: spec.dim === 'qname' ? 25 : 12}), signal);
      rows(slot, spec, data.breakdown, onFilter);
      if (first) {
        first = false;
        drawSeries(data);
      }
    } catch (e) {
      if (!isAbort(e)) slot.status.textContent = 'Could not load: ' + e.message;
    }
  }));
}

function drawSeries(data) {
  const ui = state.ui;
  const b = data.breakdown;
  const series = [{label: 'Queries', color: '--s1', fill: true, data: b.series.map(p => ({timestamp: p[0], value: p[1]}))}];
  drawChart('dnstap', 'kn-chart-dnstap', series, data, v => formatCount(Math.round(v)));
  ui.dnstapTotal.textContent = formatCount(b.total);
  ui.dnstapMessage.textContent = b.total ? '' : 'No dnstap traffic matches in this range.';
  const notes = [b.exact ? 'Totals are exact' : 'Filtered by name, so totals are lower bounds'];
  if (b.queries_only) notes.push('some minutes had no responses logged, so response codes are unknown there (turn on log-responses)');
  ui.dnstapNote.textContent = notes.join(' · ');
}
