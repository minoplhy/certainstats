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

// countMax is a y-axis top for whole counts: a multiple of the four grid
// steps, so every gridline lands on an integer.
export function countMax(series) {
  const top = Math.max(0, ...series.flatMap(s => s.data.map(p => p.value)));
  return Math.max(4, Math.ceil(top / 4) * 4);
}

// drawChart hands one response to the shared chart engine: the axis is the
// window the server answered for, the step its bucket width, and brushing
// zooms the whole page (state.onZoom).
export function drawChart(key, canvasId, series, data, formatter, yMax = null) {
  const customRange = {start: data.start, end: data.end};
  const chart = state.charts[key];
  if (chart) {
    chart.updateSeries(series, state.view.hours, yMax, data.end, customRange, undefined, data.step);
    return chart;
  }
  state.charts[key] = window.CertainStatsChart?.renderMultiChart(canvasId, {
    seriesList: series, hours: state.view.hours, customRange, queryEndTime: data.end, minMax: 1, stepMs: data.step, formatter, yMax,
    onZoom: (start, end) => state.onZoom?.(start, end)
  });
  return state.charts[key];
}

function draw(group, data) {
  const series = seriesFor(data.series);
  const formatter = group === 'bytes' ? v => formatBytes(v) + '/s' : v => formatRate(v) + '/s';
  drawChart(group, 'kn-chart-' + group, series, data, formatter);
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
