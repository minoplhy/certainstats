import { getPtVal, calculateDowntimes } from '../../../lib/chart/series.js';
import { readToken } from '../../../lib/chart/canvas.js';
import { getBounds, getTimeRange } from './geometry.js';
import { drawGrid, drawOutages, drawSeries, drawTimeAxis, drawBrush } from './layers.js';
import { drawHover } from './tooltip.js';

// Redraws the whole chart from its state; each numbered step is one layer function.
export function draw(chart) {
  const { canvas, ctx, seriesList, formatter, queryEndTime, maxAdd, maxCap, minMax, yMax } = chart;
  const summaries=seriesList.map(series=>{const values=(series.data||[]).map(getPtVal).filter(value=>typeof value==='number' && Number.isFinite(value));return series.label+': '+(values.length ? values.length+' samples; latest '+formatter(values[values.length-1]) : 'No samples');});
  canvas.setAttribute('role','img');canvas.setAttribute('aria-label',summaries.join('. ')+' · '+Intl.DateTimeFormat().resolvedOptions().timeZone);

  let b = getBounds(chart);
  if (b.width === 0 || b.height === 0) return;

  canvas.width = b.width * window.devicePixelRatio;
  canvas.height = b.height * window.devicePixelRatio;
  ctx.scale(window.devicePixelRatio, window.devicePixelRatio);
  ctx.clearRect(0, 0, b.width, b.height);

  const tr = getTimeRange(chart);
  const minTs = tr.minTs;
  const maxTs = tr.maxTs;
  const tsSpan = Math.max(1, maxTs - minTs);

  let computedMax = 0;
  let hasPoints = false;
  seriesList.forEach(s => {
    (s.data || []).forEach(p => {
      if (p.value !== null && typeof p.value === 'number' && !isNaN(p.value)) {
        hasPoints = true;
        if (p.value > computedMax) computedMax = p.value;
      }
    });
  });

  const gridColor = readToken('--grid');
  const axisTextColor = readToken('--muted');
  const crosshairColor = readToken('--line-strong');
  const ttBg = readToken('--surface');
  const ttBorder = readToken('--line');
  const ttHeader = readToken('--muted');
  const ttLabel = readToken('--text-2');
  const ttVal = readToken('--text');
  const badColor = readToken('--bad');
  const surfaceColor = readToken('--surface');
  const monoFont = readToken('--font-mono') || 'monospace';
  const bodyFont = readToken('--font-body') || 'sans-serif';

  if (!hasPoints) {
    ctx.fillStyle = axisTextColor;
    ctx.font = '13px ' + bodyFont;
    ctx.textAlign = 'center';
    ctx.fillText('No data in this time range', b.width / 2, b.height / 2);
    return;
  }

  let effectiveMax;
  if (maxAdd !== null && maxAdd !== undefined) {
    const cap = (maxCap !== null && maxCap !== undefined) ? maxCap : 100;
    effectiveMax = Math.min(cap, Math.max(minMax, computedMax + maxAdd));
  } else {
    effectiveMax = (yMax !== null && yMax !== undefined) ? Math.max(yMax, computedMax) : computedMax;
  }
  if (!effectiveMax || isNaN(effectiveMax) || effectiveMax <= 0) effectiveMax = 100;

  // Fit the left gutter to the y-axis labels before laying out the plot
  ctx.font = '10px ' + monoFont;
  let labelW = 0;
  for (let i = 0; i <= 4; i++) labelW = Math.max(labelW, ctx.measureText(formatter(effectiveMax - (effectiveMax / 4) * i)).width);
  const fitted = Math.max(36, Math.ceil(labelW) + 14);
  if (fitted !== chart.leftPad) {
    chart.leftPad = fitted;
    b = getBounds(chart);
  }

  const primarySeries = (seriesList[0] && seriesList[0].data) ? seriesList[0].data : [];
  const downtimes = calculateDowntimes(primarySeries, queryEndTime, tsSpan);
  const view = {
    ctx, b, seriesList, formatter, queryEndTime, minTs, maxTs, tsSpan, effectiveMax, primarySeries, downtimes,
    gridColor, axisTextColor, crosshairColor, ttBg, ttBorder, ttHeader, ttLabel, ttVal, badColor, surfaceColor, monoFont, bodyFont,
    isDragging: chart.isDragging, dragStartX: chart.dragStartX, currentDragX: chart.currentDragX, hoverX: chart.hoverX
  };
  drawGrid(view);
  drawOutages(view);
  drawSeries(view);
  drawTimeAxis(view);
  drawBrush(view);
  drawHover(view);
}
