import { state } from '../state.js';
import { formatCount } from '../format.js';

function widthLabel(ms) {
  const m = ms / 60000;
  if (m < 60) return m + ' min';
  if (m < 1440) return (m / 60) + ' h';
  return (m / 1440) + ' d';
}

// drawTimeline draws one alert-count series on canvasId and returns the chart.
export function drawTimeline(existing, canvasId, data, range, onZoom) {
  const series = [{label: 'Alerts', color: '--s1', fill: true, data: data.points.map(p => ({timestamp: p[0], value: p[1]}))}];
  const customRange = {start: range.start, end: range.end};
  const hours = Math.max(1, Math.round((range.end - range.start) / 3600000));
  if (existing) {
    existing.updateSeries(series, hours, null, range.end, customRange);
    return existing;
  }
  return window.CertainStatsChart.renderMultiChart(canvasId, {
    seriesList: series, hours, customRange, queryEndTime: range.end, minMax: 1,
    formatter: v => formatCount(Math.round(v)), onZoom
  });
}

export function renderTimeline(data, onZoom) {
  const ui = state.ui;
  state.charts.timeline = drawTimeline(state.charts.timeline, 'cs-timeline', data, state.range, onZoom);
  const total = data.points.reduce((n, p) => n + p[1], 0);
  ui.timelineNote.textContent = 'per ' + widthLabel(data.width_ms) + (data.source === 'rollup' ? ' · hourly summary' : '');
  ui.timelineMessage.textContent = total ? '' : 'No alerts in this range.';
}
