import { formatTimeAxis, formatDuration } from '../../../lib/chart/format.js';
import { buildCleanSeries } from '../../../lib/chart/series.js';
import { readToken, resolveColor, withAlpha, tracePath } from '../../../lib/chart/canvas.js';

// Chart layers, drawn in this order by draw(); view is built there.
export function drawGrid(view) {
  const { ctx, b, formatter, effectiveMax, gridColor, axisTextColor, monoFont } = view;
  // 1. Draw Gridlines & Y-Axis labels
  ctx.strokeStyle = gridColor;
  ctx.lineWidth = 1;
  const gridSteps = 4;
  for (let i = 0; i <= gridSteps; i++) {
    const y = b.padding.top + (b.graphHeight / gridSteps) * i;
    ctx.beginPath();
    ctx.moveTo(b.padding.left, y);
    ctx.lineTo(b.width - b.padding.right, y);
    ctx.stroke();

    const val = effectiveMax - (effectiveMax / gridSteps) * i;
    ctx.fillStyle = axisTextColor;
    ctx.font = '10px ' + monoFont;
    ctx.textAlign = 'right';
    ctx.fillText(formatter(val), b.padding.left - 8, y + 3);
  }
}

export function drawOutages(view) {
  const { ctx, b, minTs, tsSpan, downtimes, badColor, bodyFont } = view;
  // 2. Draw Downtime Outage Bands
  downtimes.forEach(dw => {
    const x1 = Math.max(b.padding.left, b.padding.left + ((dw.start - minTs) / tsSpan) * b.graphWidth);
    const x2 = Math.min(b.padding.left + b.graphWidth, b.padding.left + ((dw.end - minTs) / tsSpan) * b.graphWidth);
    const w = Math.max(2, x2 - x1);

    ctx.fillStyle = withAlpha(badColor, 0.1);
    ctx.fillRect(x1, b.padding.top, w, b.graphHeight);

    ctx.strokeStyle = withAlpha(badColor, 0.45);
    ctx.setLineDash([3, 3]);
    ctx.beginPath();
    ctx.moveTo(x1, b.padding.top);
    ctx.lineTo(x1, b.padding.top + b.graphHeight);
    ctx.moveTo(x2, b.padding.top);
    ctx.lineTo(x2, b.padding.top + b.graphHeight);
    ctx.stroke();
    ctx.setLineDash([]);

    const label = 'Offline ' + formatDuration(dw.end - dw.start);
    ctx.font = '600 10px ' + bodyFont;
    if (ctx.measureText(label).width + 12 < w) {
      ctx.fillStyle = badColor;
      ctx.textAlign = 'center';
      ctx.fillText(label, x1 + w / 2, b.padding.top + 14);
    }
  });
}

export function drawSeries(view) {
  const { ctx, b, seriesList, queryEndTime, minTs, maxTs, tsSpan, effectiveMax, surfaceColor } = view;
  // 3. Draw Metric Series Paths (smooth curves, soft area, latest-point marker)
  const toXY = p => ({
    x: b.padding.left + ((Math.max(minTs, Math.min(maxTs, p.timestamp)) - minTs) / tsSpan) * b.graphWidth,
    y: b.padding.top + b.graphHeight - (Math.min(p.value, effectiveMax) / effectiveMax) * b.graphHeight
  });
  const baseY = b.padding.top + b.graphHeight;
  const endMarkers = [];

  seriesList.forEach(series => {
    const rawData = series.data || [];
    if (rawData.length === 0) return;
    const cleanData = buildCleanSeries(rawData, queryEndTime, tsSpan);
    const color = resolveColor(series.color);

    const segments = [[]];
    cleanData.forEach(p => {
      if (p.value === null || isNaN(p.value)) {
        if (segments[segments.length - 1].length) segments.push([]);
        return;
      }
      segments[segments.length - 1].push(toXY(p));
    });

    segments.filter(sg => sg.length > 0).forEach(sg => {
      if (series.fill !== false) {
        ctx.beginPath();
        ctx.moveTo(sg[0].x, baseY);
        tracePath(ctx, sg, false);
        ctx.lineTo(sg[sg.length - 1].x, baseY);
        ctx.closePath();
        const grad = ctx.createLinearGradient(0, b.padding.top, 0, baseY);
        grad.addColorStop(0, withAlpha(color, series.fill ? 0.24 : 0.12));
        grad.addColorStop(1, withAlpha(color, 0.02));
        ctx.fillStyle = grad;
        ctx.fill();
      }
      ctx.beginPath();
      tracePath(ctx, sg, true);
      ctx.strokeStyle = color;
      ctx.lineWidth = 2;
      ctx.lineJoin = 'round';
      ctx.lineCap = 'round';
      ctx.stroke();
    });

    const lastSeg = segments.filter(sg => sg.length > 0).pop();
    const lastRaw = cleanData[cleanData.length - 1];
    if (lastSeg && lastRaw && lastRaw.value !== null && !isNaN(lastRaw.value)) {
      endMarkers.push({ pt: lastSeg[lastSeg.length - 1], color: color });
    }
  });

  endMarkers.forEach(m => {
    ctx.beginPath();
    ctx.arc(m.pt.x, m.pt.y, 3.5, 0, Math.PI * 2);
    ctx.fillStyle = m.color;
    ctx.fill();
    ctx.lineWidth = 2;
    ctx.strokeStyle = surfaceColor;
    ctx.stroke();
  });
}

