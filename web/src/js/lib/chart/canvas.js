// Theme tokens: series colors may be passed as '--s1', 'var(--s1)' or a literal color.
export function readToken(name) {
  return getComputedStyle(document.documentElement).getPropertyValue(name).trim();
}

export function resolveColor(c, fallback) {
  if (!c) return readToken(fallback || '--s1');
  if (c.startsWith('var(')) c = c.slice(4, -1).trim();
  if (c.startsWith('--')) return readToken(c) || readToken(fallback || '--s1');
  return c;
}

export function withAlpha(color, a) {
  const m = /^#([0-9a-f]{3}|[0-9a-f]{6})$/i.exec(color);
  if (!m) return color;
  let h = m[1];
  if (h.length === 3) h = h.split('').map(x => x + x).join('');
  const n = parseInt(h, 16);
  return 'rgba(' + (n >> 16 & 255) + ',' + (n >> 8 & 255) + ',' + (n & 255) + ',' + a + ')';
}

// Smooth path through points using horizontal midpoint bezier segments (no overshoot in y).
export function tracePath(ctx, pts, moveFirst) {
  pts.forEach((p, i) => {
    if (i === 0) {
      if (moveFirst) ctx.moveTo(p.x, p.y); else ctx.lineTo(p.x, p.y);
      return;
    }
    const prev = pts[i - 1];
    const mx = (prev.x + p.x) / 2;
    ctx.bezierCurveTo(mx, prev.y, mx, p.y, p.x, p.y);
  });
}

export function prepareCanvas(canvas) {
  const dpr = window.devicePixelRatio || 1;
  const w = canvas.clientWidth, h = canvas.clientHeight;
  if (!w || !h) return null;
  canvas.width = Math.round(w * dpr);
  canvas.height = Math.round(h * dpr);
  const ctx = canvas.getContext('2d');
  ctx.setTransform(dpr, 0, 0, dpr, 0, 0);
  ctx.clearRect(0, 0, w, h);
  return { ctx: ctx, w: w, h: h };
}
