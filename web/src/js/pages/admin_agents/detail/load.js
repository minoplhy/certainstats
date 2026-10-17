import { state } from '../state.js';
import { safeId } from './dom.js';
import { syncInpageNotesUI } from '../editing/notes.js';
import { resetInpageEditStates } from '../editing/rename.js';
import { renderAgentVersion } from './version.js';

// Agent detail: header facts, live tiles and history charts.

export function handleInpageZoom(startMs, endMs) {
  state.inpageCustomRange = { start: startMs, end: endMs };
  if (state.inpageTimePicker) {
    state.inpageTimePicker.setCustomRange(startMs, endMs);
  }
}

export function renderInpageLiveState(agentId) {
  if (!agentId) return;
  const agent = state.agentsData.find(a => a.agent_id === agentId) || {};
  const snap = window.CertainStatsTelemetry.normalizeSnapshot(state.liveMetricsStore[agentId]) || agent.latest_snap;

  // Render disks immediately from remembered state
  const inpageDisksGrid = document.getElementById('inpage-disks-grid');
  if (inpageDisksGrid) {
    const disks = (agent.disks && agent.disks.length > 0) ? agent.disks : [];
    const partitionCards = [];
    if (disks && disks.length > 0) {
      disks.forEach(d => {
        const path = d.path || '/';
        const snapDisk = (snap && snap.disks) ? snap.disks.find(x => x.path === path) : null;
        const used = snapDisk ? snapDisk.used_bytes : (d.used_bytes ?? null);
        const total = d.total_bytes || (snapDisk ? snapDisk.total_bytes : 0) || 0;
        partitionCards.push({path, used, total, read:d.read_bytes ?? null, write:d.write_bytes ?? null});
      });
    } else {
      const diskUsed = snap ? (snap.disk_used_bytes ?? null) : null;
      const diskTotal = agent.disk_size || (snap ? snap.disk_total_bytes : 0) || 0;
      partitionCards.push({path:'/', used:diskUsed, total:diskTotal, read:agent.total_disk_read_bytes ?? null, write:agent.total_disk_write_bytes ?? null});
    }
    window.CertainStatsTelemetry.renderPartitions(inpageDisksGrid, partitionCards, {});
  }

  if (snap) {
    const inpageCpu = document.getElementById('inpage-hw-cpu-bar');
    const inpageRam = document.getElementById('inpage-hw-ram-bar');
    const inpageDisk = document.getElementById('inpage-hw-disk-bar');
    const inpageSwap = document.getElementById('inpage-hw-swap-bar');
    const ramPct = (agent.ram_size && agent.ram_size > 0) ? (snap.ram_used_bytes / agent.ram_size) * 100 : 0;
    const diskPct = (agent.disk_size && agent.disk_size > 0 && snap.disk_used_bytes > 0) ? (snap.disk_used_bytes / agent.disk_size) * 100 : 0;
    const swapPct = (agent.swap_size && agent.swap_size > 0 && snap.ram_swap_used_bytes > 0) ? (snap.ram_swap_used_bytes / agent.swap_size) * 100 : 0;

    if (inpageCpu) inpageCpu.style.width = Math.min(snap.cpu_usage_percent, 100) + '%';
    if (inpageRam) inpageRam.style.width = Math.min(ramPct, 100) + '%';
    if (inpageDisk) inpageDisk.style.width = Math.min(diskPct, 100) + '%';
    if (inpageSwap) inpageSwap.style.width = Math.min(swapPct, 100) + '%';

    const fmt = window.CertainStatsChart.formatBytes;
    const tileCpu = document.getElementById('inpage-tile-cpu'); if (tileCpu) tileCpu.textContent = (snap.cpu_usage_percent == null ? "Unavailable" : snap.cpu_usage_percent.toFixed(1) + "%");
    const tileRam = document.getElementById('inpage-tile-ram'); if (tileRam) tileRam.textContent = fmt(snap.ram_used_bytes);
    const tileDisk = document.getElementById('inpage-tile-disk'); if (tileDisk) tileDisk.textContent = fmt(snap.disk_used_bytes);
    const tileSwap = document.getElementById('inpage-tile-swap'); if (tileSwap) tileSwap.textContent = fmt(snap.ram_swap_used_bytes);

    const inpageCpuUsr = document.getElementById('inpage-live-cpu-usr'); if (inpageCpuUsr) inpageCpuUsr.textContent = (snap.cpu_usage_percent == null ? "Unavailable" : snap.cpu_usage_percent.toFixed(1) + "%");
    const inpageCpuIo = document.getElementById('inpage-live-cpu-io'); if (inpageCpuIo) inpageCpuIo.textContent = (snap.cpu_iowait_percent == null ? "Unavailable" : snap.cpu_iowait_percent.toFixed(1) + "%");
    const inpageCpuStl = document.getElementById('inpage-live-cpu-stl'); if (inpageCpuStl) inpageCpuStl.textContent = (snap.cpu_steal_percent == null ? "Unavailable" : snap.cpu_steal_percent.toFixed(1) + "%");
    const inpageRamUsed = document.getElementById('inpage-live-ram-used'); if (inpageRamUsed) inpageRamUsed.textContent = window.CertainStatsChart.formatBytes(snap.ram_used_bytes);
    const inpageRamSwap = document.getElementById('inpage-live-ram-swap'); if (inpageRamSwap) inpageRamSwap.textContent = window.CertainStatsChart.formatBytes(snap.ram_swap_used_bytes);
    const inpageNetRx = document.getElementById('inpage-live-net-rx'); if (inpageNetRx) inpageNetRx.textContent = '↓ ' + window.CertainStatsChart.formatBps(snap.rx_bps);
    const inpageNetTx = document.getElementById('inpage-live-net-tx'); if (inpageNetTx) inpageNetTx.textContent = '↑ ' + window.CertainStatsChart.formatBps(snap.tx_bps);

    if (snap.uptime != null) {
      const inpageUptimeEl = document.getElementById('inpage-spec-uptime');
      if (inpageUptimeEl) inpageUptimeEl.textContent = window.CertainStatsTelemetry.formatUptime(snap.uptime);
    }
  }
}

