/* CertainStats — Enhanced Multi-Series Vanilla JS Telemetry Chart Renderer with Collision-Proof X Labels, Scoped Brush Zoom & Downtime Detection */

import { formatBytes, formatBps } from '../../lib/chart/format.js';
import { getPtTs, getPtVal, convertDeltaToRate, calculateDowntimes } from '../../lib/chart/series.js';
import { resolveColor } from '../../lib/chart/canvas.js';
import { renderMultiChart } from './multi/index.js';
import { initThemeToggle } from './theme_toggle.js';

window.CertainStatsChart = {
  getPtTs,
  getPtVal,
  formatBytes,
  formatBps,
  convertDeltaToRate,
  calculateDowntimes,
  resolveColor,
  renderMultiChart,
  initThemeToggle
};

document.addEventListener('DOMContentLoaded', function () {
  window.CertainStatsChart.initThemeToggle();
});
