import { getPtTs } from '../../../lib/chart/series.js';
import { draw } from './draw.js';
import { bindInteraction } from './interaction.js';

export function renderMultiChart(canvasId, options) {
  function listen(target,type,callback,options={}) {target.addEventListener(type,callback,Object.assign({},options,{signal:listenerController.signal}));}
  const listenerController = new AbortController();
  const canvas = document.getElementById(canvasId);
  if (!canvas) return null;

  // Everything draw() and the interaction handlers read or change for this chart.
  const chart = {
    canvas,
    ctx: canvas.getContext('2d'),
    seriesList: options.seriesList || [],
    formatter: options.formatter || ((v) => v.toFixed(1) + (options.unit || '')),
    hours: options.hours || 6,
    customRange: options.customRange || null,
    yMax: (options.yMax !== undefined && options.yMax !== null) ? options.yMax : null,
    maxAdd: (options.maxAdd !== undefined && options.maxAdd !== null) ? options.maxAdd : null,
    maxCap: (options.maxCap !== undefined && options.maxCap !== null) ? options.maxCap : null,
    minMax: (options.minMax !== undefined && options.minMax !== null) ? options.minMax : 1,
    onZoom: options.onZoom || null,
    queryEndTime: options.queryEndTime || Date.now(),

    // Brush Selection State
    isDragging: false,
    dragStartX: null,
    currentDragX: null,
    hoverX: null,
    leftPad: 56 // grows to fit the widest y-axis label
  };
  const redraw = () => draw(chart);

  bindInteraction(chart, listen);

  redraw();

  const onThemeChange = () => redraw();
  listen(window, 'resize', redraw);
  listen(window, 'certainstats_theme_change', onThemeChange);

  return {
    updateSeries: function (newSeriesList, newHours, newYMax, newQueryEndTime, newCustomRange, newMaxAdd) {
      chart.seriesList = newSeriesList || [];
      if (newHours !== undefined) chart.hours = newHours;
      if (newYMax !== undefined) chart.yMax = (newYMax !== null) ? newYMax : null;
      if (newQueryEndTime !== undefined) chart.queryEndTime = newQueryEndTime;
      if (newCustomRange !== undefined) chart.customRange = newCustomRange;
      if (newMaxAdd !== undefined) chart.maxAdd = newMaxAdd;
      redraw();
    },

    updateLivePoint: function (timestamp, valuesMap) {
      if (!valuesMap) return;
      if (chart.customRange) return; // Freeze live chart mutation while viewing a custom zoomed range
      let needsRedraw = false;
      const ts = (typeof timestamp === 'number' && !isNaN(timestamp)) ? timestamp : (new Date(timestamp).getTime() || Date.now());

      const durationMs = (chart.hours !== null && chart.hours > 0) ? chart.hours * 3600 * 1000 : 6 * 3600 * 1000;
      const stepMs = Math.max(15000, durationMs / 1000);

      chart.seriesList.forEach(s => {
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
        chart.queryEndTime = Math.max(chart.queryEndTime || 0, ts);
        redraw();
      }
    },

    destroy: function () {
      listenerController.abort();
      window.removeEventListener('resize', redraw);
      window.removeEventListener('certainstats_theme_change', onThemeChange);
    }
  };
}
