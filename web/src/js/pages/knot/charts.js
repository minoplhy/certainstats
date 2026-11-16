import { state } from './state.js';
import { get, isAbort } from './api.js';
import { counterParams } from './view.js';
import { el, formatRate, formatBytes, formatCount } from './format.js';

export const GROUPS = ['protocol', 'rcode', 'qtype', 'operation', 'bytes'];
const PALETTE = ['--s1', '--s2', '--s3', '--s4'];
const OTHER = '--s5';

// seriesFor turns API lines into chart series; "other" is always grey.
export function seriesFor(lines) {
  let next = 0;
  return lines.map(l => ({
    label: l.name,
    color: l.name === 'other' ? OTHER : PALETTE[next++ % PALETTE.length],
    fill: false,
    data: l.points.map(p => ({timestamp: p[0], value: p[1]}))
  }));
}

function legend(group, lines, series) {
  const box = document.querySelector('[data-kn-legend="' + group + '"]');
  if (!box) return;
  box.replaceChildren(...lines.map((l, i) => {
    const item = el('span', 'chart-legend-item');
    const dot = el('span', 'chart-legend-dot');
    dot.style.background = 'var(' + series[i].color + ')';
    dot.setAttribute('aria-hidden', 'true');
    const total = group === 'bytes' ? formatBytes(l.total) : formatCount(l.total);
    item.append(dot, document.createTextNode(l.name + ' '), el('span', 'chart-legend-val', total));
    return item;
  }));
}

function message(group, text) {
  const node = document.querySelector('[data-kn-chart-message="' + group + '"]');
  if (node) node.textContent = text;
}

function draw(group, data) {
  const series = seriesFor(data.series);
  const customRange = {start: data.start, end: data.end};
  const hours = Math.max(1, Math.round((data.end - data.start) / 3600000));
  const formatter = group === 'bytes' ? v => formatBytes(v) + '/s' : v => formatRate(v) + '/s';
  const chart = state.charts[group];
  if (chart) {
    chart.updateSeries(series, hours, null, data.end, customRange, undefined, data.step);
  } else {
    state.charts[group] = window.CertainStatsChart?.renderMultiChart('kn-chart-' + group, {
      seriesList: series, hours, customRange, queryEndTime: data.end, minMax: 1, stepMs: data.step, formatter
    });
  }
  legend(group, data.series, series);
}

// loadCharts reads one series group per request, in parallel.
export async function loadCharts(signal, note) {
  const v = state.view;
  await Promise.all(GROUPS.map(async group => {
    if (note) {
      draw(group, {series: [], start: Date.now() - 3600000, end: Date.now(), step: 60000});
      message(group, note);
      return;
    }
    try {
      const data = await get('series?' + counterParams(v, {group}), signal);
      draw(group, data);
      const empty = data.series.every(l => !l.total);
      message(group, data.collecting ? 'Collecting, first numbers in about a minute.' : empty ? 'No traffic in this range.' : '');
    } catch (e) {
      if (!isAbort(e)) message(group, 'Could not load: ' + e.message);
    }
  }));
}
