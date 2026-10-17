import { getPtTs, getPtVal } from '../../../lib/chart/series.js';
import { readToken, resolveColor, withAlpha, tracePath, prepareCanvas } from '../../../lib/chart/canvas.js';

// Small axis-less trend line for cards. points: [[ts, value], ...] or {timestamp, value}.
export function drawSparkline(canvas, points, options) {
  options = options || {};
  const c = prepareCanvas(canvas);
  if (!c) return;
  const ctx = c.ctx;
  const data = (points || []).map(p => ({ t: getPtTs(p), v: getPtVal(p) })).filter(p => typeof p.v === 'number' && !isNaN(p.v));
  if (data.length < 2) {
    ctx.strokeStyle = readToken('--grid');
    ctx.setLineDash([3, 4]);
    ctx.beginPath();
    ctx.moveTo(0, c.h - 2);
    ctx.lineTo(c.w, c.h - 2);
    ctx.stroke();
    return;
  }
  const end = Date.now();
  const start = end - (options.hours || 24) * 3600 * 1000;
  const max = options.max || Math.max.apply(null, data.map(p => p.v)) * 1.15 || 1;
  const pad = 4;
  const X = t => Math.max(0, Math.min(c.w - pad, ((t - start) / (end - start)) * (c.w - pad)));
  const Y = v => pad + (c.h - pad * 2) * (1 - Math.min(v, max) / max);
  const color = resolveColor(options.color || '--s1');

  // Split into segments at gaps larger than 3x the typical step
  const steps = data.slice(1).map((p, i) => p.t - data[i].t).sort((a, b) => a - b);
  const step = steps[Math.floor(steps.length / 2)] || 60000;
  const segs = [[]];
  data.forEach((p, i) => {
    if (i > 0 && p.t - data[i - 1].t > step * 3) segs.push([]);
    segs[segs.length - 1].push({ x: X(p.t), y: Y(p.v) });
  });

  segs.filter(sg => sg.length > 1).forEach(sg => {
    ctx.beginPath();
    ctx.moveTo(sg[0].x, c.h);
    tracePath(ctx, sg, false);
    ctx.lineTo(sg[sg.length - 1].x, c.h);
    ctx.closePath();
    ctx.fillStyle = withAlpha(color, 0.16);
    ctx.fill();
    ctx.beginPath();
    tracePath(ctx, sg, true);
    ctx.strokeStyle = color;
    ctx.lineWidth = 1.75;
    ctx.lineJoin = 'round';
    ctx.stroke();
  });

  const last = data[data.length - 1];
  if (end - last.t < step * 3) {
    ctx.beginPath();
    ctx.arc(X(last.t), Y(last.v), 3, 0, Math.PI * 2);
    ctx.fillStyle = color;
    ctx.fill();
    ctx.lineWidth = 2;
    ctx.strokeStyle = readToken('--surface');
    ctx.stroke();
  }
}
