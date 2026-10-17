// chart is the renderMultiChart state; see index.js.
export function getBounds(chart) {
  const { canvas, leftPad } = chart;
  const width = canvas.clientWidth || 300;
  const height = canvas.clientHeight || 180;
  const padding = { top: 16, right: 12, bottom: 24, left: leftPad };
  return {
    width: width,
    height: height,
    padding: padding,
    graphWidth: Math.max(10, width - padding.left - padding.right),
    graphHeight: Math.max(10, height - padding.top - padding.bottom)
  };
}

export function getTimeRange(chart) {
  const { customRange, hours, queryEndTime, seriesList } = chart;
  if (customRange && customRange.start && customRange.end && customRange.end > customRange.start) {
    return { minTs: customRange.start, maxTs: customRange.end };
  }
  if (hours !== null && hours > 0) {
    const maxTs = queryEndTime || Date.now();
    const minTs = maxTs - hours * 3600 * 1000;
    return { minTs: minTs, maxTs: maxTs };
  }
  let minTs = Infinity;
  let maxTs = -Infinity;
  seriesList.forEach(s => {
    (s.data || []).forEach(p => {
      if (p.timestamp != null) {
        if (p.timestamp < minTs) minTs = p.timestamp;
        if (p.timestamp > maxTs) maxTs = p.timestamp;
      }
    });
  });
  if (minTs === Infinity || maxTs === -Infinity) {
    const now = queryEndTime || Date.now();
    return { minTs: now - 6 * 3600 * 1000, maxTs: now };
  }
  if (queryEndTime && queryEndTime > maxTs) maxTs = queryEndTime;
  return { minTs: minTs, maxTs: Math.max(maxTs, minTs + 60000) };
}
