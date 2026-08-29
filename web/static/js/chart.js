/* CertainStats — Enhanced Multi-Series Vanilla JS Telemetry Chart Renderer with Collision-Proof X Labels, Scoped Brush Zoom & Downtime Detection */

(function () {
  'use strict';

  function formatBytes(bytes) {
    if (!bytes || bytes === 0) return '0 B';
    const units = ['B', 'KB', 'MB', 'GB', 'TB'];
    let v = bytes, i = 0;
    while (v >= 1024 && i < units.length - 1) { v /= 1024; i++; }
    return v.toFixed(1) + ' ' + (units[i] || 'TB');
  }

  function formatBps(bps) {
    if (!bps || bps === 0) return '0 B/s';
    const units = ['B/s', 'KB/s', 'MB/s', 'GB/s'];
    let v = bps, i = 0;
    while (v >= 1024 && i < units.length - 1) { v /= 1024; i++; }
    return v.toFixed(1) + ' ' + (units[i] || 'GB/s');
  }

  function formatTimeAxis(timestamp, spanMs) {
    const d = new Date(timestamp);
    const months = ['Jan', 'Feb', 'Mar', 'Apr', 'May', 'Jun', 'Jul', 'Aug', 'Sep', 'Oct', 'Nov', 'Dec'];
    const pad = (n) => (n < 10 ? '0' + n : n);

    if (spanMs <= 24 * 3600 * 1000) {
      return pad(d.getHours()) + ':' + pad(d.getMinutes());
    }
    if (spanMs <= 7 * 86400 * 1000) {
      return months[d.getMonth()] + ' ' + d.getDate() + ' ' + pad(d.getHours()) + ':' + pad(d.getMinutes());
    }
    if (spanMs <= 90 * 86400 * 1000) {
      return months[d.getMonth()] + ' ' + d.getDate();
    }
    return d.getFullYear() + '-' + pad(d.getMonth() + 1) + '-' + pad(d.getDate());
  }

  // Convert raw metric delta points to instantaneous rates in Bytes/second
  function convertDeltaToRate(pts) {
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

  function getPtTs(pt) {
    if (!pt) return 0;
    if (pt.timestamp != null) return pt.timestamp;
    if (pt.time != null) return pt.time;
    if (Array.isArray(pt)) return pt[0];
    return 0;
  }

  function getPtVal(pt) {
    if (!pt) return null;
    if (pt.value !== undefined) return pt.value;
    if (Array.isArray(pt)) return pt[1];
    return null;
  }

  // Downtime Gap Detection Algorithm
  function calculateDowntimes(points, queryEndTime, tsSpan) {
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
  function buildCleanSeries(points, queryEndTime, tsSpan) {
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

  window.CertainStatsChart = {
    getPtTs: getPtTs,
    getPtVal: getPtVal,
    formatBytes: formatBytes,
    formatBps: formatBps,
    convertDeltaToRate: convertDeltaToRate,
    calculateDowntimes: calculateDowntimes,

    renderMultiChart: function (canvasId, options) {
      const canvas = document.getElementById(canvasId);
      if (!canvas) return null;

      const ctx = canvas.getContext('2d');
      let seriesList = options.seriesList || [];
      const formatter = options.formatter || ((v) => v.toFixed(1) + (options.unit || ''));
      let hours = options.hours || 6;
      let customRange = options.customRange || null;
      let yMax = (options.yMax !== undefined && options.yMax !== null) ? options.yMax : null;
      let maxAdd = (options.maxAdd !== undefined && options.maxAdd !== null) ? options.maxAdd : null;
      let maxCap = (options.maxCap !== undefined && options.maxCap !== null) ? options.maxCap : null;
      let minMax = (options.minMax !== undefined && options.minMax !== null) ? options.minMax : 1;
      const onZoom = options.onZoom || null;
      let queryEndTime = options.queryEndTime || Date.now();

      // Brush Selection State
      let isDragging = false;
      let dragStartX = null;
      let currentDragX = null;
      let hoverX = null;

      function getBounds() {
        const width = canvas.clientWidth || 300;
        const height = canvas.clientHeight || 180;
        const padding = { top: 25, right: 20, bottom: 25, left: 55 };
        return {
          width: width,
          height: height,
          padding: padding,
          graphWidth: Math.max(10, width - padding.left - padding.right),
          graphHeight: Math.max(10, height - padding.top - padding.bottom)
        };
      }

      function getTimeRange() {
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

      function draw() {
        const b = getBounds();
        if (b.width === 0 || b.height === 0) return;

        canvas.width = b.width * window.devicePixelRatio;
        canvas.height = b.height * window.devicePixelRatio;
        ctx.scale(window.devicePixelRatio, window.devicePixelRatio);
        ctx.clearRect(0, 0, b.width, b.height);

        const tr = getTimeRange();
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

        const isLight = document.documentElement.getAttribute('data-theme') === 'light';
        const gridColor = isLight ? 'rgba(0, 0, 0, 0.08)' : 'rgba(255, 255, 255, 0.06)';
        const axisTextColor = isLight ? '#64748b' : '#8e909a';
        const crosshairColor = isLight ? 'rgba(0, 0, 0, 0.35)' : 'rgba(255, 255, 255, 0.25)';
        const ttBg = isLight ? 'rgba(255, 255, 255, 0.96)' : 'rgba(15, 17, 23, 0.94)';
        const ttBorder = isLight ? 'rgba(0, 0, 0, 0.15)' : 'rgba(255, 255, 255, 0.15)';
        const ttHeader = isLight ? '#64748b' : '#8e909a';
        const ttLabel = isLight ? '#475569' : '#9ca3af';
        const ttVal = isLight ? '#0f172a' : '#ffffff';

        if (!hasPoints) {
          ctx.fillStyle = axisTextColor;
          ctx.font = '12px Inter, sans-serif';
          ctx.textAlign = 'center';
          ctx.fillText('No telemetry data available', b.width / 2, b.height / 2);
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
          ctx.font = '10px JetBrains Mono, monospace';
          ctx.textAlign = 'right';
          ctx.fillText(formatter(val), b.padding.left - 8, y + 3);
        }

        // 2. Draw Downtime Outage Bands
        const primarySeries = (seriesList[0] && seriesList[0].data) ? seriesList[0].data : [];
        const downtimes = calculateDowntimes(primarySeries, queryEndTime, tsSpan);
        downtimes.forEach(dw => {
          const x1 = Math.max(b.padding.left, b.padding.left + ((dw.start - minTs) / tsSpan) * b.graphWidth);
          const x2 = Math.min(b.padding.left + b.graphWidth, b.padding.left + ((dw.end - minTs) / tsSpan) * b.graphWidth);
          const w = Math.max(2, x2 - x1);

          ctx.fillStyle = 'rgba(239, 68, 68, 0.16)';
          ctx.fillRect(x1, b.padding.top, w, b.graphHeight);

          ctx.strokeStyle = 'rgba(239, 68, 68, 0.4)';
          ctx.setLineDash([3, 3]);
          ctx.beginPath();
          ctx.moveTo(x1, b.padding.top);
          ctx.lineTo(x1, b.padding.top + b.graphHeight);
          ctx.moveTo(x2, b.padding.top);
          ctx.lineTo(x2, b.padding.top + b.graphHeight);
          ctx.stroke();
          ctx.setLineDash([]);
        });

        // 3. Draw Metric Series Paths
        seriesList.forEach(series => {
          const rawData = series.data || [];
          if (rawData.length === 0) return;
          const cleanData = buildCleanSeries(rawData, queryEndTime, tsSpan);
          const color = series.color || '#6366f1';

          // Area Gradient
          if (series.fill) {
            let started = false;
            let firstX = b.padding.left;
            let lastX = b.padding.left;

            cleanData.forEach(p => {
              if (p.value === null || isNaN(p.value)) {
                if (started) {
                  ctx.lineTo(lastX, b.padding.top + b.graphHeight);
                  ctx.closePath();
                  const grad = ctx.createLinearGradient(0, b.padding.top, 0, b.padding.top + b.graphHeight);
                  grad.addColorStop(0, color + '44');
                  grad.addColorStop(1, color + '00');
                  ctx.fillStyle = grad;
                  ctx.fill();
                  started = false;
                }
                return;
              }

              const clampedTs = Math.max(minTs, Math.min(maxTs, p.timestamp));
              const x = b.padding.left + ((clampedTs - minTs) / tsSpan) * b.graphWidth;
              const y = b.padding.top + b.graphHeight - (Math.min(p.value, effectiveMax) / effectiveMax) * b.graphHeight;

              if (!started) {
                ctx.beginPath();
                ctx.moveTo(x, b.padding.top + b.graphHeight);
                ctx.lineTo(x, y);
                firstX = x;
                started = true;
              } else {
                ctx.lineTo(x, y);
              }
              lastX = x;
            });

            if (started) {
              ctx.lineTo(lastX, b.padding.top + b.graphHeight);
              ctx.closePath();
              const grad = ctx.createLinearGradient(0, b.padding.top, 0, b.padding.top + b.graphHeight);
              grad.addColorStop(0, color + '44');
              grad.addColorStop(1, color + '00');
              ctx.fillStyle = grad;
              ctx.fill();
            }
          }

          // Series Stroke Line
          ctx.strokeStyle = color;
          ctx.lineWidth = 1.75;
          ctx.beginPath();
          let lineStarted = false;

          cleanData.forEach(p => {
            if (p.value === null || isNaN(p.value)) {
              lineStarted = false;
              return;
            }

            const clampedTs = Math.max(minTs, Math.min(maxTs, p.timestamp));
            const x = b.padding.left + ((clampedTs - minTs) / tsSpan) * b.graphWidth;
            const y = b.padding.top + b.graphHeight - (Math.min(p.value, effectiveMax) / effectiveMax) * b.graphHeight;

            if (!lineStarted) {
              ctx.moveTo(x, y);
              lineStarted = true;
            } else {
              ctx.lineTo(x, y);
            }
          });
          ctx.stroke();
        });

        // 4. Time Axis Ticks with Strict Non-Overlapping Guarantee
        ctx.font = '10px JetBrains Mono, monospace';
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

        // 5. Drag Brush Selection Overlay
        if (isDragging && dragStartX !== null && currentDragX !== null) {
          const start = Math.max(b.padding.left, Math.min(dragStartX, currentDragX));
          const end = Math.min(b.padding.left + b.graphWidth, Math.max(dragStartX, currentDragX));
          const w = Math.max(0, end - start);

          ctx.fillStyle = 'rgba(99, 102, 241, 0.22)';
          ctx.fillRect(start, b.padding.top, w, b.graphHeight);

          ctx.strokeStyle = 'rgba(99, 102, 241, 0.9)';
          ctx.lineWidth = 1.5;
          ctx.strokeRect(start, b.padding.top, w, b.graphHeight);
        }

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
                    color: s.color || '#6366f1',
                    val: formatter(val)
                  });
                }
              }
            });

            // Only render crosshair & tooltip popup if there is valid data at this location (not blank data)
            if (anyValidPointInRange && tooltipRows.length > 0) {
              ctx.strokeStyle = crosshairColor;
              ctx.setLineDash([3, 3]);
              ctx.beginPath();
              ctx.moveTo(hoverX, b.padding.top);
              ctx.lineTo(hoverX, b.padding.top + b.graphHeight);
              ctx.stroke();
              ctx.setLineDash([]);

              const timeHeader = formatTimeAxis(hoverTs, tsSpan);
              ctx.font = 'bold 10px JetBrains Mono, monospace';
              let maxTextW = ctx.measureText(timeHeader).width;
              ctx.font = '10px JetBrains Mono, monospace';
              tooltipRows.forEach(r => {
                const rowText = r.label + ': ' + r.val;
                const w = ctx.measureText(rowText).width + 16;
                if (w > maxTextW) maxTextW = w;
              });

              const ttWidth = Math.max(110, maxTextW + 20);
              const rowH = 14;
              const ttHeight = 20 + tooltipRows.length * rowH + 6;

              let ttX = hoverX + 12;
              if (ttX + ttWidth > b.width - b.padding.right) {
                ttX = hoverX - ttWidth - 12;
              }
              let ttY = b.padding.top + 6;

              ctx.fillStyle = ttBg;
              ctx.strokeStyle = ttBorder;
              ctx.lineWidth = 1;

              const cr = 6;
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
              ctx.font = 'bold 10px JetBrains Mono, monospace';
              ctx.textAlign = 'left';
              ctx.fillText(timeHeader, ttX + 8, ttY + 13);

              tooltipRows.forEach((r, idx) => {
                const y = ttY + 26 + idx * rowH;
                ctx.fillStyle = r.color;
                ctx.beginPath();
                ctx.arc(ttX + 11, y - 3, 3, 0, Math.PI * 2);
                ctx.fill();

                ctx.fillStyle = ttLabel;
                ctx.font = '10px JetBrains Mono, monospace';
                ctx.fillText(r.label + ':', ttX + 18, y);

                ctx.fillStyle = ttVal;
                ctx.font = 'bold 10px JetBrains Mono, monospace';
                ctx.textAlign = 'right';
                ctx.fillText(r.val, ttX + ttWidth - 8, y);
                ctx.textAlign = 'left';
              });
            }
          }
        }
      }

      function getCanvasRelativeX(clientX) {
        const rect = canvas.getBoundingClientRect();
        return clientX - rect.left;
      }

      function handleDragStart(clientX) {
        const b = getBounds();
        const x = getCanvasRelativeX(clientX);
        if (x >= b.padding.left && x <= b.padding.left + b.graphWidth) {
          isDragging = true;
          dragStartX = x;
          currentDragX = x;
        }
      }

      function handleDragMove(clientX) {
        const b = getBounds();
        const x = getCanvasRelativeX(clientX);
        hoverX = x;

        if (isDragging) {
          currentDragX = Math.max(b.padding.left, Math.min(b.padding.left + b.graphWidth, x));
          draw();
        }
      }

      function handleDragEnd() {
        if (!isDragging) return;
        isDragging = false;
        const b = getBounds();
        if (dragStartX !== null && currentDragX !== null && Math.abs(currentDragX - dragStartX) > 8) {
          const x1 = Math.min(dragStartX, currentDragX);
          const x2 = Math.max(dragStartX, currentDragX);
          const tr = getTimeRange();
          const span = tr.maxTs - tr.minTs;

          const startMs = Math.round(tr.minTs + ((x1 - b.padding.left) / b.graphWidth) * span);
          const endMs = Math.round(tr.minTs + ((x2 - b.padding.left) / b.graphWidth) * span);

          if (onZoom && startMs < endMs) {
            onZoom(startMs, endMs);
          }
        }
        dragStartX = null;
        currentDragX = null;
        draw();
      }

      // Mouse Events
      canvas.addEventListener('mousedown', function (e) {
        handleDragStart(e.clientX);
      });

      window.addEventListener('mousemove', function (e) {
        if (isDragging) {
          handleDragMove(e.clientX);
        }
      });

      canvas.addEventListener('mousemove', function (e) {
        if (!isDragging) {
          hoverX = getCanvasRelativeX(e.clientX);
          draw();
        }
      });

      canvas.addEventListener('mouseleave', function () {
        if (!isDragging) {
          hoverX = null;
          draw();
        }
      });

      window.addEventListener('mouseup', function () {
        if (isDragging) {
          handleDragEnd();
        }
      });

      // Touch Events (Mobile Drag Zoom)
      canvas.addEventListener('touchstart', function (e) {
        if (e.touches && e.touches[0]) {
          handleDragStart(e.touches[0].clientX);
        }
      }, { passive: true });

      canvas.addEventListener('touchmove', function (e) {
        if (isDragging && e.touches && e.touches[0]) {
          handleDragMove(e.touches[0].clientX);
          e.preventDefault();
        }
      }, { passive: false });

      canvas.addEventListener('touchend', function () {
        if (isDragging) {
          handleDragEnd();
        }
      });

      draw();

      const onThemeChange = () => draw();
      window.addEventListener('resize', draw);
      window.addEventListener('certainstats_theme_change', onThemeChange);

      return {
        updateSeries: function (newSeriesList, newHours, newYMax, newQueryEndTime, newCustomRange, newMaxAdd) {
          seriesList = newSeriesList || [];
          if (newHours !== undefined) hours = newHours;
          if (newYMax !== undefined) yMax = (newYMax !== null) ? newYMax : null;
          if (newQueryEndTime !== undefined) queryEndTime = newQueryEndTime;
          if (newCustomRange !== undefined) customRange = newCustomRange;
          if (newMaxAdd !== undefined) maxAdd = newMaxAdd;
          draw();
        },

        updateLivePoint: function (timestamp, valuesMap) {
          if (!valuesMap) return;
          if (customRange) return; // Freeze live chart mutation while viewing a custom zoomed range
          let needsRedraw = false;
          const ts = (typeof timestamp === 'number' && !isNaN(timestamp)) ? timestamp : (new Date(timestamp).getTime() || Date.now());

          const durationMs = (hours !== null && hours > 0) ? hours * 3600 * 1000 : 6 * 3600 * 1000;
          const stepMs = Math.max(15000, durationMs / 1000);

          seriesList.forEach(s => {
            let val = valuesMap[s.label];
            if (val === undefined) {
              const labelLower = (s.label || '').toLowerCase();
              for (const [k, v] of Object.entries(valuesMap)) {
                if (k.toLowerCase() === labelLower) {
                  val = v;
                  break;
                }
              }
            }

            if (val !== undefined && typeof val === 'number' && !isNaN(val)) {
              if (!s.data) s.data = [];
              const len = s.data.length;
              if (len > 0) {
                const lastTs = getPtTs(s.data[len - 1]);
                if (Math.abs(ts - lastTs) < stepMs) {
                  if (Array.isArray(s.data[len - 1])) {
                    s.data[len - 1][1] = val;
                  } else {
                    s.data[len - 1].value = val;
                  }
                } else {
                  s.data.push({ timestamp: ts, value: val });
                  if (s.data.length > 1200) {
                    s.data.shift();
                  }
                }
              } else {
                s.data.push({ timestamp: ts, value: val });
              }
              needsRedraw = true;
            }
          });

          if (needsRedraw) {
            queryEndTime = Math.max(queryEndTime || 0, ts);
            draw();
          }
        },

        destroy: function () {
          window.removeEventListener('resize', draw);
          window.removeEventListener('certainstats_theme_change', onThemeChange);
        }
      };
    },

    renderChart: function (canvasId, options) {
      return this.renderMultiChart(canvasId, {
        seriesList: [{
          label: options.label || 'Metric',
          color: options.color || '#6366f1',
          fill: true,
          data: options.data || []
        }],
        formatter: options.formatter || ((v) => v.toFixed(1) + (options.unit || '')),
        hours: options.hours || 6,
        customRange: options.customRange || null,
        yMax: (options.yMax !== undefined && options.yMax !== null) ? options.yMax : null,
        onZoom: options.onZoom || null
      });
    },

    formatUptime: function (seconds) {
      if (!seconds || seconds <= 0) return '0m';
      const s = Math.floor(seconds);
      const days = Math.floor(s / 86400);
      const hours = Math.floor((s % 86400) / 3600);
      const minutes = Math.floor((s % 3600) / 60);

      if (days > 0) {
        return `${days}d ${hours}h`;
      }
      if (hours > 0) {
        return `${hours}h ${minutes}m`;
      }
      return `${minutes}m`;
    },

    initThemeToggle: function () {
      const toggleBtns = document.querySelectorAll('#theme-toggle, #theme-toggle-desktop');
      if (!toggleBtns || toggleBtns.length === 0) return;

      const currentTheme = localStorage.getItem('certainstats_theme') || 'dark';
      document.documentElement.setAttribute('data-theme', currentTheme);

      toggleBtns.forEach(btn => {
        btn.addEventListener('click', function () {
          const now = document.documentElement.getAttribute('data-theme') === 'light' ? 'dark' : 'light';
          document.documentElement.setAttribute('data-theme', now);
          localStorage.setItem('certainstats_theme', now);
          window.dispatchEvent(new CustomEvent('certainstats_theme_change', { detail: { theme: now } }));
        });
      });
    }
  };

  document.addEventListener('DOMContentLoaded', function () {
    window.CertainStatsChart.initThemeToggle();
  });
})();
