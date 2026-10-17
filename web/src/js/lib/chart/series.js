// Convert raw metric delta points to instantaneous rates in Bytes/second
export function convertDeltaToRate(pts) {
  if (!pts || pts.length === 0) return [];
  return pts.map((pt, j) => {
    const ts = Array.isArray(pt) ? pt[0] : (pt.timestamp ?? pt.time);
    const v = Array.isArray(pt) ? (pt[1] ?? 0) : (pt.value ?? 0);

    let prevTs = null;
    if (j > 0) {
      prevTs = Array.isArray(pts[j - 1]) ? pts[j - 1][0] : (pts[j - 1].timestamp ?? pts[j - 1].time);
    }

    if (prevTs === null) {
      let nextTs = ts + 60000;
      if (pts.length > 1) {
        nextTs = Array.isArray(pts[1]) ? pts[1][0] : (pts[1].timestamp ?? pts[1].time);
      }
      const dt = Math.max(1, (nextTs - ts) / 1000);
      return [ts, dt > 0 ? Math.max(0, v / dt) : 0];
    }
    const dt = Math.max(1, (ts - prevTs) / 1000);
    return [ts, dt > 0 ? Math.max(0, v / dt) : 0];
  });
}

export function getPtTs(pt) {
  if (!pt) return 0;
  if (pt.timestamp != null) return pt.timestamp;
  if (pt.time != null) return pt.time;
  if (Array.isArray(pt)) return pt[0];
  return 0;
}

export function getPtVal(pt) {
  if (!pt) return null;
  if (pt.value !== undefined) return pt.value;
  if (Array.isArray(pt)) return pt[1];
  return null;
}

// Downtime Gap Detection Algorithm
export function calculateDowntimes(points, queryEndTime, tsSpan) {
  if (!points || points.length === 0) return [];
  const expectedStep = tsSpan ? Math.max(60000, tsSpan / 1000) : 60000;
  const threshold = Math.max(180000, expectedStep * 2.5);

  const intervals = [];
  for (let i = 1; i < points.length; i++) {
    const prev = getPtTs(points[i - 1]);
    const curr = getPtTs(points[i]);
    const diff = curr - prev;
    if (diff > threshold) {
      intervals.push({
        start: prev + expectedStep,
        end: curr - expectedStep
      });
    }
  }

  if (queryEndTime && points.length > 0) {
    const last = getPtTs(points[points.length - 1]);
    const tailThreshold = Math.max(180000, threshold * 1.5);
    if (queryEndTime > last + tailThreshold) {
      intervals.push({
        start: last + expectedStep,
        end: queryEndTime
      });
    }
  }
  return intervals;
}

// Build chart series with null points across outages
export function buildCleanSeries(points, queryEndTime, tsSpan) {
  if (!points || points.length < 2) return points || [];
  const expectedStep = tsSpan ? Math.max(60000, tsSpan / 1000) : 60000;
  const threshold = Math.max(180000, expectedStep * 2.5);

  const result = [];
  for (let i = 0; i < points.length; i++) {
    if (i > 0) {
      const prev = getPtTs(points[i - 1]);
      const curr = getPtTs(points[i]);
      const diff = curr - prev;
      if (diff > threshold) {
        result.push({ timestamp: prev + expectedStep, value: null });
        result.push({ timestamp: curr - expectedStep, value: null });
      }
    }
    result.push({ timestamp: getPtTs(points[i]), value: getPtVal(points[i]) });
  }
  if (queryEndTime && points.length > 0) {
    const last = getPtTs(points[points.length - 1]);
    const tailThreshold = Math.max(180000, threshold * 1.5);
    if (queryEndTime > last + tailThreshold) {
      result.push({ timestamp: last + expectedStep, value: null });
      result.push({ timestamp: queryEndTime, value: null });
    }
  }
  return result;
}
