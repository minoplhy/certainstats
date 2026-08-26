(function () {
  'use strict';

  let config = {};
  let pubCpuChart = null, pubRamChart = null, pubNetChart = null;
  let pubDiskCharts = {}; // { [path]: { usageChart, ioChart } }
  let pubTimePicker = null;
  let currentPubCustomRange = null;

  const PUB_DETAIL_METADATA_SYNC_INTERVAL_MS = 300000;
  let lastPubDetailSyncTime = Date.now();

  function safeId(p) {
    return (!p || p === '/') ? 'root' : p.replace(/[^a-zA-Z0-9_-]/g, '_');
  }

  function handlePubZoom(startMs, endMs) {
    currentPubCustomRange = { start: startMs, end: endMs };
    if (pubTimePicker) {
      pubTimePicker.setCustomRange(startMs, endMs);
    }
  }

  function loadPublicMetrics(hours, customRange) {
    const publicPath = (document.body?.getAttribute('data-public-path') || '').replace(/\/+$/, '');
    const dashId = config.dashId || '';
    const pubId = config.pubId || '';
    const queryStr = customRange
      ? 'start=' + customRange.start + '&end=' + customRange.end
      : 'hours=' + hours;
    const qEnd = customRange ? customRange.end : Date.now();
    const baseQuery = 'dashboard_id=' + encodeURIComponent(dashId) + '&agent_id=' + encodeURIComponent(pubId) + '&' + queryStr;
    const pubAllowedMetrics = config.allowedMetrics || [];

    // 1. Fetch Public CPU
    if (pubAllowedMetrics.includes('agent_cpu_usage') || pubAllowedMetrics.includes('agent_cpu_iowait') || pubAllowedMetrics.includes('agent_cpu_steal')) {
      Promise.all([
        pubAllowedMetrics.includes('agent_cpu_usage') ? fetch(publicPath + '/api/public/metrics?' + baseQuery + '&metric=agent_cpu_usage').then(r => r.json()).catch(() => ({ series: [] })) : Promise.resolve({ series: [] }),
        pubAllowedMetrics.includes('agent_cpu_iowait') ? fetch(publicPath + '/api/public/metrics?' + baseQuery + '&metric=agent_cpu_iowait').then(r => r.json()).catch(() => ({ series: [] })) : Promise.resolve({ series: [] }),
        pubAllowedMetrics.includes('agent_cpu_steal') ? fetch(publicPath + '/api/public/metrics?' + baseQuery + '&metric=agent_cpu_steal').then(r => r.json()).catch(() => ({ series: [] })) : Promise.resolve({ series: [] })
      ]).then(([resUsr, resIO, resStl]) => {
        const ptsUsr = (resUsr && resUsr.series && resUsr.series[0]) ? resUsr.series[0].data : [];
        const ptsIO = (resIO && resIO.series && resIO.series[0]) ? resIO.series[0].data : [];
        const ptsStl = (resStl && resStl.series && resStl.series[0]) ? resStl.series[0].data : [];

        const lastUsr = ptsUsr.length ? ptsUsr[ptsUsr.length - 1][1] : 0;
        const lastIO = ptsIO.length ? ptsIO[ptsIO.length - 1][1] : 0;
        const lastStl = ptsStl.length ? ptsStl[ptsStl.length - 1][1] : 0;
        const elUsr = document.getElementById('pub-live-cpu-usr'); if (elUsr) elUsr.textContent = lastUsr.toFixed(1) + '%';
        const elIO = document.getElementById('pub-live-cpu-io'); if (elIO) elIO.textContent = lastIO.toFixed(1) + '%';
        const elStl = document.getElementById('pub-live-cpu-stl'); if (elStl) elStl.textContent = lastStl.toFixed(1) + '%';

        const seriesList = [];
        if (pubAllowedMetrics.includes('agent_cpu_usage')) {
          seriesList.push({ label: 'Usr', color: '#6366f1', fill: true, data: ptsUsr.map(p => ({ timestamp: p[0], value: p[1] })) });
        }
        if (pubAllowedMetrics.includes('agent_cpu_iowait')) {
          seriesList.push({ label: 'IO', color: '#94a3b8', fill: false, data: ptsIO.map(p => ({ timestamp: p[0], value: p[1] })) });
        }
        if (pubAllowedMetrics.includes('agent_cpu_steal')) {
          seriesList.push({ label: 'Stl', color: '#ef4444', fill: false, data: ptsStl.map(p => ({ timestamp: p[0], value: p[1] })) });
        }

        if (!pubCpuChart) {
          pubCpuChart = window.CertainStatsChart.renderMultiChart('pub-chart-cpu', {
            seriesList: seriesList,
            unit: '%',
            maxAdd: 1,
            maxCap: 100,
            hours: hours,
            customRange: customRange,
            queryEndTime: qEnd,
            onZoom: handlePubZoom
          });
        } else {
          pubCpuChart.updateSeries(seriesList, hours, null, qEnd, customRange, 1);
        }
      });
    }

    // 2. Fetch Public RAM
    if (pubAllowedMetrics.includes('agent_ram_used') || pubAllowedMetrics.includes('agent_swap_used')) {
      Promise.all([
        pubAllowedMetrics.includes('agent_ram_used') ? fetch(publicPath + '/api/public/metrics?' + baseQuery + '&metric=agent_ram_used').then(r => r.json()).catch(() => ({ series: [] })) : Promise.resolve({ series: [] }),
        pubAllowedMetrics.includes('agent_swap_used') ? fetch(publicPath + '/api/public/metrics?' + baseQuery + '&metric=agent_swap_used').then(r => r.json()).catch(() => ({ series: [] })) : Promise.resolve({ series: [] })
      ]).then(([resRam, resSwap]) => {
        const ptsRam = (resRam && resRam.series && resRam.series[0]) ? resRam.series[0].data : [];
        const ptsSwap = (resSwap && resSwap.series && resSwap.series[0]) ? resSwap.series[0].data : [];
        const maxCapacity = Math.max(config.ramSize || 0, config.swapSize || 0);

        const lastRam = ptsRam.length ? ptsRam[ptsRam.length - 1][1] : 0;
        const lastSwap = ptsSwap.length ? ptsSwap[ptsSwap.length - 1][1] : 0;
        const elRam = document.getElementById('pub-live-ram-used'); if (elRam) elRam.textContent = window.CertainStatsChart.formatBytes(lastRam);
        const elSwap = document.getElementById('pub-live-ram-swap'); if (elSwap) elSwap.textContent = window.CertainStatsChart.formatBytes(lastSwap);

        const seriesList = [];
        if (pubAllowedMetrics.includes('agent_ram_used')) {
          seriesList.push({ label: 'RAM', color: '#14b8a6', fill: false, data: ptsRam.map(p => ({ timestamp: p[0], value: p[1] })) });
        }
        if (pubAllowedMetrics.includes('agent_swap_used')) {
          seriesList.push({ label: 'Swap', color: '#94a3b8', fill: false, data: ptsSwap.map(p => ({ timestamp: p[0], value: p[1] })) });
        }

        if (!pubRamChart) {
          pubRamChart = window.CertainStatsChart.renderMultiChart('pub-chart-ram', {
            seriesList: seriesList,
            formatter: window.CertainStatsChart.formatBytes,
            yMax: maxCapacity > 0 ? maxCapacity : null,
            hours: hours,
            customRange: customRange,
            queryEndTime: qEnd,
            onZoom: handlePubZoom
          });
        } else {
          pubRamChart.updateSeries(seriesList, hours, maxCapacity > 0 ? maxCapacity : null, qEnd, customRange);
        }
      });
    }

    // 3. Fetch Public Network RX/TX
    if (pubAllowedMetrics.includes('agent_rx_bytes') || pubAllowedMetrics.includes('agent_tx_bytes')) {
      Promise.all([
        pubAllowedMetrics.includes('agent_rx_bytes') ? fetch(publicPath + '/api/public/metrics?' + baseQuery + '&metric=agent_rx_bytes').then(r => r.json()).catch(() => ({ series: [] })) : Promise.resolve({ series: [] }),
        pubAllowedMetrics.includes('agent_tx_bytes') ? fetch(publicPath + '/api/public/metrics?' + baseQuery + '&metric=agent_tx_bytes').then(r => r.json()).catch(() => ({ series: [] })) : Promise.resolve({ series: [] })
      ]).then(([resRx, resTx]) => {
        const ptsRx = (resRx && resRx.series && resRx.series[0]) ? resRx.series[0].data : [];
        const ptsTx = (resTx && resTx.series && resTx.series[0]) ? resTx.series[0].data : [];

        const rateRx = window.CertainStatsChart.convertDeltaToRate(ptsRx);
        const rateTx = window.CertainStatsChart.convertDeltaToRate(ptsTx);
        const lastRx = rateRx.length ? rateRx[rateRx.length - 1][1] : 0;
        const lastTx = rateTx.length ? rateTx[rateTx.length - 1][1] : 0;
        const elRx = document.getElementById('pub-live-net-rx'); if (elRx) elRx.textContent = '↓ ' + window.CertainStatsChart.formatBps(lastRx);
        const elTx = document.getElementById('pub-live-net-tx'); if (elTx) elTx.textContent = '↑ ' + window.CertainStatsChart.formatBps(lastTx);

        const seriesList = [];
        if (pubAllowedMetrics.includes('agent_rx_bytes')) {
          seriesList.push({ label: 'RX', color: '#1e40af', fill: false, data: rateRx.map(p => ({ timestamp: p[0], value: p[1] })) });
        }
        if (pubAllowedMetrics.includes('agent_tx_bytes')) {
          seriesList.push({ label: 'TX', color: '#7e22ce', fill: false, data: rateTx.map(p => ({ timestamp: p[0], value: p[1] })) });
        }

        if (!pubNetChart) {
          pubNetChart = window.CertainStatsChart.renderMultiChart('pub-chart-net', {
            seriesList: seriesList,
            formatter: window.CertainStatsChart.formatBps,
            hours: hours,
            customRange: customRange,
            queryEndTime: qEnd,
            onZoom: handlePubZoom
          });
        } else {
          pubNetChart.updateSeries(seriesList, hours, undefined, qEnd, customRange);
        }
      });
    }

    // 4. Fetch Disk Usage & Disk I/O Rates (Separated Per Disk Partition)
    if (pubAllowedMetrics.includes('agent_disk_used') || pubAllowedMetrics.includes('agent_disk_read_bytes') || pubAllowedMetrics.includes('agent_disk_write_bytes')) {
      Promise.all([
        pubAllowedMetrics.includes('agent_disk_used') ? fetch(publicPath + '/api/public/metrics?' + baseQuery + '&metric=agent_disk_used').then(r => r.json()).catch(() => ({ series: [] })) : Promise.resolve({ series: [] }),
        pubAllowedMetrics.includes('agent_disk_read_bytes') ? fetch(publicPath + '/api/public/metrics?' + baseQuery + '&metric=agent_disk_read_bytes').then(r => r.json()).catch(() => ({ series: [] })) : Promise.resolve({ series: [] }),
        pubAllowedMetrics.includes('agent_disk_write_bytes') ? fetch(publicPath + '/api/public/metrics?' + baseQuery + '&metric=agent_disk_write_bytes').then(r => r.json()).catch(() => ({ series: [] })) : Promise.resolve({ series: [] })
      ]).then(([resDisk, resRead, resWrite]) => {
        const diskSeries = (resDisk && resDisk.series) ? resDisk.series : [];
        const readSeries = (resRead && resRead.series) ? resRead.series : [];
        const writeSeries = (resWrite && resWrite.series) ? resWrite.series : [];

        const pathSet = new Set();
        diskSeries.forEach(s => pathSet.add(s.labels?.path || '/'));
        readSeries.forEach(s => pathSet.add(s.labels?.path || '/'));
        writeSeries.forEach(s => pathSet.add(s.labels?.path || '/'));
        if (pathSet.size === 0) pathSet.add('/');

        const paths = Array.from(pathSet);
        const container = document.getElementById('pub-disk-charts-grid');
        if (!container) return;

        const expectedIds = paths.map(p => 'pub-disk-card-usage-' + safeId(p)).join(',');
        const currentIds = Array.from(container.children).map(c => c.id).filter(id => id.startsWith('pub-disk-card-usage-')).join(',');

        if (expectedIds !== currentIds) {
          Object.values(pubDiskCharts).forEach(dc => {
            if (dc.usageChart && dc.usageChart.destroy) dc.usageChart.destroy();
            if (dc.ioChart && dc.ioChart.destroy) dc.ioChart.destroy();
          });
          pubDiskCharts = {};

          let html = '';
          paths.forEach(p => {
            const safe = safeId(p);
            html += `
              <div class="card" id="pub-disk-card-usage-${safe}" style="${pubAllowedMetrics.includes('agent_disk_used') ? '' : 'display:none;'}">
                <div class="chart-header-row">
                  <h3 class="chart-header-title">Disk Usage (${p})</h3>
                  <div class="chart-legend-pills">
                    <span class="chart-legend-item"><span class="chart-legend-dot" style="background-color: #8b5cf6;"></span>Used: <span class="chart-legend-val" id="pub-live-disk-used-${safe}">0 B</span></span>
                  </div>
                </div>
                <div class="chart-container" style="height: 220px;">
                  <canvas id="pub-chart-disk-${safe}" class="chart-canvas"></canvas>
                </div>
              </div>
              <div class="card" id="pub-disk-card-io-${safe}" style="${(pubAllowedMetrics.includes('agent_disk_read_bytes') || pubAllowedMetrics.includes('agent_disk_write_bytes')) ? '' : 'display:none;'}">
                <div class="chart-header-row">
                  <h3 class="chart-header-title">Disk I/O Rate (${p})</h3>
                  <div class="chart-legend-pills">
                    ${pubAllowedMetrics.includes('agent_disk_read_bytes') ? `<span class="chart-legend-item"><span class="chart-legend-dot" style="background-color: #fb923c;"></span>Read: <span class="chart-legend-val" id="pub-live-disk-read-${safe}">0 B/s</span></span>` : ''}
                    ${pubAllowedMetrics.includes('agent_disk_write_bytes') ? `<span class="chart-legend-item"><span class="chart-legend-dot" style="background-color: #ef4444;"></span>Write: <span class="chart-legend-val" id="pub-live-disk-write-${safe}">0 B/s</span></span>` : ''}
                  </div>
                </div>
                <div class="chart-container" style="height: 220px;">
                  <canvas id="pub-chart-disk-io-${safe}" class="chart-canvas"></canvas>
                </div>
              </div>`;
          });
          container.innerHTML = html;
        }

        paths.forEach(p => {
          const safe = safeId(p);
          const sDisk = diskSeries.find(s => (s.labels?.path || '/') === p);
          const sRead = readSeries.find(s => (s.labels?.path || '/') === p);
          const sWrite = writeSeries.find(s => (s.labels?.path || '/') === p);

          const ptsUsed = sDisk ? (sDisk.data || []) : [];
          const rateRead = window.CertainStatsChart.convertDeltaToRate(sRead ? (sRead.data || []) : []);
          const rateWrite = window.CertainStatsChart.convertDeltaToRate(sWrite ? (sWrite.data || []) : []);

          const lastUsed = ptsUsed.length ? (ptsUsed[ptsUsed.length - 1][1] || 0) : 0;
          const lastR = rateRead.length ? (rateRead[rateRead.length - 1][1] || 0) : 0;
          const lastW = rateWrite.length ? (rateWrite[rateWrite.length - 1][1] || 0) : 0;

          const elUsed = document.getElementById('pub-live-disk-used-' + safe); if (elUsed) elUsed.textContent = window.CertainStatsChart.formatBytes(lastUsed);
          const elR = document.getElementById('pub-live-disk-read-' + safe); if (elR) elR.textContent = window.CertainStatsChart.formatBps(lastR);
          const elW = document.getElementById('pub-live-disk-write-' + safe); if (elW) elW.textContent = window.CertainStatsChart.formatBps(lastW);

          const usageSeries = [];
          if (pubAllowedMetrics.includes('agent_disk_used')) {
            usageSeries.push({ label: 'Used', color: '#8b5cf6', fill: true, data: ptsUsed.map(pt => ({ timestamp: pt[0], value: pt[1] })) });
          }

          const ioSeries = [];
          if (pubAllowedMetrics.includes('agent_disk_read_bytes')) {
            ioSeries.push({ label: 'Read', color: '#fb923c', fill: false, data: rateRead.map(pt => ({ timestamp: pt[0], value: pt[1] })) });
          }
          if (pubAllowedMetrics.includes('agent_disk_write_bytes')) {
            ioSeries.push({ label: 'Write', color: '#ef4444', fill: false, data: rateWrite.map(pt => ({ timestamp: pt[0], value: pt[1] })) });
          }

          let totalDisk = 0;
          if (config.disks) {
            const d = config.disks.find(x => x.path === p);
            if (d && d.total_bytes > 0) totalDisk = d.total_bytes;
          }
          if (!totalDisk && (!p || p === '/')) {
            totalDisk = config.diskSize || 0;
          }

          if (!pubDiskCharts[p]) {
            let usageChart = null;
            if (pubAllowedMetrics.includes('agent_disk_used')) {
              usageChart = window.CertainStatsChart.renderMultiChart('pub-chart-disk-' + safe, {
                seriesList: usageSeries,
                formatter: window.CertainStatsChart.formatBytes,
                yMax: totalDisk > 0 ? totalDisk : null,
                hours: hours,
                customRange: customRange,
                queryEndTime: qEnd,
                onZoom: handlePubZoom
              });
            }
            let ioChart = null;
            if (pubAllowedMetrics.includes('agent_disk_read_bytes') || pubAllowedMetrics.includes('agent_disk_write_bytes')) {
              ioChart = window.CertainStatsChart.renderMultiChart('pub-chart-disk-io-' + safe, {
                seriesList: ioSeries,
                formatter: window.CertainStatsChart.formatBps,
                hours: hours,
                customRange: customRange,
                queryEndTime: qEnd,
                onZoom: handlePubZoom
              });
            }
            pubDiskCharts[p] = { usageChart, ioChart };
          } else {
            if (pubDiskCharts[p].usageChart) {
              pubDiskCharts[p].usageChart.updateSeries(usageSeries, hours, totalDisk > 0 ? totalDisk : null, qEnd, customRange);
            }
            if (pubDiskCharts[p].ioChart) {
              pubDiskCharts[p].ioChart.updateSeries(ioSeries, hours, undefined, qEnd, customRange);
            }
          }
        });
      });
    }
  }

  function syncPublicAgentDetailMetadata(force) {
    const now = Date.now();
    if (!force && (now - lastPubDetailSyncTime < PUB_DETAIL_METADATA_SYNC_INTERVAL_MS)) return;
    lastPubDetailSyncTime = now;

    const publicPath = (document.body?.getAttribute('data-public-path') || '').replace(/\/+$/, '');
    const dashSlug = config.dashSlug || '';
    const pubId = config.pubId || '';
    if (!dashSlug) return;

    fetch(publicPath + '/api/public/dashboard/' + encodeURIComponent(dashSlug))
      .then(r => r.ok ? r.json() : null)
      .then(data => {
        if (!data || !data.agents || !Array.isArray(data.agents)) return;
        const target = data.agents.find(a => a.public_id === pubId);
        if (!target) return;

        const elCpu = document.getElementById('pub-detail-hw-cpu');
        if (elCpu && target.cpu_cores !== undefined) elCpu.textContent = target.cpu_cores || '-';
        const elRam = document.getElementById('pub-detail-hw-ram');
        if (elRam && target.ram_size !== undefined) elRam.textContent = window.CertainStatsChart.formatBytes(target.ram_size);
        const elDisk = document.getElementById('pub-detail-hw-disk');
        if (elDisk && target.disk_size !== undefined) elDisk.textContent = window.CertainStatsChart.formatBytes(target.disk_size);
        const elSwap = document.getElementById('pub-detail-hw-swap');
        if (elSwap && target.swap_size !== undefined) elSwap.textContent = window.CertainStatsChart.formatBytes(target.swap_size);
        const elKernel = document.getElementById('pub-detail-spec-kernel');
        if (elKernel && target.linux_version !== undefined) elKernel.textContent = target.linux_version || 'Linux';
        const elCpuModel = document.getElementById('pub-detail-spec-cpu');
        if (elCpuModel && target.cpu_model !== undefined) elCpuModel.textContent = target.cpu_model || 'Generic CPU';

        const elNet = document.getElementById('pub-detail-odom-net');
        if (elNet && target.net && (target.net.total_rx_bytes !== undefined || target.net.total_tx_bytes !== undefined)) {
          elNet.innerHTML = '<span>↓ ' + window.CertainStatsChart.formatBytes(target.net.total_rx_bytes || 0) + '</span> / <span>↑ ' + window.CertainStatsChart.formatBytes(target.net.total_tx_bytes || 0) + '</span>';
        }

        const elDiskOdom = document.getElementById('pub-detail-odom-disk');
        if (elDiskOdom && target.disks && Array.isArray(target.disks) && target.disks.length > 0) {
          let dr = 0, dw = 0;
          target.disks.forEach(d => { dr += d.read_bytes ?? d.ReadBytes ?? 0; dw += d.write_bytes ?? d.WriteBytes ?? 0; });
          elDiskOdom.innerHTML = '<span>R: ' + window.CertainStatsChart.formatBytes(dr) + '</span> / <span>W: ' + window.CertainStatsChart.formatBytes(dw) + '</span>';
        }
      })
      .catch(() => {});
  }

  function init(options) {
    config = options || {};
    const pubAllowedMetrics = config.allowedMetrics || [];
    const publicPath = (document.body?.getAttribute('data-public-path') || '').replace(/\/+$/, '');
    const wsUrl = (publicPath ? publicPath : '') + '/api/public/ws/' + encodeURIComponent(config.dashId || '');

    // 1. Immediate WebSocket connection for Live Stats
    window.CertainStatsTelemetry.initWebSocket(wsUrl, function(snaps) {
      const snap = window.CertainStatsTelemetry.normalizeSnapshot(snaps[config.pubId]);
      if (!snap) return;

      const totalRam = config.ramSize || 0;
      const totalDisk = config.diskSize || 0;
      const totalSwap = config.swapSize || 0;

      const hwCpu = document.getElementById('pub-detail-hw-cpu-bar');
      const hwRam = document.getElementById('pub-detail-hw-ram-bar');
      const hwDisk = document.getElementById('pub-detail-hw-disk-bar');
      const hwSwap = document.getElementById('pub-detail-hw-swap-bar');

      if (hwCpu && pubAllowedMetrics.includes('agent_cpu_usage')) hwCpu.style.width = Math.min(snap.cpu_usage_percent, 100) + '%';
      if (hwRam && totalRam > 0 && pubAllowedMetrics.includes('agent_ram_used')) hwRam.style.width = Math.min((snap.ram_used_bytes / totalRam) * 100, 100) + '%';
      if (hwDisk && totalDisk > 0 && snap.disk_used_bytes > 0 && pubAllowedMetrics.includes('agent_disk_used')) hwDisk.style.width = Math.min((snap.disk_used_bytes / totalDisk) * 100, 100) + '%';
      if (hwSwap && totalSwap > 0 && snap.ram_swap_used_bytes > 0 && pubAllowedMetrics.includes('agent_swap_used')) hwSwap.style.width = Math.min((snap.ram_swap_used_bytes / totalSwap) * 100, 100) + '%';

      // Update Disks Section
      const disksGrid = document.getElementById('pub-detail-disks-grid');
      if (disksGrid && pubAllowedMetrics.includes('agent_disk_used')) {
        if (snap.disks && snap.disks.length > 0) {
          snap.disks.forEach(d => {
            const path = d.path || '/';
            const used = d.used_bytes || 0;
            const total = d.total_bytes || 0;
            const pct = total > 0 ? (used / total) * 100 : 0;
            const pctEl = document.getElementById('pub-detail-disk-pct-' + safeId(path)); if (pctEl) pctEl.textContent = pct > 0 ? pct.toFixed(1) + '%' : '-';
            const valEl = document.getElementById('pub-detail-disk-val-' + safeId(path)); if (valEl) valEl.textContent = window.CertainStatsChart.formatBytes(used) + (total ? ' / ' + window.CertainStatsChart.formatBytes(total) : '');
            const barEl = document.getElementById('pub-detail-disk-bar-' + safeId(path)); if (barEl) barEl.style.width = Math.min(pct, 100) + '%';
          });
        } else if (snap.disk_used_bytes > 0) {
          const diskPct = (totalDisk > 0) ? (snap.disk_used_bytes / totalDisk) * 100 : 0;
          const pctEl = document.getElementById('pub-detail-disk-pct-root'); if (pctEl) pctEl.textContent = diskPct.toFixed(1) + '%';
          const valEl = document.getElementById('pub-detail-disk-val-root'); if (valEl) valEl.textContent = window.CertainStatsChart.formatBytes(snap.disk_used_bytes) + (totalDisk ? ' / ' + window.CertainStatsChart.formatBytes(totalDisk) : '');
          const barEl = document.getElementById('pub-detail-disk-bar-root'); if (barEl) barEl.style.width = Math.min(diskPct, 100) + '%';
        }
      }

      // Live Legend Label Updates
      const elCpuUsr = document.getElementById('pub-live-cpu-usr'); if (elCpuUsr && pubAllowedMetrics.includes('agent_cpu_usage')) elCpuUsr.textContent = snap.cpu_usage_percent.toFixed(1) + '%';
      const elCpuIo = document.getElementById('pub-live-cpu-io'); if (elCpuIo && pubAllowedMetrics.includes('agent_cpu_iowait')) elCpuIo.textContent = snap.cpu_iowait_percent.toFixed(1) + '%';
      const elCpuStl = document.getElementById('pub-live-cpu-stl'); if (elCpuStl && pubAllowedMetrics.includes('agent_cpu_steal')) elCpuStl.textContent = snap.cpu_steal_percent.toFixed(1) + '%';
      const elRamUsed = document.getElementById('pub-live-ram-used'); if (elRamUsed && pubAllowedMetrics.includes('agent_ram_used')) elRamUsed.textContent = window.CertainStatsChart.formatBytes(snap.ram_used_bytes);
      const elRamSwap = document.getElementById('pub-live-ram-swap'); if (elRamSwap && pubAllowedMetrics.includes('agent_swap_used')) elRamSwap.textContent = window.CertainStatsChart.formatBytes(snap.ram_swap_used_bytes);
      const elNetRx = document.getElementById('pub-live-net-rx'); if (elNetRx && pubAllowedMetrics.includes('agent_rx_bytes')) elNetRx.textContent = '↓ ' + window.CertainStatsChart.formatBps(snap.rx_bps);
      const elNetTx = document.getElementById('pub-live-net-tx'); if (elNetTx && pubAllowedMetrics.includes('agent_tx_bytes')) elNetTx.textContent = '↑ ' + window.CertainStatsChart.formatBps(snap.tx_bps);

      if (snap.uptime != null) {
        const uptimeEl = document.getElementById('pub-detail-uptime');
        if (uptimeEl) uptimeEl.textContent = window.CertainStatsTelemetry.formatUptime(snap.uptime);
      }

      if (!currentPubCustomRange) {
        const ts = Date.now();
        if (pubCpuChart) {
          const map = {};
          if (pubAllowedMetrics.includes('agent_cpu_usage')) map['Usr'] = snap.cpu_usage_percent;
          if (pubAllowedMetrics.includes('agent_cpu_iowait')) map['IO'] = snap.cpu_iowait_percent;
          if (pubAllowedMetrics.includes('agent_cpu_steal')) map['Stl'] = snap.cpu_steal_percent;
          pubCpuChart.updateLivePoint(ts, map);
        }
        if (pubRamChart) {
          const map = {};
          if (pubAllowedMetrics.includes('agent_ram_used')) map['RAM'] = snap.ram_used_bytes;
          if (pubAllowedMetrics.includes('agent_swap_used')) map['Swap'] = snap.ram_swap_used_bytes;
          pubRamChart.updateLivePoint(ts, map);
        }
        if (pubNetChart) {
          const map = {};
          if (pubAllowedMetrics.includes('agent_rx_bytes')) map['RX'] = snap.rx_bps;
          if (pubAllowedMetrics.includes('agent_tx_bytes')) map['TX'] = snap.tx_bps;
          pubNetChart.updateLivePoint(ts, map);
        }

        if (snap.disks && snap.disks.length > 0) {
          snap.disks.forEach(d => {
            const path = d.path || '/';
            const safe = safeId(path);
            const elUsed = document.getElementById('pub-live-disk-used-' + safe); if (elUsed && pubAllowedMetrics.includes('agent_disk_used')) elUsed.textContent = window.CertainStatsChart.formatBytes(d.used_bytes);
            const elR = document.getElementById('pub-live-disk-read-' + safe); if (elR && pubAllowedMetrics.includes('agent_disk_read_bytes')) elR.textContent = window.CertainStatsChart.formatBps(d.read_bytes || 0);
            const elW = document.getElementById('pub-live-disk-write-' + safe); if (elW && pubAllowedMetrics.includes('agent_disk_write_bytes')) elW.textContent = window.CertainStatsChart.formatBps(d.write_bytes || 0);

            if (pubDiskCharts[path]) {
              if (pubDiskCharts[path].usageChart && pubAllowedMetrics.includes('agent_disk_used')) {
                pubDiskCharts[path].usageChart.updateLivePoint(ts, { Used: d.used_bytes });
              }
              if (pubDiskCharts[path].ioChart) {
                const map = {};
                if (pubAllowedMetrics.includes('agent_disk_read_bytes')) map['Read'] = d.read_bytes || 0;
                if (pubAllowedMetrics.includes('agent_disk_write_bytes')) map['Write'] = d.write_bytes || 0;
                pubDiskCharts[path].ioChart.updateLivePoint(ts, map);
              }
            }
          });
        } else {
          const elUsed = document.getElementById('pub-live-disk-used-root'); if (elUsed && pubAllowedMetrics.includes('agent_disk_used')) elUsed.textContent = window.CertainStatsChart.formatBytes(snap.disk_used_bytes);
          const elR = document.getElementById('pub-live-disk-read-root'); if (elR && pubAllowedMetrics.includes('agent_disk_read_bytes')) elR.textContent = window.CertainStatsChart.formatBps(snap.disk_read_bps);
          const elW = document.getElementById('pub-live-disk-write-root'); if (elW && pubAllowedMetrics.includes('agent_disk_write_bytes')) elW.textContent = window.CertainStatsChart.formatBps(snap.disk_write_bps);

          if (pubDiskCharts['/']) {
            if (pubDiskCharts['/'].usageChart && pubAllowedMetrics.includes('agent_disk_used')) {
              pubDiskCharts['/'].usageChart.updateLivePoint(ts, { Used: snap.disk_used_bytes });
            }
            if (pubDiskCharts['/'].ioChart) {
              const map = {};
              if (pubAllowedMetrics.includes('agent_disk_read_bytes')) map['Read'] = snap.disk_read_bps;
              if (pubAllowedMetrics.includes('agent_disk_write_bytes')) map['Write'] = snap.disk_write_bps;
              pubDiskCharts['/'].ioChart.updateLivePoint(ts, map);
            }
          }
        }
      }
    });

    // 2. Setup on DOM Ready
    window.CertainStatsTelemetry.onReady(function() {
      const maxDays = config.maxDays || 7;
      pubTimePicker = window.CertainStatsTelemetry.initTimeRangeBar('pub-detail-time-range-bar', {
        maxDays: maxDays,
        onSelect: function(h) {
          loadPublicMetrics(h);
        }
      });

      let activeHours = 6;
      try { activeHours = parseInt(localStorage.getItem('certainstats_public_active_hours') || '6', 10); } catch (e) {}
      loadPublicMetrics(isNaN(activeHours) || activeHours <= 0 ? 6 : activeHours);
    });

    setInterval(syncPublicAgentDetailMetadata, PUB_DETAIL_METADATA_SYNC_INTERVAL_MS);
  }

  // Export module namespace
  window.CertainStatsPublicAgentDetail = {
    init: init,
    loadPublicMetrics: loadPublicMetrics,
    syncPublicAgentDetailMetadata: syncPublicAgentDetailMetadata
  };
})();
