import { SPARK_HOURS, SPARK_MAX_AGE_MS, SPARK_STAGGER_MS } from '../shared/constants.js';

// Row sparklines.
export function createSparklines(ctx) {
  const { sparklines } = ctx;
  const s = ctx.state;

  function drawSparkline(monitorId) {
    const entry = s.rowElements.get(monitorId);
    const cached = sparklines.cache.get(monitorId);
    if (!entry || !cached) return;
    window.CertainStatsChart.drawSparkline?.(entry.spark, cached.points, { color: '--s1', hours: SPARK_HOURS });
  }

  // Draws new rows from the cache and fetches stale histories, one request
  // per monitor like the agent cards' sparklines.
  function updateSparklines(monitorIds) {
    const stale = [];
    for (const monitorId of monitorIds) {
      const cached = sparklines.cache.get(monitorId);
      if (cached) drawSparkline(monitorId);
      if ((!cached || Date.now() - cached.fetchedAt > SPARK_MAX_AGE_MS) && !sparklines.pending.has(monitorId)) stale.push(monitorId);
    }
    if (!stale.length || !ctx.visible()) return;

    if (!sparklines.controller) sparklines.controller = new AbortController();
    const signal = sparklines.controller.signal;
    stale.forEach((monitorId, index) => {
      sparklines.pending.add(monitorId);
      const timer = setTimeout(async () => {
        if (signal.aborted) return;
        let points = [];
        try {
          const data = await ctx.api('network-monitors/' + encodeURIComponent(monitorId) + '/history?hours=' + SPARK_HOURS, { signal });
          points = data.points.map(point => ({ timestamp: point.timestamp, value: point.response_avg_ms }));
        } catch (e) {
          if (e.name === 'AbortError' || signal.aborted) return;
        } finally {
          sparklines.pending.delete(monitorId);
        }
        sparklines.cache.set(monitorId, { points, fetchedAt: Date.now() });
        drawSparkline(monitorId);
      }, index * SPARK_STAGGER_MS);
      signal.addEventListener('abort', () => clearTimeout(timer), { once: true });
    });
  }

  function redrawSparklines() {
    for (const monitorId of s.rowElements.keys()) drawSparkline(monitorId);
  }

  return { drawSparkline, updateSparklines, redrawSparklines };
}
