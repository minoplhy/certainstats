import { state } from '../state.js';
import { safeId } from '../nav/dom.js';
import { renderPubInpageLiveState } from './live.js';

// Agent detail: header facts, history charts and storage.

export function loadPublicDetailMetrics(pubId, hours, customRange) {
  if (state.maxDays <= 0 || !state.pubAllowedMetrics.length) return;
  const supported=[1,6,12,24,48,168,720,2160,4320,8760,17520].filter(value=>value<=state.maxDays*24);
  if (!supported.includes(hours)) hours=supported.includes(6) ? 6 : supported[0];
  customRange=null;
  const requestGroup = window.CertainStatsRequests.begin('public-detail-view', () => loadPublicDetailMetrics(pubId, hours, customRange));
  const publicPath = (document.body?.getAttribute('data-public-path') || '').replace(/\/+$/, '');
  const agent = state.pubAgentsData.find(a => a.public_id === pubId);
  let baseQuery = 'dashboard_id=' + encodeURIComponent(state.dashId) + '&agent_id=' + encodeURIComponent(pubId);
  if (customRange && customRange.start && customRange.end) {
    baseQuery += '&start=' + customRange.start + '&end=' + customRange.end;
  } else {
    baseQuery += '&hours=' + hours;
  }
  const qEnd = customRange ? customRange.end : Date.now();

  if (agent) {
    const isOnline = agent.is_online;
    const dotEl = document.getElementById('pub-inpage-dot');
    const badgeEl = document.getElementById('pub-inpage-badge');
    if (dotEl) dotEl.className = 'status-dot ' + (isOnline === undefined || isOnline === null ? '' : isOnline ? 'online' : 'offline');
    if (badgeEl) {
      badgeEl.className = 'badge ' + (isOnline === undefined || isOnline === null ? '' : isOnline ? 'badge-online' : 'badge-offline');
      badgeEl.textContent = isOnline === undefined || isOnline === null ? 'Status unavailable' : isOnline ? 'Online' : 'Offline';
    }

    const elName = document.getElementById('pub-inpage-name');
    if (elName) elName.textContent = agent.display_name;
    const elCpu = document.getElementById('pub-inpage-hw-cpu');
    if (elCpu) elCpu.textContent = agent.cpu_cores || '-';
    const elRam = document.getElementById('pub-inpage-hw-ram');
    if (elRam) elRam.textContent = window.CertainStatsChart.formatBytes(agent.ram_size);
    const elDisk = document.getElementById('pub-inpage-hw-disk');
    if (elDisk) elDisk.textContent = window.CertainStatsChart.formatBytes(agent.disk_size);
    const elSwap = document.getElementById('pub-inpage-hw-swap');
    if (elSwap) elSwap.textContent = window.CertainStatsChart.formatBytes(agent.swap_size);

    const pubIdEl = document.getElementById('pub-inpage-spec-id');
    if (pubIdEl) pubIdEl.textContent = agent.public_id;
    const elUptime = document.getElementById('pub-inpage-spec-uptime');
    if (elUptime) elUptime.textContent = window.CertainStatsTelemetry.formatUptime(agent.uptime);
    const elKernel = document.getElementById('pub-inpage-spec-kernel');
    if (elKernel) elKernel.textContent = agent.linux_version || 'Linux';
    const elCpuModel = document.getElementById('pub-inpage-spec-cpu');
    if (elCpuModel) elCpuModel.textContent = agent.cpu_model || 'Generic CPU';

    // Render dynamic multi-disk Storage Partitions and live specs immediately from remembered state
    renderPubInpageLiveState(pubId);

    const odoNet = document.getElementById('pub-inpage-odo-net');
    if (odoNet) odoNet.innerHTML = '<span>↓ ' + window.CertainStatsChart.formatBytes(agent.total_rx_bytes || 0) + '</span><span>↑ ' + window.CertainStatsChart.formatBytes(agent.total_tx_bytes || 0) + '</span>';

    let totalDiskRead = agent.total_disk_read_bytes || 0;
    let totalDiskWrite = agent.total_disk_write_bytes || 0;
    if (!totalDiskRead && !totalDiskWrite && agent.disks) {
      agent.disks.forEach(d => {
        totalDiskRead += (d.read_bytes || 0);
        totalDiskWrite += (d.write_bytes || 0);
      });
    }
    const odoDisk = document.getElementById('pub-inpage-odo-disk');
    if (odoDisk) odoDisk.innerHTML = '<span>R ' + window.CertainStatsChart.formatBytes(totalDiskRead) + '</span><span>W ' + window.CertainStatsChart.formatBytes(totalDiskWrite) + '</span>';
  }

  // 1. Fetch CPU (Usr, IO, Steal)
  if (state.pubAllowedMetrics.includes('agent_cpu_usage') || state.pubAllowedMetrics.includes('agent_cpu_iowait') || state.pubAllowedMetrics.includes('agent_cpu_steal')) {
    Promise.all([
      state.pubAllowedMetrics.includes('agent_cpu_usage') ? requestGroup.json(publicPath + '/api/public/metrics?' + baseQuery + '&metric=agent_cpu_usage').catch(() => ({ series: [] })) : Promise.resolve({ series: [] }),
      state.pubAllowedMetrics.includes('agent_cpu_iowait') ? requestGroup.json(publicPath + '/api/public/metrics?' + baseQuery + '&metric=agent_cpu_iowait').catch(() => ({ series: [] })) : Promise.resolve({ series: [] }),
      state.pubAllowedMetrics.includes('agent_cpu_steal') ? requestGroup.json(publicPath + '/api/public/metrics?' + baseQuery + '&metric=agent_cpu_steal').catch(() => ({ series: [] })) : Promise.resolve({ series: [] })
    ]).then(([resUsr, resIO, resStl]) => {
    if (requestGroup.signal.aborted) return;
      const ptsUsr = (resUsr && resUsr.series && resUsr.series[0]) ? resUsr.series[0].data : [];
      const ptsIO = (resIO && resIO.series && resIO.series[0]) ? resIO.series[0].data : [];
      const ptsStl = (resStl && resStl.series && resStl.series[0]) ? resStl.series[0].data : [];

      const lastUsr = ptsUsr.length ? ptsUsr[ptsUsr.length - 1][1] : null;
      const lastIO = ptsIO.length ? ptsIO[ptsIO.length - 1][1] : null;
      const lastStl = ptsStl.length ? ptsStl[ptsStl.length - 1][1] : null;
      const elUsr = document.getElementById('pub-inpage-live-cpu-usr'); if (elUsr) elUsr.textContent = (lastUsr == null ? "Unavailable" : lastUsr.toFixed(1) + "%");
      const elIO = document.getElementById('pub-inpage-live-cpu-io'); if (elIO) elIO.textContent = (lastIO == null ? "Unavailable" : lastIO.toFixed(1) + "%");
      const elStl = document.getElementById('pub-inpage-live-cpu-stl'); if (elStl) elStl.textContent = (lastStl == null ? "Unavailable" : lastStl.toFixed(1) + "%");

      const seriesList = [];
      if (state.pubAllowedMetrics.includes('agent_cpu_usage')) {
        seriesList.push({ label: 'Usr', color: 'var(--s1)', fill: true, data: ptsUsr.map(p => ({ timestamp: p[0], value: p[1] })) });
      }
      if (state.pubAllowedMetrics.includes('agent_cpu_iowait')) {
        seriesList.push({ label: 'IO', color: 'var(--s2)', fill: false, data: ptsIO.map(p => ({ timestamp: p[0], value: p[1] })) });
      }
      if (state.pubAllowedMetrics.includes('agent_cpu_steal')) {
        seriesList.push({ label: 'Stl', color: 'var(--s3)', fill: false, data: ptsStl.map(p => ({ timestamp: p[0], value: p[1] })) });
      }

      if (!state.pubInpageCpuChart) {
        state.pubInpageCpuChart = window.CertainStatsChart.renderMultiChart('pub-inpage-chart-cpu', {
          seriesList: seriesList,
          unit: '%',
          maxAdd: 1,
          maxCap: 100,
          hours: hours,
          customRange: customRange,
          queryEndTime: qEnd
        });
      } else {
        state.pubInpageCpuChart.updateSeries(seriesList, hours, null, qEnd, customRange, 1);
      }
    });
  }

  // 2. Fetch RAM (RAM, Swap)
  if (state.pubAllowedMetrics.includes('agent_ram_used') || state.pubAllowedMetrics.includes('agent_swap_used')) {
    Promise.all([
      state.pubAllowedMetrics.includes('agent_ram_used') ? requestGroup.json(publicPath + '/api/public/metrics?' + baseQuery + '&metric=agent_ram_used').catch(() => ({ series: [] })) : Promise.resolve({ series: [] }),
      state.pubAllowedMetrics.includes('agent_swap_used') ? requestGroup.json(publicPath + '/api/public/metrics?' + baseQuery + '&metric=agent_swap_used').catch(() => ({ series: [] })) : Promise.resolve({ series: [] })
    ]).then(([resRam, resSwap]) => {
    if (requestGroup.signal.aborted) return;
      const ptsRam = (resRam && resRam.series && resRam.series[0]) ? resRam.series[0].data : [];
      const ptsSwap = (resSwap && resSwap.series && resSwap.series[0]) ? resSwap.series[0].data : [];
      const maxCapacity = Math.max(agent ? agent.ram_size : 0, agent ? agent.swap_size : 0);

      const lastRam = ptsRam.length ? ptsRam[ptsRam.length - 1][1] : null;
      const lastSwap = ptsSwap.length ? ptsSwap[ptsSwap.length - 1][1] : null;
      const elRam = document.getElementById('pub-inpage-live-ram-used'); if (elRam) elRam.textContent = window.CertainStatsChart.formatBytes(lastRam);
      const elSwap = document.getElementById('pub-inpage-live-ram-swap'); if (elSwap) elSwap.textContent = window.CertainStatsChart.formatBytes(lastSwap);

      const seriesList = [];
      if (state.pubAllowedMetrics.includes('agent_ram_used')) {
        seriesList.push({ label: 'RAM', color: 'var(--s4)', fill: false, data: ptsRam.map(p => ({ timestamp: p[0], value: p[1] })) });
      }
      if (state.pubAllowedMetrics.includes('agent_swap_used')) {
        seriesList.push({ label: 'Swap', color: 'var(--s5)', fill: false, data: ptsSwap.map(p => ({ timestamp: p[0], value: p[1] })) });
      }

      if (!state.pubInpageRamChart) {
        state.pubInpageRamChart = window.CertainStatsChart.renderMultiChart('pub-inpage-chart-ram', {
          seriesList: seriesList,
          formatter: window.CertainStatsChart.formatBytes,
          yMax: maxCapacity > 0 ? maxCapacity : null,
          hours: hours,
          customRange: customRange,
          queryEndTime: qEnd
        });
      } else {
        state.pubInpageRamChart.updateSeries(seriesList, hours, maxCapacity > 0 ? maxCapacity : null, qEnd, customRange);
      }
    });
  }

  // 3. Fetch Network RX/TX
  if (state.pubAllowedMetrics.includes('agent_rx_bytes') || state.pubAllowedMetrics.includes('agent_tx_bytes')) {
    Promise.all([
      state.pubAllowedMetrics.includes('agent_rx_bytes') ? requestGroup.json(publicPath + '/api/public/metrics?' + baseQuery + '&metric=agent_rx_bytes').catch(() => ({ series: [] })) : Promise.resolve({ series: [] }),
      state.pubAllowedMetrics.includes('agent_tx_bytes') ? requestGroup.json(publicPath + '/api/public/metrics?' + baseQuery + '&metric=agent_tx_bytes').catch(() => ({ series: [] })) : Promise.resolve({ series: [] })
    ]).then(([resRx, resTx]) => {
    if (requestGroup.signal.aborted) return;
      const ptsRx = (resRx && resRx.series && resRx.series[0]) ? resRx.series[0].data : [];
      const ptsTx = (resTx && resTx.series && resTx.series[0]) ? resTx.series[0].data : [];

      const rateRx = (resRx?.series?.[0]?.rate_data || window.CertainStatsChart.convertDeltaToRate(ptsRx));
      const rateTx = (resTx?.series?.[0]?.rate_data || window.CertainStatsChart.convertDeltaToRate(ptsTx));
      const lastRx = rateRx.length ? rateRx[rateRx.length - 1][1] : null;
      const lastTx = rateTx.length ? rateTx[rateTx.length - 1][1] : null;
      const elRx = document.getElementById('pub-inpage-live-net-rx'); if (elRx) elRx.textContent = '↓ ' + window.CertainStatsChart.formatBps(lastRx);
      const elTx = document.getElementById('pub-inpage-live-net-tx'); if (elTx) elTx.textContent = '↑ ' + window.CertainStatsChart.formatBps(lastTx);

      const seriesList = [];
      if (state.pubAllowedMetrics.includes('agent_rx_bytes')) {
        seriesList.push({ label: 'RX', color: 'var(--rx)', fill: false, data: rateRx.map(p => ({ timestamp: p[0], value: p[1] })) });
      }
      if (state.pubAllowedMetrics.includes('agent_tx_bytes')) {
        seriesList.push({ label: 'TX', color: 'var(--tx)', fill: false, data: rateTx.map(p => ({ timestamp: p[0], value: p[1] })) });
      }

      if (!state.pubInpageNetChart) {
        state.pubInpageNetChart = window.CertainStatsChart.renderMultiChart('pub-inpage-chart-net', {
          seriesList: seriesList,
          formatter: window.CertainStatsChart.formatBps,
          hours: hours,
          customRange: customRange,
          queryEndTime: qEnd
        });
      } else {
        state.pubInpageNetChart.updateSeries(seriesList, hours, undefined, qEnd, customRange);
      }
    });
  }

  // 4. Fetch Disk Usage & Disk I/O Rates (Separated Per Disk Partition)
  if (state.pubAllowedMetrics.includes('agent_disk_used') || state.pubAllowedMetrics.includes('agent_disk_read_bytes') || state.pubAllowedMetrics.includes('agent_disk_write_bytes')) {
    Promise.all([
      state.pubAllowedMetrics.includes('agent_disk_used') ? requestGroup.json(publicPath + '/api/public/metrics?' + baseQuery + '&metric=agent_disk_used').catch(() => ({ series: [] })) : Promise.resolve({ series: [] }),
      state.pubAllowedMetrics.includes('agent_disk_read_bytes') ? requestGroup.json(publicPath + '/api/public/metrics?' + baseQuery + '&metric=agent_disk_read_bytes').catch(() => ({ series: [] })) : Promise.resolve({ series: [] }),
      state.pubAllowedMetrics.includes('agent_disk_write_bytes') ? requestGroup.json(publicPath + '/api/public/metrics?' + baseQuery + '&metric=agent_disk_write_bytes').catch(() => ({ series: [] })) : Promise.resolve({ series: [] })
    ]).then(([resDisk, resRead, resWrite]) => {
    if (requestGroup.signal.aborted) return;
      const diskSeries = (resDisk && resDisk.series) ? resDisk.series : [];
      const readSeries = (resRead && resRead.series) ? resRead.series : [];
      const writeSeries = (resWrite && resWrite.series) ? resWrite.series : [];

      const pathSet = new Set();
      diskSeries.forEach(s => pathSet.add(s.labels?.path || '/'));
      readSeries.forEach(s => pathSet.add(s.labels?.path || '/'));
      writeSeries.forEach(s => pathSet.add(s.labels?.path || '/'));
      if (pathSet.size === 0) pathSet.add('/');

      const paths = Array.from(pathSet);
      const container = document.getElementById('pub-inpage-disk-charts-grid');
      if (!container) return;

      const expectedIds = paths.map(p => 'pub-inpage-disk-card-usage-' + safeId(p)).join(',');
      const currentIds = Array.from(container.children).map(c => c.id).filter(id => id.startsWith('pub-inpage-disk-card-usage-')).join(',');

      if (expectedIds !== currentIds) {
        Object.values(state.pubInpageDiskCharts).forEach(dc => {
          if (dc.usageChart && dc.usageChart.destroy) dc.usageChart.destroy();
          if (dc.ioChart && dc.ioChart.destroy) dc.ioChart.destroy();
        });
        state.pubInpageDiskCharts = {};

        window.CertainStatsDiskDOM.render(container, paths, 'pub-inpage-', state.pubAllowedMetrics);
      }

      paths.forEach(p => {
        const safe = safeId(p);
        const sDisk = diskSeries.find(s => (s.labels?.path || '/') === p);
        const sRead = readSeries.find(s => (s.labels?.path || '/') === p);
        const sWrite = writeSeries.find(s => (s.labels?.path || '/') === p);

        const ptsUsed = sDisk ? (sDisk.data || []) : [];
        const rateRead = (sRead?.rate_data || window.CertainStatsChart.convertDeltaToRate(sRead?.data || []));
        const rateWrite = (sWrite?.rate_data || window.CertainStatsChart.convertDeltaToRate(sWrite?.data || []));

        const lastUsed = ptsUsed.length ? ptsUsed[ptsUsed.length - 1][1] : null;
        const lastR = rateRead.length ? rateRead[rateRead.length - 1][1] : null;
        const lastW = rateWrite.length ? rateWrite[rateWrite.length - 1][1] : null;

        const elUsed = document.getElementById('pub-inpage-live-disk-used-' + safe); if (elUsed) elUsed.textContent = window.CertainStatsChart.formatBytes(lastUsed);
        const elR = document.getElementById('pub-inpage-live-disk-read-' + safe); if (elR) elR.textContent = window.CertainStatsChart.formatBps(lastR);
        const elW = document.getElementById('pub-inpage-live-disk-write-' + safe); if (elW) elW.textContent = window.CertainStatsChart.formatBps(lastW);

        const usageSeries = [];
        if (state.pubAllowedMetrics.includes('agent_disk_used')) {
          usageSeries.push({ label: 'Used', color: 'var(--s1)', fill: true, data: ptsUsed.map(pt => ({ timestamp: pt[0], value: pt[1] })) });
        }

        const ioSeries = [];
        if (state.pubAllowedMetrics.includes('agent_disk_read_bytes')) {
          ioSeries.push({ label: 'Read', color: 'var(--s2)', fill: false, data: rateRead.map(pt => ({ timestamp: pt[0], value: pt[1] })) });
        }
        if (state.pubAllowedMetrics.includes('agent_disk_write_bytes')) {
          ioSeries.push({ label: 'Write', color: 'var(--s3)', fill: false, data: rateWrite.map(pt => ({ timestamp: pt[0], value: pt[1] })) });
        }

        let totalDisk = 0;
        if (agent && agent.disks) {
          const d = agent.disks.find(x => x.path === p);
          if (d && d.total_bytes > 0) totalDisk = d.total_bytes;
        }
        if (!totalDisk && agent && agent.latest_snap && agent.latest_snap.disks) {
          const sd = agent.latest_snap.disks.find(x => x.path === p);
          if (sd && sd.total_bytes > 0) totalDisk = sd.total_bytes;
        }
        if (!totalDisk && (!p || p === '/')) {
          totalDisk = agent ? (agent.disk_size || 0) : 0;
        }

        if (!state.pubInpageDiskCharts[p]) {
          let usageChart = null;
          if (state.pubAllowedMetrics.includes('agent_disk_used')) {
            usageChart = window.CertainStatsChart.renderMultiChart('pub-inpage-chart-disk-' + safe, {
              seriesList: usageSeries,
              formatter: window.CertainStatsChart.formatBytes,
              yMax: totalDisk > 0 ? totalDisk : null,
              hours: hours,
              customRange: customRange,
              queryEndTime: qEnd
            });
          }
          let ioChart = null;
          if (state.pubAllowedMetrics.includes('agent_disk_read_bytes') || state.pubAllowedMetrics.includes('agent_disk_write_bytes')) {
            ioChart = window.CertainStatsChart.renderMultiChart('pub-inpage-chart-disk-io-' + safe, {
              seriesList: ioSeries,
              formatter: window.CertainStatsChart.formatBps,
              hours: hours,
              customRange: customRange,
              queryEndTime: qEnd
            });
          }
          state.pubInpageDiskCharts[p] = { usageChart, ioChart };
        } else {
          if (state.pubInpageDiskCharts[p].usageChart) {
            state.pubInpageDiskCharts[p].usageChart.updateSeries(usageSeries, hours, totalDisk > 0 ? totalDisk : null, qEnd, customRange);
          }
          if (state.pubInpageDiskCharts[p].ioChart) {
            state.pubInpageDiskCharts[p].ioChart.updateSeries(ioSeries, hours, undefined, qEnd, customRange);
          }
        }
      });
    });
  }
}
