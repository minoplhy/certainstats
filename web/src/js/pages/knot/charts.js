import { state } from './state.js';
import { get, isAbort } from './api.js';
import { counterParams } from './view.js';
import { el, formatRate, formatBytes, formatCount } from './format.js';

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
  const box = state.ui.legend;
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

function message(text) {
  if (state.ui.chartMessage) state.ui.chartMessage.textContent = text;
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

// shown is the group on the traffic chart. The chart keeps the formatter it
// was made with, so that formatter reads this to label bytes or rates.
let shown = 'protocol';
const trafficFormat = v => shown === 'bytes' ? formatBytes(v) + '/s' : formatRate(v) + '/s';

function draw(group, data) {
  shown = group;
  const series = seriesFor(data.series);
  drawChart('traffic', 'kn-chart-traffic', series, data, trafficFormat);
  legend(group, data.series, series);
}

// syncTrafficTabs marks the traffic tab of the view's group as selected.
export function syncTrafficTabs() {
  for (const tab of state.ui.trafficTabs?.querySelectorAll('[data-kn-traffic-tab]') || []) {
    const on = tab.dataset.knTrafficTab === state.view.chart;
    tab.setAttribute('aria-selected', String(on));
    tab.tabIndex = on ? 0 : -1;
  }
}

// loadTraffic reads the one counter group the traffic chart shows. A reply
// for a group the reader has since switched away from is dropped.
export async function loadTraffic(signal, note) {
  const group = state.view.chart;
  syncTrafficTabs();
  if (note) {
    draw(group, {series: [], start: Date.now() - 3600000, end: Date.now(), step: 60000});
    message(note);
    return;
  }
  try {
    const data = await get('series?' + counterParams(state.view, {group}), signal);
    if (group !== state.view.chart) return;
    draw(group, data);
    const empty = data.series.every(l => !l.total);
    message(data.collecting ? 'Collecting, first numbers in about a minute.' : empty ? 'No traffic in this range.' : '');
  } catch (e) {
    if (!isAbort(e)) message('Could not load: ' + e.message);
  }
}