export function drawTimeAxis(view) {
  const { ctx, b, minTs, maxTs, tsSpan, axisTextColor, monoFont } = view;
  // 4. Time Axis Ticks with Strict Non-Overlapping Guarantee
  ctx.font = '10px ' + monoFont;
  ctx.fillStyle = axisTextColor;

  const sampleStart = formatTimeAxis(minTs, tsSpan);
  const sampleEnd = formatTimeAxis(maxTs, tsSpan);
  const sampleMid = formatTimeAxis(minTs + tsSpan / 2, tsSpan);
  const maxLabelWidth = Math.max(
    ctx.measureText(sampleStart).width,
    ctx.measureText(sampleEnd).width,
    ctx.measureText(sampleMid).width
  );

  const neededSlotWidth = Math.max(70, maxLabelWidth + 24);
  const maxTicks = Math.max(2, Math.floor(b.graphWidth / neededSlotWidth));
  const tickCount = Math.min(5, maxTicks);

  let lastDrawnRight = -Infinity;

  for (let i = 0; i <= tickCount; i++) {
    const t = minTs + (tsSpan / tickCount) * i;
    const x = b.padding.left + (i / tickCount) * b.graphWidth;
    const text = formatTimeAxis(t, tsSpan);
    const textWidth = ctx.measureText(text).width;

    let align = 'center';
    let leftEdge = x - textWidth / 2;
    let rightEdge = x + textWidth / 2;

    if (i === 0) {
      align = 'left';
      leftEdge = x;
      rightEdge = x + textWidth;
    } else if (i === tickCount) {
      align = 'right';
      leftEdge = x - textWidth;
      rightEdge = x;
    }

    if (leftEdge >= lastDrawnRight + 8) {
      if (i < tickCount) {
        const lastLabelLeft = (b.padding.left + b.graphWidth) - ctx.measureText(sampleEnd).width;
        if (rightEdge + 8 > lastLabelLeft) {
          continue;
        }
      }

      ctx.textAlign = align;
      ctx.fillText(text, x, b.height - 6);
      lastDrawnRight = rightEdge;
    }
  }
}

export function drawBrush(view) {
  const { ctx, b, isDragging, dragStartX, currentDragX } = view;
  // 5. Drag Brush Selection Overlay
  if (isDragging && dragStartX !== null && currentDragX !== null) {
    const start = Math.max(b.padding.left, Math.min(dragStartX, currentDragX));
    const end = Math.min(b.padding.left + b.graphWidth, Math.max(dragStartX, currentDragX));
    const w = Math.max(0, end - start);

    const accent = readToken('--accent');
    ctx.fillStyle = withAlpha(accent, 0.14);
    ctx.fillRect(start, b.padding.top, w, b.graphHeight);

    ctx.strokeStyle = withAlpha(accent, 0.8);
    ctx.lineWidth = 1;
    ctx.strokeRect(start, b.padding.top, w, b.graphHeight);
  }
}