export function loadDetailMetrics(agentId, hours, customRange) {
  if (state.activeRoute === 'network' || document.hidden) return;
  const requestGroup = window.CertainStatsRequests.begin('agents-detail-view', () => loadDetailMetrics(agentId, hours, customRange));
  const agent = state.agentsData.find(a => a.agent_id === agentId);
  const queryStr = customRange 
    ? 'start=' + customRange.start + '&end=' + customRange.end 
    : 'hours=' + hours;
  const qEnd = customRange ? customRange.end : Date.now();

  if (agent) {
    resetInpageEditStates();
    const isOnline = agent.is_online;
    const dotEl = document.getElementById('detail-active-dot');
    const badgeEl = document.getElementById('detail-active-badge');
    if (dotEl) dotEl.className = 'status-dot ' + (isOnline === undefined || isOnline === null ? '' : isOnline ? 'online' : 'offline');
    if (badgeEl) {
      badgeEl.className = 'badge ' + (isOnline === undefined || isOnline === null ? '' : isOnline ? 'badge-online' : 'badge-offline');
      badgeEl.textContent = isOnline === undefined || isOnline === null ? 'Status unavailable' : isOnline ? 'Online' : 'Offline';
    }

    const elName = document.getElementById('detail-active-name'); if (elName) elName.textContent = agent.nickname || agent.agent_id;
    renderAgentVersion(agent);
    const elType = document.getElementById('detail-active-type'); if (elType) elType.textContent = agent.agent_type || 'beszel';
    const elCpu = document.getElementById('inpage-hw-cpu'); if (elCpu) elCpu.textContent = agent.cpu_cores || '-';
    const elRam = document.getElementById('inpage-hw-ram'); if (elRam) elRam.textContent = window.CertainStatsChart.formatBytes(agent.ram_size);
    const elDisk = document.getElementById('inpage-hw-disk'); if (elDisk) elDisk.textContent = window.CertainStatsChart.formatBytes(agent.disk_size);
    const elSwap = document.getElementById('inpage-hw-swap'); if (elSwap) elSwap.textContent = window.CertainStatsChart.formatBytes(agent.swap_size);

    const idEl = document.getElementById('inpage-spec-id');
    if (idEl) idEl.textContent = agent.agent_id;
    const elUptime = document.getElementById('inpage-spec-uptime'); if (elUptime) elUptime.textContent = window.CertainStatsTelemetry.formatUptime(agent.uptime);
    const elKernel = document.getElementById('inpage-spec-kernel'); if (elKernel) elKernel.textContent = agent.linux_version || 'Linux';
    const elCpuModel = document.getElementById('inpage-spec-cpu'); if (elCpuModel) elCpuModel.textContent = agent.cpu_model || 'Generic CPU';

    // Render dynamic multi-disk Storage Partitions and live specs immediately from remembered state
    renderInpageLiveState(agentId);

    const odoNet = document.getElementById('inpage-odo-net');
    if (odoNet) odoNet.innerHTML = '<span>↓ ' + window.CertainStatsChart.formatBytes(agent.total_rx_bytes || 0) + '</span><span>↑ ' + window.CertainStatsChart.formatBytes(agent.total_tx_bytes || 0) + '</span>';

    let totalDiskRead = agent.total_disk_read_bytes || 0;
    let totalDiskWrite = agent.total_disk_write_bytes || 0;
    if (!totalDiskRead && !totalDiskWrite && agent.disks) {
      agent.disks.forEach(d => {
        totalDiskRead += (d.read_bytes || 0);
        totalDiskWrite += (d.write_bytes || 0);
      });
    }
    const odoDisk = document.getElementById('inpage-odo-disk');
    if (odoDisk) odoDisk.innerHTML = '<span>R ' + window.CertainStatsChart.formatBytes(totalDiskRead) + '</span><span>W ' + window.CertainStatsChart.formatBytes(totalDiskWrite) + '</span>';

    // Populate action form IDs and notes preview
    const elDeleteId = document.getElementById('inpage-delete-id'); if (elDeleteId) elDeleteId.value = agent.agent_id;
    syncInpageNotesUI(agent.note || '');
  }

  // 1. Fetch CPU (Usr, IO, Steal)
  Promise.all([
    requestGroup.json(state.panelPath + '/api/metrics?agent_id=' + agentId + '&metric=agent_cpu_usage&' + queryStr).catch(() => ({ series: [] })),
    requestGroup.json(state.panelPath + '/api/metrics?agent_id=' + agentId + '&metric=agent_cpu_iowait&' + queryStr).catch(() => ({ series: [] })),
    requestGroup.json(state.panelPath + '/api/metrics?agent_id=' + agentId + '&metric=agent_cpu_steal&' + queryStr).catch(() => ({ series: [] }))
  ]).then(([resUsr, resIO, resStl]) => {
    if (requestGroup.signal.aborted) return;
    const ptsUsr = (resUsr && resUsr.series && resUsr.series[0]) ? resUsr.series[0].data : [];
    const ptsIO = (resIO && resIO.series && resIO.series[0]) ? resIO.series[0].data : [];
    const ptsStl = (resStl && resStl.series && resStl.series[0]) ? resStl.series[0].data : [];

    const lastUsr = ptsUsr.length ? ptsUsr[ptsUsr.length - 1][1] : null;
    const lastIO = ptsIO.length ? ptsIO[ptsIO.length - 1][1] : null;
    const lastStl = ptsStl.length ? ptsStl[ptsStl.length - 1][1] : null;
    const elUsr = document.getElementById('inpage-live-cpu-usr'); if (elUsr) elUsr.textContent = (lastUsr == null ? "Unavailable" : lastUsr.toFixed(1) + "%");
    const elIO = document.getElementById('inpage-live-cpu-io'); if (elIO) elIO.textContent = (lastIO == null ? "Unavailable" : lastIO.toFixed(1) + "%");
    const elStl = document.getElementById('inpage-live-cpu-stl'); if (elStl) elStl.textContent = (lastStl == null ? "Unavailable" : lastStl.toFixed(1) + "%");

    const seriesList = [
      { label: 'Usr', color: 'var(--s1)', fill: true, data: ptsUsr.map(p => ({ timestamp: p[0], value: p[1] })) },
      { label: 'IO', color: 'var(--s2)', fill: false, data: ptsIO.map(p => ({ timestamp: p[0], value: p[1] })) },
      { label: 'Stl', color: 'var(--s3)', fill: false, data: ptsStl.map(p => ({ timestamp: p[0], value: p[1] })) }
    ];

    if (!state.inpageCpuChart) {
      state.inpageCpuChart = window.CertainStatsChart.renderMultiChart('inpage-chart-cpu', {
        seriesList: seriesList,
        unit: '%',
        maxAdd: 1,
        maxCap: 100,
        hours: hours,
        customRange: customRange,
        queryEndTime: qEnd,
        onZoom: handleInpageZoom
      });
    } else {
      state.inpageCpuChart.updateSeries(seriesList, hours, null, qEnd, customRange, 1);
    }
  });

  // 2. Fetch RAM & Swap
  Promise.all([
    requestGroup.json(state.panelPath + '/api/metrics?agent_id=' + agentId + '&metric=agent_ram_used&' + queryStr).catch(() => ({ series: [] })),
    requestGroup.json(state.panelPath + '/api/metrics?agent_id=' + agentId + '&metric=agent_swap_used&' + queryStr).catch(() => ({ series: [] }))
  ]).then(([resRam, resSwap]) => {
    if (requestGroup.signal.aborted) return;
    const ptsRam = (resRam && resRam.series && resRam.series[0]) ? resRam.series[0].data : [];
    const ptsSwap = (resSwap && resSwap.series && resSwap.series[0]) ? resSwap.series[0].data : [];
    const totalRam = agent ? agent.ram_size : 0;

    const lastRam = ptsRam.length ? ptsRam[ptsRam.length - 1][1] : null;
    const lastSwap = ptsSwap.length ? ptsSwap[ptsSwap.length - 1][1] : null;
    const elRam = document.getElementById('inpage-live-ram-used'); if (elRam) elRam.textContent = window.CertainStatsChart.formatBytes(lastRam);
    const elSwap = document.getElementById('inpage-live-ram-swap'); if (elSwap) elSwap.textContent = window.CertainStatsChart.formatBytes(lastSwap);

    const seriesList = [
      { label: 'RAM', color: 'var(--s4)', fill: true, data: ptsRam.map(p => ({ timestamp: p[0], value: p[1] })) },
      { label: 'Swap', color: 'var(--s5)', fill: false, data: ptsSwap.map(p => ({ timestamp: p[0], value: p[1] })) }
    ];

    if (!state.inpageRamChart) {
      state.inpageRamChart = window.CertainStatsChart.renderMultiChart('inpage-chart-ram', {
        seriesList: seriesList,
        formatter: window.CertainStatsChart.formatBytes,
        yMax: totalRam > 0 ? totalRam : null,
        hours: hours,
        customRange: customRange,
        queryEndTime: qEnd,
        onZoom: handleInpageZoom
      });
    } else {
      state.inpageRamChart.updateSeries(seriesList, hours, totalRam > 0 ? totalRam : null, qEnd, customRange);
    }
  });

  // 3. Fetch Network (RX / TX)
  Promise.all([
    requestGroup.json(state.panelPath + '/api/metrics?agent_id=' + agentId + '&metric=agent_rx_bytes&' + queryStr).catch(() => ({ series: [] })),
    requestGroup.json(state.panelPath + '/api/metrics?agent_id=' + agentId + '&metric=agent_tx_bytes&' + queryStr).catch(() => ({ series: [] }))
  ]).then(([resRx, resTx]) => {
    if (requestGroup.signal.aborted) return;
    const ptsRx = (resRx && resRx.series && resRx.series[0]) ? resRx.series[0].data : [];
    const ptsTx = (resTx && resTx.series && resTx.series[0]) ? resTx.series[0].data : [];

    const rateRx = (resRx?.series?.[0]?.rate_data || window.CertainStatsChart.convertDeltaToRate(ptsRx));
    const rateTx = (resTx?.series?.[0]?.rate_data || window.CertainStatsChart.convertDeltaToRate(ptsTx));
    const lastRx = rateRx.length ? rateRx[rateRx.length - 1][1] : null;
    const lastTx = rateTx.length ? rateTx[rateTx.length - 1][1] : null;
    const elRx = document.getElementById('inpage-live-net-rx'); if (elRx) elRx.textContent = '↓ ' + window.CertainStatsChart.formatBps(lastRx);
    const elTx = document.getElementById('inpage-live-net-tx'); if (elTx) elTx.textContent = '↑ ' + window.CertainStatsChart.formatBps(lastTx);

    const seriesList = [
      { label: 'RX', color: 'var(--rx)', fill: false, data: rateRx.map(p => ({ timestamp: p[0], value: p[1] })) },
      { label: 'TX', color: 'var(--tx)', fill: false, data: rateTx.map(p => ({ timestamp: p[0], value: p[1] })) }
    ];

    if (!state.inpageNetChart) {
      state.inpageNetChart = window.CertainStatsChart.renderMultiChart('inpage-chart-net', {
        seriesList: seriesList,
        formatter: window.CertainStatsChart.formatBps,
        hours: hours,
        customRange: customRange,
        queryEndTime: qEnd,
        onZoom: handleInpageZoom
      });
    } else {
      state.inpageNetChart.updateSeries(seriesList, hours, undefined, qEnd, customRange);
    }
  });

  // 4. Fetch Disk Usage & Disk I/O Rates (Separated Per Disk Partition)
  Promise.all([
    requestGroup.json(state.panelPath + '/api/metrics?agent_id=' + agentId + '&metric=agent_disk_used&' + queryStr).catch(() => ({ series: [] })),
    requestGroup.json(state.panelPath + '/api/metrics?agent_id=' + agentId + '&metric=agent_disk_read_bytes&' + queryStr).catch(() => ({ series: [] })),
    requestGroup.json(state.panelPath + '/api/metrics?agent_id=' + agentId + '&metric=agent_disk_write_bytes&' + queryStr).catch(() => ({ series: [] }))
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
    const container = document.getElementById('inpage-disk-charts-grid');
    if (!container) return;

    const expectedIds = paths.map(p => 'inpage-disk-card-usage-' + safeId(p)).join(',');
    const currentIds = Array.from(container.children).map(c => c.id).filter(id => id.startsWith('inpage-disk-card-usage-')).join(',');

    if (expectedIds !== currentIds) {
      Object.values(state.inpageDiskCharts).forEach(dc => {
        if (dc.usageChart && dc.usageChart.destroy) dc.usageChart.destroy();
        if (dc.ioChart && dc.ioChart.destroy) dc.ioChart.destroy();
      });
      state.inpageDiskCharts = {};

      window.CertainStatsDiskDOM.render(container, paths, 'inpage-');
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

      const elUsed = document.getElementById('inpage-live-disk-used-' + safe); if (elUsed) elUsed.textContent = window.CertainStatsChart.formatBytes(lastUsed);
      const elR = document.getElementById('inpage-live-disk-read-' + safe); if (elR) elR.textContent = window.CertainStatsChart.formatBps(lastR);
      const elW = document.getElementById('inpage-live-disk-write-' + safe); if (elW) elW.textContent = window.CertainStatsChart.formatBps(lastW);

      const usageSeries = [{
        label: 'Used',
        color: 'var(--s1)',
        fill: true,
        data: ptsUsed.map(pt => ({ timestamp: pt[0], value: pt[1] }))
      }];

      const ioSeries = [
        { label: 'Read', color: 'var(--s2)', fill: false, data: rateRead.map(pt => ({ timestamp: pt[0], value: pt[1] })) },
        { label: 'Write', color: 'var(--s3)', fill: false, data: rateWrite.map(pt => ({ timestamp: pt[0], value: pt[1] })) }
      ];

      let diskTotal = 0;
      if (agent && agent.disks) {
        const d = agent.disks.find(x => x.path === p);
        if (d && d.total_bytes > 0) diskTotal = d.total_bytes;
      }
      if (!diskTotal && agent && agent.latest_snap && agent.latest_snap.disks) {
        const sd = agent.latest_snap.disks.find(x => x.path === p);
        if (sd && sd.total_bytes > 0) diskTotal = sd.total_bytes;
      }
      if (!diskTotal && (!p || p === '/')) {
        diskTotal = agent ? (agent.disk_size || 0) : 0;
      }

      if (!state.inpageDiskCharts[p]) {
        const usageChart = window.CertainStatsChart.renderMultiChart('inpage-chart-disk-' + safe, {
          seriesList: usageSeries,
          formatter: window.CertainStatsChart.formatBytes,
          yMax: diskTotal > 0 ? diskTotal : null,
          hours: hours,
          customRange: customRange,
          queryEndTime: qEnd,
          onZoom: handleInpageZoom
        });
        const ioChart = window.CertainStatsChart.renderMultiChart('inpage-chart-disk-io-' + safe, {
          seriesList: ioSeries,
          formatter: window.CertainStatsChart.formatBps,
          hours: hours,
          customRange: customRange,
          queryEndTime: qEnd,
          onZoom: handleInpageZoom
        });
        state.inpageDiskCharts[p] = { usageChart, ioChart };
      } else {
        state.inpageDiskCharts[p].usageChart.updateSeries(usageSeries, hours, diskTotal > 0 ? diskTotal : null, qEnd, customRange);
        state.inpageDiskCharts[p].ioChart.updateSeries(ioSeries, hours, undefined, qEnd, customRange);
      }
    });
  });
}
