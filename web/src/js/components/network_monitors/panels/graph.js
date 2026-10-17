import { GRAPH_LIMIT } from '../shared/constants.js';
import { el, seriesColor, probeDetails, seriesLabel } from '../shared/format.js';

// Latency and loss graphs with their legend.
export function createGraph(ctx) {
  const { ui, hiddenSeries, pending, picker } = ctx;
  const s = ctx.state;

  function renderLegend() {
    ui.legend.replaceChildren();
    const groups = new Map();
    for (const monitor of s.candidates) {
      const key = monitor.target + ' · ' + monitor.protocol.toUpperCase();
      if (!groups.has(key)) {
        const group = el('div', 'network-legend-group');
        group.append(el('strong', 'network-legend-title', key));
        ui.legend.append(group);
        groups.set(key, group);
      }

      const isHidden = hiddenSeries.has(monitor.monitor_id);
      const toggle = el('button', 'network-series-toggle' + (isHidden ? ' is-hidden' : ''));
      toggle.type = 'button';
      toggle.setAttribute('aria-pressed', String(!isHidden));
      toggle.setAttribute('aria-label', 'Show ' + seriesLabel(monitor));
      const dot = el('span', 'chart-legend-dot');
      dot.style.backgroundColor = seriesColor(monitor.monitor_id);
      toggle.append(dot, el('span', '', monitor.agent_name + probeDetails(monitor)));
      toggle.addEventListener('click', () => {
        if (hiddenSeries.has(monitor.monitor_id)) hiddenSeries.delete(monitor.monitor_id);
        else hiddenSeries.add(monitor.monitor_id);
        renderLegend();
        paintHistory();
      });
      groups.get(key).append(toggle);
    }
  }

  function paintHistory() {
    if (!s.historyData) return;
    const byId = new Map(s.candidates.map(monitor => [monitor.monitor_id, monitor]));
    const shown = s.historyData.items.filter(item => byId.has(item.monitor_id) && !hiddenSeries.has(item.monitor_id));
    const series = key => shown.map(item => ({
      label: seriesLabel(byId.get(item.monitor_id)),
      color: seriesColor(item.monitor_id),
      fill: false,
      data: item.points.map(point => ({ timestamp: point.timestamp, value: point[key] }))
    }));
    const latency = series('response_avg_ms');
    const loss = series('loss_pct');

    // Brushing either chart zooms both and updates the picker's custom range.
    const zoom = (start, end) => {
      s.customRange = { start: Math.floor(start), end: Math.floor(end) };
      picker.setState({ hours: s.hours, customRange: s.customRange });
      loadHistory();
    };
    if (!s.charts.length) {
      const shared = { customRange: s.customRange, hours: s.hours, queryEndTime: s.historyData.end, onZoom: zoom };
      s.charts = [
        window.CertainStatsChart.renderMultiChart(ui.latency.id, { ...shared, seriesList: latency, formatter: v => v.toFixed(2) + ' ms' }),
        window.CertainStatsChart.renderMultiChart(ui.loss.id, { ...shared, seriesList: loss, formatter: v => v.toFixed(2) + '%', yMax: 100, maxCap: 100 })
      ];
    } else {
      s.charts[0]?.updateSeries(latency, s.hours, null, s.historyData.end, s.customRange);
      s.charts[1]?.updateSeries(loss, s.hours, 100, s.historyData.end, s.customRange);
    }

    const hasPoints = [...latency, ...loss].some(s => s.data.some(point => point.value != null));
    let message = '';
    if (!s.candidates.length) message = 'No monitors match these filters.';
    else if (!latency.length) message = 'All series are hidden. Use the legend to show a monitor.';
    else if (!hasPoints) message = 'No history for the selected time range yet.';
    ui['chart-message'].replaceChildren(el('span', '', message));
  }

  function destroyCharts() {
    s.charts.forEach(chart => chart?.destroy());
    s.charts = [];
  }

  async function loadCandidates() {
    if (!ctx.visible()) return;
    pending.history?.abort();
    pending.history = null;
    const controller = ctx.begin('candidates');
    if (!s.historyData) ui['chart-message'].textContent = 'Loading monitors…';
    try {
      const params = ctx.filterParams();
      params.set('page', '1');
      params.set('limit', String(GRAPH_LIMIT));
      const data = await ctx.api('network-monitors?' + params, { signal: controller.signal });
      if (controller !== pending.candidates || !ctx.visible()) return;

      s.candidates = data.items;
      ctx.rememberTargets(s.candidates);
      ui['chart-count'].textContent = 'Showing ' + s.candidates.length + ' of ' + data.total + ' matching monitors' +
        (data.total > GRAPH_LIMIT ? ' · Narrow the filters to see other monitors.' : '');
      renderLegend();
      await loadHistory();
    } catch (e) {
      if (e.name !== 'AbortError' && controller === pending.candidates) ctx.showError(ui['chart-message'], e.message, loadCandidates);
    }
  }

  // Each monitor's history is its own request, like the agent metrics graphs.
  async function loadHistory() {
    if (!ctx.visible()) return;
    if (!s.candidates.length) {
      pending.history?.abort();
      s.historyData = { items: [], end: Date.now() };
      paintHistory();
      return;
    }
    const controller = ctx.begin('history');
    ui['chart-message'].textContent = 'Loading history…';

    const params = new URLSearchParams();
    if (s.customRange) {
      params.set('start', s.customRange.start);
      params.set('end', s.customRange.end);
    } else {
      params.set('hours', String(s.hours));
    }
    const results = await Promise.all(s.candidates.map(monitor =>
      ctx.api('network-monitors/' + encodeURIComponent(monitor.monitor_id) + '/history?' + params, { signal: controller.signal })
        .then(data => ({ data }), error => ({ error }))
    ));
    if (controller !== pending.history || !ctx.visible()) return;

    const loaded = results.filter(result => result.data).map(result => result.data);
    const failure = results.find(result => result.error && result.error.name !== 'AbortError')?.error;
    // Keep the current plots when nothing loaded; otherwise show what did.
    if (loaded.length || !failure) {
      s.historyData = { items: loaded, end: Math.max(0, ...loaded.map(data => data.end)) || Date.now() };
      paintHistory();
    }
    if (failure) ctx.showError(ui['chart-message'], failure.message, loadHistory);
  }

  return { renderLegend, paintHistory, destroyCharts, loadCandidates, loadHistory };
}
