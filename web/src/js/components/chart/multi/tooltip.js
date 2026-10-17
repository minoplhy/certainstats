import { formatTimeAxis } from '../../../lib/chart/format.js';
import { getPtTs, getPtVal } from '../../../lib/chart/series.js';
import { resolveColor } from '../../../lib/chart/canvas.js';

export function drawHover(view) {
  const { ctx, b, seriesList, formatter, minTs, tsSpan, primarySeries, downtimes, isDragging, hoverX,
    crosshairColor, ttBg, ttBorder, ttHeader, ttLabel, ttVal, monoFont, bodyFont } = view;
  // 6. Interactive Hover Crosshair & Dynamic Values Tooltip
  if (!isDragging && hoverX !== null && hoverX >= b.padding.left && hoverX <= b.padding.left + b.graphWidth) {
    const hoverTs = minTs + ((hoverX - b.padding.left) / b.graphWidth) * tsSpan;

    // Do NOT pop up tooltip if hovering over an outage band (red downtime bar)
    const isDowntime = downtimes.some(dw => hoverTs >= dw.start && hoverTs <= dw.end);

    if (!isDowntime) {
      // Determine maximum allowable distance to nearest real data point
      let maxAllowableDiff = 60000;
      if (primarySeries.length >= 2) {
        const gaps = [];
        for (let i = 1; i < primarySeries.length; i++) {
          const d = getPtTs(primarySeries[i]) - getPtTs(primarySeries[i - 1]);
          if (d > 0) gaps.push(d);
        }
        if (gaps.length > 0) {
          gaps.sort((a, b) => a - b);
          const normalInterval = Math.max(15000, gaps[Math.floor(gaps.length * 0.15)] || gaps[0]);
          maxAllowableDiff = normalInterval * 2.5;
        }
      }

      // Find closest points for each series within acceptable range
      const tooltipRows = [];
      let anyValidPointInRange = false;

      seriesList.forEach(s => {
        const data = s.data || [];
        if (data.length === 0) return;
        let closest = null;
        let minDiff = Infinity;
        for (let k = 0; k < data.length; k++) {
          const pt = data[k];
          const ts = getPtTs(pt);
          const diff = Math.abs(ts - hoverTs);
          if (diff < minDiff) {
            minDiff = diff;
            closest = pt;
          }
        }
        if (closest && minDiff <= maxAllowableDiff) {
          const val = getPtVal(closest);
          if (val !== null && typeof val === 'number' && !isNaN(val)) {
            anyValidPointInRange = true;
            tooltipRows.push({
              label: s.label || 'Metric',
              color: resolveColor(s.color),
              val: formatter(val)
            });
          }
        }
      });

      // Only render crosshair & tooltip popup if there is valid data at this location (not blank data)
      if (anyValidPointInRange && tooltipRows.length > 0) {
        ctx.strokeStyle = crosshairColor;
        ctx.lineWidth = 1;
        ctx.setLineDash([3, 3]);
        ctx.beginPath();
        ctx.moveTo(hoverX, b.padding.top);
        ctx.lineTo(hoverX, b.padding.top + b.graphHeight);
        ctx.stroke();
        ctx.setLineDash([]);

        const timeHeader = formatTimeAxis(hoverTs, tsSpan);
        ctx.font = '600 11px ' + monoFont;
        let maxTextW = ctx.measureText(timeHeader).width;
        ctx.font = '11px ' + monoFont;
        tooltipRows.forEach(r => {
          const rowText = r.label + ': ' + r.val;
          const w = ctx.measureText(rowText).width + 16;
          if (w > maxTextW) maxTextW = w;
        });

        const ttWidth = Math.max(120, maxTextW + 24);
        const rowH = 17;
        const ttHeight = 26 + tooltipRows.length * rowH + 6;

        let ttX = hoverX + 12;
        if (ttX + ttWidth > b.width - b.padding.right) {
          ttX = hoverX - ttWidth - 12;
        }
        let ttY = b.padding.top + 6;

        ctx.fillStyle = ttBg;
        ctx.strokeStyle = ttBorder;
        ctx.lineWidth = 1;

        const cr = 8;
        ctx.beginPath();
        ctx.moveTo(ttX + cr, ttY);
        ctx.lineTo(ttX + ttWidth - cr, ttY);
        ctx.quadraticCurveTo(ttX + ttWidth, ttY, ttX + ttWidth, ttY + cr);
        ctx.lineTo(ttX + ttWidth, ttY + ttHeight - cr);
        ctx.quadraticCurveTo(ttX + ttWidth, ttY + ttHeight, ttX + ttWidth - cr, ttY + ttHeight);
        ctx.lineTo(ttX + cr, ttY + ttHeight);
        ctx.quadraticCurveTo(ttX, ttY + ttHeight, ttX, ttY + ttHeight - cr);
        ctx.lineTo(ttX, ttY + cr);
        ctx.quadraticCurveTo(ttX, ttY, ttX + cr, ttY);
        ctx.closePath();
        ctx.fill();
        ctx.stroke();

        ctx.fillStyle = ttHeader;
        ctx.font = '600 11px ' + monoFont;
        ctx.textAlign = 'left';
        ctx.fillText(timeHeader, ttX + 10, ttY + 17);

        tooltipRows.forEach((r, idx) => {
          const y = ttY + 36 + idx * rowH;
          ctx.fillStyle = r.color;
          ctx.fillRect(ttX + 10, y - 7, 8, 3);

          ctx.fillStyle = ttLabel;
          ctx.font = '11px ' + bodyFont;
          ctx.fillText(r.label, ttX + 24, y);

          ctx.fillStyle = ttVal;
          ctx.font = '500 11px ' + monoFont;
          ctx.textAlign = 'right';
          ctx.fillText(r.val, ttX + ttWidth - 10, y);
          ctx.textAlign = 'left';
        });
      }
    }
  }
}
