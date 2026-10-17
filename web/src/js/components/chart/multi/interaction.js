import { getBounds, getTimeRange } from './geometry.js';
import { draw } from './draw.js';

// Brush-to-zoom and hover tracking; listen() ties every listener to the chart's lifetime.
export function bindInteraction(chart, listen) {
  const { canvas } = chart;
  function getCanvasRelativeX(clientX) {
    const rect = canvas.getBoundingClientRect();
    return clientX - rect.left;
  }

  function handleDragStart(clientX) {
    const b = getBounds(chart);
    const x = getCanvasRelativeX(clientX);
    if (x >= b.padding.left && x <= b.padding.left + b.graphWidth) {
      chart.isDragging = true;
      chart.dragStartX = x;
      chart.currentDragX = x;
    }
  }

  function handleDragMove(clientX) {
    const b = getBounds(chart);
    const x = getCanvasRelativeX(clientX);
    chart.hoverX = x;

    if (chart.isDragging) {
      chart.currentDragX = Math.max(b.padding.left, Math.min(b.padding.left + b.graphWidth, x));
      draw(chart);
    }
  }

  function handleDragEnd() {
    if (!chart.isDragging) return;
    chart.isDragging = false;
    const b = getBounds(chart);
    if (chart.dragStartX !== null && chart.currentDragX !== null && Math.abs(chart.currentDragX - chart.dragStartX) > 8) {
      const x1 = Math.min(chart.dragStartX, chart.currentDragX);
      const x2 = Math.max(chart.dragStartX, chart.currentDragX);
      const tr = getTimeRange(chart);
      const span = tr.maxTs - tr.minTs;

      const startMs = Math.round(tr.minTs + ((x1 - b.padding.left) / b.graphWidth) * span);
      const endMs = Math.round(tr.minTs + ((x2 - b.padding.left) / b.graphWidth) * span);

      if (chart.onZoom && startMs < endMs) {
        chart.onZoom(startMs, endMs);
      }
    }
    chart.dragStartX = null;
    chart.currentDragX = null;
    draw(chart);
  }

  // Mouse Events
  listen(canvas, 'mousedown', function (e) {
    handleDragStart(e.clientX);
  });

  listen(window, 'mousemove', function (e) {
    if (chart.isDragging) {
      handleDragMove(e.clientX);
    }
  });

  listen(canvas, 'mousemove', function (e) {
    if (!chart.isDragging) {
      chart.hoverX = getCanvasRelativeX(e.clientX);
      draw(chart);
    }
  });

  listen(canvas, 'mouseleave', function () {
    if (!chart.isDragging) {
      chart.hoverX = null;
      draw(chart);
    }
  });

  listen(window, 'mouseup', function () {
    if (chart.isDragging) {
      handleDragEnd();
    }
  });
}
