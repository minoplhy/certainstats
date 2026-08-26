(function () {
  'use strict';

  let dashId = '';
  let dashSlug = '';
  let maxDays = 30;
  let pubAllowedMetrics = [];
  let pubAgentsData = [];
  let pubLiveMetricsStore = {};
  let currentActivePubId = null;
  let savedPubScrollY = 0;
  let pubInpageCpuChart = null, pubInpageRamChart = null, pubInpageNetChart = null;
  let pubInpageDiskCharts = {}; // { [path]: { usageChart, ioChart } }

  const PUB_METADATA_SYNC_INTERVAL_MS = 300000;
  let lastPubSyncTime = Date.now();

  function safeId(p) {
    return (!p || p === '/') ? 'root' : p.replace(/[^a-zA-Z0-9_-]/g, '_');
  }

  function getPublicBasePath() {
    const publicPath = (document.body?.getAttribute('data-public-path') || '').replace(/\/+$/, '');
    if (dashSlug) {
      if (publicPath && publicPath !== '/') {
        return publicPath.endsWith('/' + dashSlug) ? publicPath : (publicPath + '/' + dashSlug);
      }
      return '/dashboard/' + dashSlug;
    }
    return publicPath || '/dashboard';
  }

  function handlePubItemClick(event, pubId) {
    if (event.ctrlKey || event.metaKey || event.button === 1) {
      return; // Allow standard browser new tab
    }
    event.preventDefault();
    navigateToPubAgent(pubId);
  }

  function navigateToPubAgent(pubId) {
    const basePath = getPublicBasePath();
    const targetUrl = basePath + '/' + pubId;
    if (window.location.pathname !== targetUrl) {
      history.pushState({ pubId: pubId }, '', targetUrl);
    }
    window.dispatchEvent(new PopStateEvent('popstate'));
  }

  function closePublicDetail() {
    const basePath = getPublicBasePath();
    const targetUrl = basePath || '/';
    if (window.location.pathname !== targetUrl) {
      history.pushState({}, '', targetUrl);
    }
    window.dispatchEvent(new PopStateEvent('popstate'));
  }

  function setPublicViewMode(mode) {
    const gridContainer = document.getElementById('public-grid-container');
    const listContainer = document.getElementById('public-list-container');
    const btnGrid = document.getElementById('btn-pub-grid');
    const btnList = document.getElementById('btn-pub-list');

    if (mode === 'list') {
      if (gridContainer) gridContainer.style.display = 'none';
      if (listContainer) listContainer.style.display = 'block';
      if (btnGrid) btnGrid.classList.remove('active');
      if (btnList) btnList.classList.add('active');
    } else {
      if (gridContainer) gridContainer.style.display = 'grid';
      if (listContainer) listContainer.style.display = 'none';
      if (btnGrid) btnGrid.classList.add('active');
      if (btnList) btnList.classList.remove('active');
    }
  }

  function filterPublicMonitors() {
    const query = (document.getElementById('public-search-input').value || '').toLowerCase();
    document.querySelectorAll('.public-monitor-item, .public-monitor-row').forEach(el => {
      const name = (el.getAttribute('data-name') || '').toLowerCase();
      if (name.includes(query)) {
        el.style.display = '';
      } else {
        el.style.display = 'none';
      }
    });
  }

  function renderPubInpageLiveState(pubId) {
    if (!pubId) return;
    const agent = pubAgentsData.find(a => a.public_id === pubId) || {};
    const snap = window.CertainStatsTelemetry.normalizeSnapshot(pubLiveMetricsStore[pubId]) || agent.latest_snap;

    // Render multi-disks immediately from remembered state
    const inpageDisksGrid = document.getElementById('pub-inpage-disks-grid');
    if (inpageDisksGrid && pubAllowedMetrics.includes('agent_disk_used')) {
      const disks = (agent.disks && agent.disks.length > 0) ? agent.disks : [];
      let disksHtml = '';
      if (disks && disks.length > 0) {
        disks.forEach(d => {
          const path = d.path || '/';
          const snapDisk = (snap && snap.disks) ? snap.disks.find(x => x.path === path) : null;
          const used = snapDisk ? snapDisk.used_bytes : (d.used_bytes || 0);
          const total = d.total_bytes || (snapDisk ? snapDisk.total_bytes : 0) || 0;
          const pct = total > 0 ? (used / total) * 100 : 0;
          disksHtml += `
            <div class="hw-card">
              <div class="hw-card-header">
                <span>⛁ ${path}</span>
                <span class="mono" style="color: var(--text-secondary); font-weight: 600;">${pct > 0 ? pct.toFixed(1) + '%' : '-'}</span>
              </div>
              <div class="hw-card-val-row">
                <span class="hw-card-val">${window.CertainStatsChart.formatBytes(used)}</span>
                <span class="hw-card-unit">${total ? ' / ' + window.CertainStatsChart.formatBytes(total) : ''}</span>
              </div>
              <div class="hw-card-progress-track">
                <div class="hw-card-progress-fill seg-disk" style="width: ${Math.min(pct, 100)}%;"></div>
              </div>
              <div style="display: flex; justify-content: space-between; align-items: center; font-size: 12px; margin-top: 10px; padding-top: 8px; border-top: 1px solid var(--border-color);">
                <span style="color: var(--text-secondary); font-weight: 600;">Total:</span>
                <span class="mono" style="font-size: 12px; font-weight: 600; color: var(--text-primary);">R: ${window.CertainStatsChart.formatBytes(d.read_bytes || 0)} <span style="color: var(--border-color); margin: 0 4px;">/</span> W: ${window.CertainStatsChart.formatBytes(d.write_bytes || 0)}</span>
              </div>
            </div>`;
        });
      } else {
        const diskUsed = snap ? (snap.disk_used_bytes || 0) : 0;
        const diskTotal = agent.disk_size || (snap ? snap.disk_total_bytes : 0) || 0;
        const pct = diskTotal > 0 ? (diskUsed / diskTotal) * 100 : 0;
        disksHtml += `
          <div class="hw-card">
            <div class="hw-card-header">
              <span>⛁ /</span>
              <span class="mono" style="color: var(--text-secondary); font-weight: 600;">${pct > 0 ? pct.toFixed(1) + '%' : '-'}</span>
            </div>
            <div class="hw-card-val-row">
              <span class="hw-card-val">${window.CertainStatsChart.formatBytes(diskUsed)}</span>
              <span class="hw-card-unit">${diskTotal ? ' / ' + window.CertainStatsChart.formatBytes(diskTotal) : ''}</span>
            </div>
            <div class="hw-card-progress-track">
              <div class="hw-card-progress-fill seg-disk" style="width: ${Math.min(pct, 100)}%;"></div>
            </div>
            <div style="display: flex; justify-content: space-between; align-items: center; font-size: 12px; margin-top: 10px; padding-top: 8px; border-top: 1px solid var(--border-color);">
              <span style="color: var(--text-secondary); font-weight: 600;">Total:</span>
              <span class="mono" style="font-size: 12px; font-weight: 600; color: var(--text-primary);">R: ${window.CertainStatsChart.formatBytes(agent.total_disk_read_bytes || 0)} <span style="color: var(--border-color); margin: 0 4px;">/</span> W: ${window.CertainStatsChart.formatBytes(agent.total_disk_write_bytes || 0)}</span>
            </div>
          </div>`;
      }
      inpageDisksGrid.innerHTML = disksHtml;
    }

    if (snap) {
      const inpageCpu = document.getElementById('pub-inpage-hw-cpu-bar');
      const inpageRam = document.getElementById('pub-inpage-hw-ram-bar');
      const inpageDisk = document.getElementById('pub-inpage-hw-disk-bar');
      const inpageSwap = document.getElementById('pub-inpage-hw-swap-bar');

      const ramPct = (agent.ram_size && agent.ram_size > 0) ? (snap.ram_used_bytes / agent.ram_size) * 100 : 0;
      const diskPct = (agent.disk_size && agent.disk_size > 0 && snap.disk_used_bytes > 0) ? (snap.disk_used_bytes / agent.disk_size) * 100 : 0;
      const swapPct = (agent.swap_size && agent.swap_size > 0 && snap.ram_swap_used_bytes > 0) ? (snap.ram_swap_used_bytes / agent.swap_size) * 100 : 0;

      if (inpageCpu && pubAllowedMetrics.includes('agent_cpu_usage')) inpageCpu.style.width = Math.min(snap.cpu_usage_percent, 100) + '%';
      if (inpageRam && pubAllowedMetrics.includes('agent_ram_used')) inpageRam.style.width = Math.min(ramPct, 100) + '%';
      if (inpageDisk && diskPct > 0 && pubAllowedMetrics.includes('agent_disk_used')) inpageDisk.style.width = Math.min(diskPct, 100) + '%';
      if (inpageSwap && swapPct > 0 && pubAllowedMetrics.includes('agent_swap_used')) inpageSwap.style.width = Math.min(swapPct, 100) + '%';

      const inpageCpuUsr = document.getElementById('pub-inpage-live-cpu-usr'); if (inpageCpuUsr && pubAllowedMetrics.includes('agent_cpu_usage')) inpageCpuUsr.textContent = snap.cpu_usage_percent.toFixed(1) + '%';
      const inpageCpuIo = document.getElementById('pub-inpage-live-cpu-io'); if (inpageCpuIo && pubAllowedMetrics.includes('agent_cpu_iowait')) inpageCpuIo.textContent = snap.cpu_iowait_percent.toFixed(1) + '%';
      const inpageCpuStl = document.getElementById('pub-inpage-live-cpu-stl'); if (inpageCpuStl && pubAllowedMetrics.includes('agent_cpu_steal')) inpageCpuStl.textContent = snap.cpu_steal_percent.toFixed(1) + '%';
      const inpageRamUsed = document.getElementById('pub-inpage-live-ram-used'); if (inpageRamUsed && pubAllowedMetrics.includes('agent_ram_used')) inpageRamUsed.textContent = window.CertainStatsChart.formatBytes(snap.ram_used_bytes);
      const inpageRamSwap = document.getElementById('pub-inpage-live-ram-swap'); if (inpageRamSwap && pubAllowedMetrics.includes('agent_swap_used')) inpageRamSwap.textContent = window.CertainStatsChart.formatBytes(snap.ram_swap_used_bytes);
      const inpageNetRx = document.getElementById('pub-inpage-live-net-rx'); if (inpageNetRx && pubAllowedMetrics.includes('agent_rx_bytes')) inpageNetRx.textContent = '↓ ' + window.CertainStatsChart.formatBps(snap.rx_bps);
      const inpageNetTx = document.getElementById('pub-inpage-live-net-tx'); if (inpageNetTx && pubAllowedMetrics.includes('agent_tx_bytes')) inpageNetTx.textContent = '↑ ' + window.CertainStatsChart.formatBps(snap.tx_bps);

      if (snap.uptime != null) {
        const inpageUptimeEl = document.getElementById('pub-inpage-spec-uptime');
        if (inpageUptimeEl) inpageUptimeEl.textContent = window.CertainStatsTelemetry.formatUptime(snap.uptime);
      }
    }
  }

  function loadPublicDetailMetrics(pubId, hours, customRange) {
    const publicPath = (document.body?.getAttribute('data-public-path') || '').replace(/\/+$/, '');
    const agent = pubAgentsData.find(a => a.public_id === pubId);
    let baseQuery = 'dashboard_id=' + encodeURIComponent(dashId) + '&agent_id=' + encodeURIComponent(pubId);
    if (customRange && customRange.start && customRange.end) {
      baseQuery += '&start=' + customRange.start + '&end=' + customRange.end;
    } else {
      baseQuery += '&hours=' + hours;
    }

    if (agent) {
      const isOnline = agent.is_online === true || (agent.last_seen && (Date.now() - new Date(agent.last_seen).getTime()) < 120000);
      const dotEl = document.getElementById('pub-inpage-dot');
      const badgeEl = document.getElementById('pub-inpage-badge');
      if (dotEl) dotEl.className = 'status-dot ' + (isOnline ? 'online' : 'offline');
      if (badgeEl) {
        badgeEl.className = 'badge ' + (isOnline ? 'badge-online' : 'badge-offline');
        badgeEl.textContent = isOnline ? 'Online' : 'Offline';
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

      const pubIdBadge = document.getElementById('pub-inpage-spec-id');
      if (pubIdBadge) {
        pubIdBadge.textContent = agent.public_id;
        pubIdBadge.setAttribute('data-full-id', agent.public_id);
        pubIdBadge.title = 'Click to copy: ' + agent.public_id;
      }
      const elUptime = document.getElementById('pub-inpage-spec-uptime');
      if (elUptime) elUptime.textContent = window.CertainStatsTelemetry.formatUptime(agent.uptime);
      const elKernel = document.getElementById('pub-inpage-spec-kernel');
      if (elKernel) elKernel.textContent = agent.linux_version || 'Linux';
      const elCpuModel = document.getElementById('pub-inpage-spec-cpu');
      if (elCpuModel) elCpuModel.textContent = agent.cpu_model || 'Generic CPU';

      // Render dynamic multi-disk Storage Partitions and live specs immediately from remembered state
      renderPubInpageLiveState(pubId);

      const odoNet = document.getElementById('pub-inpage-odo-net');
      if (odoNet) odoNet.innerHTML = '<span>↓ ' + window.CertainStatsChart.formatBytes(agent.total_rx_bytes || 0) + '</span> / <span>↑ ' + window.CertainStatsChart.formatBytes(agent.total_tx_bytes || 0) + '</span>';

      let totalDiskRead = agent.total_disk_read_bytes || 0;
      let totalDiskWrite = agent.total_disk_write_bytes || 0;
      if (!totalDiskRead && !totalDiskWrite && agent.disks) {
        agent.disks.forEach(d => {
          totalDiskRead += (d.read_bytes || 0);
          totalDiskWrite += (d.write_bytes || 0);
        });
      }
      const odoDisk = document.getElementById('pub-inpage-odo-disk');
      if (odoDisk) odoDisk.innerHTML = '<span>R: ' + window.CertainStatsChart.formatBytes(totalDiskRead) + '</span> / <span>W: ' + window.CertainStatsChart.formatBytes(totalDiskWrite) + '</span>';
    }

    // 1. Fetch CPU (Usr, IO, Steal)
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
        const elUsr = document.getElementById('pub-inpage-live-cpu-usr'); if (elUsr) elUsr.textContent = lastUsr.toFixed(1) + '%';
        const elIO = document.getElementById('pub-inpage-live-cpu-io'); if (elIO) elIO.textContent = lastIO.toFixed(1) + '%';
        const elStl = document.getElementById('pub-inpage-live-cpu-stl'); if (elStl) elStl.textContent = lastStl.toFixed(1) + '%';

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

        if (!pubInpageCpuChart) {
          pubInpageCpuChart = window.CertainStatsChart.renderMultiChart('pub-inpage-chart-cpu', {
            seriesList: seriesList,
            unit: '%',
            maxAdd: 1,
            maxCap: 100,
            hours: hours
          });
        } else {
          pubInpageCpuChart.updateSeries(seriesList, hours, null, undefined, undefined, 1);
        }
      });
    }

    // 2. Fetch RAM (RAM, Swap)
    if (pubAllowedMetrics.includes('agent_ram_used') || pubAllowedMetrics.includes('agent_swap_used')) {
      Promise.all([
        pubAllowedMetrics.includes('agent_ram_used') ? fetch(publicPath + '/api/public/metrics?' + baseQuery + '&metric=agent_ram_used').then(r => r.json()).catch(() => ({ series: [] })) : Promise.resolve({ series: [] }),
        pubAllowedMetrics.includes('agent_swap_used') ? fetch(publicPath + '/api/public/metrics?' + baseQuery + '&metric=agent_swap_used').then(r => r.json()).catch(() => ({ series: [] })) : Promise.resolve({ series: [] })
      ]).then(([resRam, resSwap]) => {
        const ptsRam = (resRam && resRam.series && resRam.series[0]) ? resRam.series[0].data : [];
        const ptsSwap = (resSwap && resSwap.series && resSwap.series[0]) ? resSwap.series[0].data : [];
        const maxCapacity = Math.max(agent ? agent.ram_size : 0, agent ? agent.swap_size : 0);

        const lastRam = ptsRam.length ? ptsRam[ptsRam.length - 1][1] : 0;
        const lastSwap = ptsSwap.length ? ptsSwap[ptsSwap.length - 1][1] : 0;
        const elRam = document.getElementById('pub-inpage-live-ram-used'); if (elRam) elRam.textContent = window.CertainStatsChart.formatBytes(lastRam);
        const elSwap = document.getElementById('pub-inpage-live-ram-swap'); if (elSwap) elSwap.textContent = window.CertainStatsChart.formatBytes(lastSwap);

        const seriesList = [];
        if (pubAllowedMetrics.includes('agent_ram_used')) {
          seriesList.push({ label: 'RAM', color: '#14b8a6', fill: false, data: ptsRam.map(p => ({ timestamp: p[0], value: p[1] })) });
        }
        if (pubAllowedMetrics.includes('agent_swap_used')) {
          seriesList.push({ label: 'Swap', color: '#94a3b8', fill: false, data: ptsSwap.map(p => ({ timestamp: p[0], value: p[1] })) });
        }

        if (!pubInpageRamChart) {
          pubInpageRamChart = window.CertainStatsChart.renderMultiChart('pub-inpage-chart-ram', {
            seriesList: seriesList,
            formatter: window.CertainStatsChart.formatBytes,
            yMax: maxCapacity > 0 ? maxCapacity : null,
            hours: hours
          });
        } else {
          pubInpageRamChart.updateSeries(seriesList, hours, maxCapacity > 0 ? maxCapacity : null);
        }
      });
    }

    // 3. Fetch Network RX/TX
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
        const elRx = document.getElementById('pub-inpage-live-net-rx'); if (elRx) elRx.textContent = '↓ ' + window.CertainStatsChart.formatBps(lastRx);
        const elTx = document.getElementById('pub-inpage-live-net-tx'); if (elTx) elTx.textContent = '↑ ' + window.CertainStatsChart.formatBps(lastTx);

        const seriesList = [];
        if (pubAllowedMetrics.includes('agent_rx_bytes')) {
          seriesList.push({ label: 'RX', color: '#1e40af', fill: false, data: rateRx.map(p => ({ timestamp: p[0], value: p[1] })) });
        }
        if (pubAllowedMetrics.includes('agent_tx_bytes')) {
          seriesList.push({ label: 'TX', color: '#7e22ce', fill: false, data: rateTx.map(p => ({ timestamp: p[0], value: p[1] })) });
        }

        if (!pubInpageNetChart) {
          pubInpageNetChart = window.CertainStatsChart.renderMultiChart('pub-inpage-chart-net', {
            seriesList: seriesList,
            formatter: window.CertainStatsChart.formatBps,
            hours: hours
          });
        } else {
          pubInpageNetChart.updateSeries(seriesList, hours, undefined);
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
        const container = document.getElementById('pub-inpage-disk-charts-grid');
        if (!container) return;

        const expectedIds = paths.map(p => 'pub-inpage-disk-card-usage-' + safeId(p)).join(',');
        const currentIds = Array.from(container.children).map(c => c.id).filter(id => id.startsWith('pub-inpage-disk-card-usage-')).join(',');

        if (expectedIds !== currentIds) {
          Object.values(pubInpageDiskCharts).forEach(dc => {
            if (dc.usageChart && dc.usageChart.destroy) dc.usageChart.destroy();
            if (dc.ioChart && dc.ioChart.destroy) dc.ioChart.destroy();
          });
          pubInpageDiskCharts = {};

          let html = '';
          paths.forEach(p => {
            const safe = safeId(p);
            html += `
              <div class="card" id="pub-inpage-disk-card-usage-${safe}" style="${pubAllowedMetrics.includes('agent_disk_used') ? '' : 'display:none;'}">
                <div class="chart-header-row">
                  <h3 class="chart-header-title">Disk Usage (${p})</h3>
                  <div class="chart-legend-pills">
                    <span class="chart-legend-item"><span class="chart-legend-dot" style="background-color: #8b5cf6;"></span>Used: <span class="chart-legend-val" id="pub-inpage-live-disk-used-${safe}">0 B</span></span>
                  </div>
                </div>
                <div class="chart-container" style="height: 220px;">
                  <canvas id="pub-inpage-chart-disk-${safe}" class="chart-canvas"></canvas>
                </div>
              </div>
              <div class="card" id="pub-inpage-disk-card-io-${safe}" style="${(pubAllowedMetrics.includes('agent_disk_read_bytes') || pubAllowedMetrics.includes('agent_disk_write_bytes')) ? '' : 'display:none;'}">
                <div class="chart-header-row">
                  <h3 class="chart-header-title">Disk I/O Rate (${p})</h3>
                  <div class="chart-legend-pills">
                    ${pubAllowedMetrics.includes('agent_disk_read_bytes') ? `<span class="chart-legend-item"><span class="chart-legend-dot" style="background-color: #fb923c;"></span>Read: <span class="chart-legend-val" id="pub-inpage-live-disk-read-${safe}">0 B/s</span></span>` : ''}
                    ${pubAllowedMetrics.includes('agent_disk_write_bytes') ? `<span class="chart-legend-item"><span class="chart-legend-dot" style="background-color: #ef4444;"></span>Write: <span class="chart-legend-val" id="pub-inpage-live-disk-write-${safe}">0 B/s</span></span>` : ''}
                  </div>
                </div>
                <div class="chart-container" style="height: 220px;">
                  <canvas id="pub-inpage-chart-disk-io-${safe}" class="chart-canvas"></canvas>
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

          const elUsed = document.getElementById('pub-inpage-live-disk-used-' + safe); if (elUsed) elUsed.textContent = window.CertainStatsChart.formatBytes(lastUsed);
          const elR = document.getElementById('pub-inpage-live-disk-read-' + safe); if (elR) elR.textContent = window.CertainStatsChart.formatBps(lastR);
          const elW = document.getElementById('pub-inpage-live-disk-write-' + safe); if (elW) elW.textContent = window.CertainStatsChart.formatBps(lastW);

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

          if (!pubInpageDiskCharts[p]) {
            let usageChart = null;
            if (pubAllowedMetrics.includes('agent_disk_used')) {
              usageChart = window.CertainStatsChart.renderMultiChart('pub-inpage-chart-disk-' + safe, {
                seriesList: usageSeries,
                formatter: window.CertainStatsChart.formatBytes,
                yMax: totalDisk > 0 ? totalDisk : null,
                hours: hours
              });
            }
            let ioChart = null;
            if (pubAllowedMetrics.includes('agent_disk_read_bytes') || pubAllowedMetrics.includes('agent_disk_write_bytes')) {
              ioChart = window.CertainStatsChart.renderMultiChart('pub-inpage-chart-disk-io-' + safe, {
                seriesList: ioSeries,
                formatter: window.CertainStatsChart.formatBps,
                hours: hours
              });
            }
            pubInpageDiskCharts[p] = { usageChart, ioChart };
          } else {
            if (pubInpageDiskCharts[p].usageChart) {
              pubInpageDiskCharts[p].usageChart.updateSeries(usageSeries, hours, totalDisk > 0 ? totalDisk : null);
            }
            if (pubInpageDiskCharts[p].ioChart) {
              pubInpageDiskCharts[p].ioChart.updateSeries(ioSeries, hours, undefined);
            }
          }
        });
      });
    }
  }

  function applyPublicTelemetryUpdates(snaps) {
    if (snaps && typeof snaps === 'object') {
      Object.assign(pubLiveMetricsStore, snaps);
    }
    window.CertainStatsTelemetry.renderClusterStats('pub-', pubAgentsData, pubLiveMetricsStore);

    for (const id in snaps) {
      const snap = window.CertainStatsTelemetry.normalizeSnapshot(snaps[id]);
      if (!snap) continue;

      const agent = pubAgentsData.find(a => a.public_id === id) || {};
      agent.latest_snap = snap;
      if (snap.disks && snap.disks.length > 0 && agent.disks) {
        snap.disks.forEach(sd => {
          const d = agent.disks.find(x => x.path === (sd.path || '/'));
          if (d) {
            d.used_bytes = sd.used_bytes;
            if (sd.total_bytes) d.total_bytes = sd.total_bytes;
          }
        });
      }

      // Update CPU stacked bar & values
      const segUsr = document.getElementById('pub-seg-cpu-usr-' + id);
      const segIo = document.getElementById('pub-seg-cpu-io-' + id);
      const segStl = document.getElementById('pub-seg-cpu-stl-' + id);
      const valCpu = document.getElementById('pub-val-cpu-' + id);
      const tdSegCpu = document.getElementById('pub-td-seg-cpu-' + id);
      const tdCpu = document.getElementById('pub-td-cpu-' + id);

      if (valCpu) valCpu.textContent = snap.cpu_usage_percent.toFixed(1) + '%';
      if (tdCpu) tdCpu.textContent = snap.cpu_usage_percent.toFixed(1) + '%';
      if (segUsr) segUsr.style.width = Math.min(snap.cpu_usage_percent, 100) + '%';
      if (segIo) segIo.style.width = Math.min(snap.cpu_iowait_percent, 100) + '%';
      if (segStl) segStl.style.width = Math.min(snap.cpu_steal_percent, 100) + '%';
      if (tdSegCpu) tdSegCpu.style.width = Math.min(snap.cpu_usage_percent, 100) + '%';

      // Update RAM stacked bar & values
      const segRam = document.getElementById('pub-seg-ram-used-' + id);
      const segSwap = document.getElementById('pub-seg-ram-swap-' + id);
      const valRam = document.getElementById('pub-val-ram-' + id);
      const tdSegRam = document.getElementById('pub-td-seg-ram-' + id);
      const tdRam = document.getElementById('pub-td-ram-' + id);

      const ramPct = (agent.ram_size && agent.ram_size > 0) ? (snap.ram_used_bytes / agent.ram_size) * 100 : 0;
      const swapPct = (agent.swap_size && agent.swap_size > 0) ? (snap.ram_swap_used_bytes / agent.swap_size) * 100 : 0;

      if (valRam) valRam.textContent = window.CertainStatsChart.formatBytes(snap.ram_used_bytes) + (agent.ram_size ? ' / ' + window.CertainStatsChart.formatBytes(agent.ram_size) : '');
      if (tdRam) tdRam.textContent = window.CertainStatsChart.formatBytes(snap.ram_used_bytes);
      if (segRam) segRam.style.width = Math.min(ramPct, 100) + '%';
      if (segSwap) segSwap.style.width = Math.min(swapPct, 100) + '%';
      if (tdSegRam) tdSegRam.style.width = Math.min(ramPct, 100) + '%';

      // Update Disk bars (Dynamic Multi-Disk)
      const diskBarsGroup = document.getElementById('pub-disk-bars-group-' + id);
      let pubDiskUsed = snap.disk_used_bytes || 0;
      let pubDiskTotal = agent.disk_size || snap.disk_total_bytes || 0;
      if (snap.disks && snap.disks.length > 0) {
        let sumUsed = 0, sumTotal = 0;
        snap.disks.forEach(d => {
          sumUsed += d.used_bytes || 0;
          sumTotal += d.total_bytes || 0;
        });
        if (sumUsed > 0) pubDiskUsed = sumUsed;
        if (sumTotal > 0 && (!pubDiskTotal || pubDiskTotal < sumTotal)) pubDiskTotal = sumTotal;
      }
      const diskPct = (pubDiskTotal > 0 && pubDiskUsed > 0 && pubAllowedMetrics.includes('agent_disk_used')) ? (pubDiskUsed / pubDiskTotal) * 100 : 0;

      if (diskBarsGroup && pubAllowedMetrics.includes('agent_disk_used')) {
        if (snap.disks && snap.disks.length > 1) {
          let barsHtml = '';
          snap.disks.forEach(d => {
            const path = d.path || '/';
            const label = path === '/' ? 'DISK' : `DISK (${path})`;
            const used = d.used_bytes || 0;
            const total = d.total_bytes || 0;
            const pct = total > 0 ? (used / total) * 100 : 0;
            barsHtml += `
              <div class="usage-bar-group">
                <div class="usage-bar-label">
                  <span class="title">${label}</span>
                  <span class="value">${window.CertainStatsChart.formatBytes(used)}${total ? ' / ' + window.CertainStatsChart.formatBytes(total) : ''}</span>
                </div>
                <div class="usage-bar-track">
                  <div class="usage-segment seg-disk" style="width: ${Math.min(pct, 100)}%;"></div>
                </div>
              </div>`;
          });
          diskBarsGroup.innerHTML = barsHtml;
        } else {
          const segDisk = document.getElementById('pub-seg-disk-' + id);
          const valDisk = document.getElementById('pub-val-disk-' + id);
          if (valDisk && pubDiskUsed > 0) {
            valDisk.textContent = window.CertainStatsChart.formatBytes(pubDiskUsed) + (pubDiskTotal ? ' / ' + window.CertainStatsChart.formatBytes(pubDiskTotal) : '');
          }
          if (segDisk && diskPct > 0) {
            segDisk.style.width = Math.min(diskPct, 100) + '%';
          }
        }
      }

      const pubTrackDisk = document.getElementById('pub-track-disk-' + id);
      if (pubTrackDisk && snap.disks && snap.disks.length > 1 && pubAllowedMetrics.includes('agent_disk_used')) {
        const rows = snap.disks.map(d => ({
          label: d.path ? `Disk (${d.path})` : 'Disk',
          val: window.CertainStatsChart.formatBytes(d.used_bytes) + (d.total_bytes ? ' / ' + window.CertainStatsChart.formatBytes(d.total_bytes) : ''),
          color: '#8b5cf6'
        }));
        pubTrackDisk.setAttribute('data-tooltip-rows', JSON.stringify(rows));
        pubTrackDisk.setAttribute('data-tooltip-header', 'Storage Partitions');
      }

      // Update Network bar
      const segRx = document.getElementById('pub-seg-net-rx-' + id);
      const segTx = document.getElementById('pub-seg-net-tx-' + id);
      const tdSegNetRx = document.getElementById('pub-td-seg-net-rx-' + id);
      const tdSegNetTx = document.getElementById('pub-td-seg-net-tx-' + id);
      const valNet = document.getElementById('pub-val-net-' + id);
      if (valNet) {
        valNet.textContent = '↓ ' + window.CertainStatsChart.formatBps(snap.rx_bps) + ' / ↑ ' + window.CertainStatsChart.formatBps(snap.tx_bps);
      }
      const tot = snap.rx_bps + snap.tx_bps;
      if (tot > 0) {
        if (segRx) segRx.style.width = ((snap.rx_bps / tot) * 100) + '%';
        if (segTx) segTx.style.width = ((snap.tx_bps / tot) * 100) + '%';
        if (tdSegNetRx) tdSegNetRx.style.width = ((snap.rx_bps / tot) * 100) + '%';
        if (tdSegNetTx) tdSegNetTx.style.width = ((snap.tx_bps / tot) * 100) + '%';
      } else {
        if (segRx) segRx.style.width = '0%';
        if (segTx) segTx.style.width = '0%';
        if (tdSegNetRx) tdSegNetRx.style.width = '0%';
        if (tdSegNetTx) tdSegNetTx.style.width = '0%';
      }

      // Update Tooltips
      const trackCpu = document.getElementById('pub-track-cpu-' + id);
      if (trackCpu) {
        const rows = [];
        if (pubAllowedMetrics.includes('agent_cpu_usage')) rows.push({ label: 'Used', val: snap.cpu_usage_percent.toFixed(1) + '%', color: '#3b82f6' });
        if (pubAllowedMetrics.includes('agent_cpu_iowait')) rows.push({ label: 'IO Wait', val: snap.cpu_iowait_percent.toFixed(1) + '%', color: '#fb923c' });
        if (pubAllowedMetrics.includes('agent_cpu_steal')) rows.push({ label: 'Steal', val: snap.cpu_steal_percent.toFixed(1) + '%', color: '#ef4444' });
        trackCpu.setAttribute('data-tooltip-rows', JSON.stringify(rows));
      }
      const trackRam = document.getElementById('pub-track-ram-' + id);
      if (trackRam) {
        const rows = [];
        if (pubAllowedMetrics.includes('agent_ram_used')) rows.push({ label: 'RAM Used', val: window.CertainStatsChart.formatBytes(snap.ram_used_bytes), color: '#14b8a6' });
        if (pubAllowedMetrics.includes('agent_swap_used')) rows.push({ label: 'Swap Used', val: window.CertainStatsChart.formatBytes(snap.ram_swap_used_bytes), color: '#4b5563' });
        trackRam.setAttribute('data-tooltip-rows', JSON.stringify(rows));
      }
      const trackNet = document.getElementById('pub-track-net-' + id);
      if (trackNet) {
        const rows = [];
        if (pubAllowedMetrics.includes('agent_rx_bytes')) rows.push({ label: 'Download (RX)', val: window.CertainStatsChart.formatBps(snap.rx_bps), color: '#1e40af' });
        if (pubAllowedMetrics.includes('agent_tx_bytes')) rows.push({ label: 'Upload (TX)', val: window.CertainStatsChart.formatBps(snap.tx_bps), color: '#7e22ce' });
        trackNet.setAttribute('data-tooltip-rows', JSON.stringify(rows));
      }
      const tdTrackNet = document.getElementById('pub-td-track-net-' + id);
      if (tdTrackNet) {
        const rows = [];
        if (pubAllowedMetrics.includes('agent_rx_bytes')) rows.push({ label: 'Download (RX)', val: window.CertainStatsChart.formatBps(snap.rx_bps), color: '#1e40af' });
        if (pubAllowedMetrics.includes('agent_tx_bytes')) rows.push({ label: 'Upload (TX)', val: window.CertainStatsChart.formatBps(snap.tx_bps), color: '#7e22ce' });
        tdTrackNet.setAttribute('data-tooltip-rows', JSON.stringify(rows));
      }

      if (snap.uptime != null) {
        agent.uptime = snap.uptime;
        const uptimeEl = document.getElementById('pub-uptime-' + id);
        const tdUptimeEl = document.getElementById('pub-td-uptime-' + id);
        const formatted = window.CertainStatsTelemetry.formatUptime(snap.uptime);
        if (uptimeEl) uptimeEl.innerHTML = 'Uptime: <strong class="mono" style="color: var(--text-secondary);">' + formatted + '</strong>';
        if (tdUptimeEl) tdUptimeEl.textContent = formatted;
      }

      // Update in-page detail if currently viewed
      if (currentActivePubId === id) {
        const inpageCpu = document.getElementById('pub-inpage-hw-cpu-bar');
        const inpageRam = document.getElementById('pub-inpage-hw-ram-bar');
        const inpageDisk = document.getElementById('pub-inpage-hw-disk-bar');
        const inpageSwap = document.getElementById('pub-inpage-hw-swap-bar');

        if (inpageCpu && pubAllowedMetrics.includes('agent_cpu_usage')) inpageCpu.style.width = Math.min(snap.cpu_usage_percent, 100) + '%';
        if (inpageRam && pubAllowedMetrics.includes('agent_ram_used')) inpageRam.style.width = Math.min(ramPct, 100) + '%';
        if (inpageDisk && agent.disk_size > 0 && snap.disk_used_bytes > 0 && pubAllowedMetrics.includes('agent_disk_used')) {
          inpageDisk.style.width = Math.min(diskPct, 100) + '%';
        }
        if (inpageSwap && agent.swap_size > 0 && snap.ram_swap_used_bytes > 0 && pubAllowedMetrics.includes('agent_swap_used')) {
          inpageSwap.style.width = Math.min(swapPct, 100) + '%';
        }

        // Update in-page Disks Section and live specs
        renderPubInpageLiveState(id);

        const inpageCpuUsr = document.getElementById('pub-inpage-live-cpu-usr'); if (inpageCpuUsr && pubAllowedMetrics.includes('agent_cpu_usage')) inpageCpuUsr.textContent = snap.cpu_usage_percent.toFixed(1) + '%';
        const inpageCpuIo = document.getElementById('pub-inpage-live-cpu-io'); if (inpageCpuIo && pubAllowedMetrics.includes('agent_cpu_iowait')) inpageCpuIo.textContent = snap.cpu_iowait_percent.toFixed(1) + '%';
        const inpageCpuStl = document.getElementById('pub-inpage-live-cpu-stl'); if (inpageCpuStl && pubAllowedMetrics.includes('agent_cpu_steal')) inpageCpuStl.textContent = snap.cpu_steal_percent.toFixed(1) + '%';
        const inpageRamUsed = document.getElementById('pub-inpage-live-ram-used'); if (inpageRamUsed && pubAllowedMetrics.includes('agent_ram_used')) inpageRamUsed.textContent = window.CertainStatsChart.formatBytes(snap.ram_used_bytes);
        const inpageRamSwap = document.getElementById('pub-inpage-live-ram-swap'); if (inpageRamSwap && pubAllowedMetrics.includes('agent_swap_used')) inpageRamSwap.textContent = window.CertainStatsChart.formatBytes(snap.ram_swap_used_bytes);
        const inpageNetRx = document.getElementById('pub-inpage-live-net-rx'); if (inpageNetRx && pubAllowedMetrics.includes('agent_rx_bytes')) inpageNetRx.textContent = '↓ ' + window.CertainStatsChart.formatBps(snap.rx_bps);
        const inpageNetTx = document.getElementById('pub-inpage-live-net-tx'); if (inpageNetTx && pubAllowedMetrics.includes('agent_tx_bytes')) inpageNetTx.textContent = '↑ ' + window.CertainStatsChart.formatBps(snap.tx_bps);

        if (snap.uptime != null) {
          const inpageUptimeEl = document.getElementById('pub-inpage-spec-uptime');
          if (inpageUptimeEl) inpageUptimeEl.textContent = window.CertainStatsTelemetry.formatUptime(snap.uptime);
        }

        const ts = Date.now();
        if (pubInpageCpuChart) {
          const map = {};
          if (pubAllowedMetrics.includes('agent_cpu_usage')) map['Usr'] = snap.cpu_usage_percent;
          if (pubAllowedMetrics.includes('agent_cpu_iowait')) map['IO'] = snap.cpu_iowait_percent;
          if (pubAllowedMetrics.includes('agent_cpu_steal')) map['Stl'] = snap.cpu_steal_percent;
          pubInpageCpuChart.updateLivePoint(ts, map);
        }
        if (pubInpageRamChart) {
          const map = {};
          if (pubAllowedMetrics.includes('agent_ram_used')) map['RAM'] = snap.ram_used_bytes;
          if (pubAllowedMetrics.includes('agent_swap_used')) map['Swap'] = snap.ram_swap_used_bytes;
          pubInpageRamChart.updateLivePoint(ts, map);
        }
        if (pubInpageNetChart) {
          const map = {};
          if (pubAllowedMetrics.includes('agent_rx_bytes')) map['RX'] = snap.rx_bps;
          if (pubAllowedMetrics.includes('agent_tx_bytes')) map['TX'] = snap.tx_bps;
          pubInpageNetChart.updateLivePoint(ts, map);
        }

        if (snap.disks && snap.disks.length > 0) {
          snap.disks.forEach(d => {
            const path = d.path || '/';
            const safe = safeId(path);
            const elUsed = document.getElementById('pub-inpage-live-disk-used-' + safe); if (elUsed && pubAllowedMetrics.includes('agent_disk_used')) elUsed.textContent = window.CertainStatsChart.formatBytes(d.used_bytes);
            const elR = document.getElementById('pub-inpage-live-disk-read-' + safe); if (elR && pubAllowedMetrics.includes('agent_disk_read_bytes')) elR.textContent = window.CertainStatsChart.formatBps(d.read_bytes || 0);
            const elW = document.getElementById('pub-inpage-live-disk-write-' + safe); if (elW && pubAllowedMetrics.includes('agent_disk_write_bytes')) elW.textContent = window.CertainStatsChart.formatBps(d.write_bytes || 0);

            if (pubInpageDiskCharts[path]) {
              if (pubInpageDiskCharts[path].usageChart && pubAllowedMetrics.includes('agent_disk_used')) {
                pubInpageDiskCharts[path].usageChart.updateLivePoint(ts, { Used: d.used_bytes });
              }
              if (pubInpageDiskCharts[path].ioChart) {
                const map = {};
                if (pubAllowedMetrics.includes('agent_disk_read_bytes')) map['Read'] = d.read_bytes || 0;
                if (pubAllowedMetrics.includes('agent_disk_write_bytes')) map['Write'] = d.write_bytes || 0;
                pubInpageDiskCharts[path].ioChart.updateLivePoint(ts, map);
              }
            }
          });
        } else {
          const elUsed = document.getElementById('pub-inpage-live-disk-used-root'); if (elUsed && pubAllowedMetrics.includes('agent_disk_used')) elUsed.textContent = window.CertainStatsChart.formatBytes(snap.disk_used_bytes);
          const elR = document.getElementById('pub-inpage-live-disk-read-root'); if (elR && pubAllowedMetrics.includes('agent_disk_read_bytes')) elR.textContent = window.CertainStatsChart.formatBps(snap.disk_read_bps);
          const elW = document.getElementById('pub-inpage-live-disk-write-root'); if (elW && pubAllowedMetrics.includes('agent_disk_write_bytes')) elW.textContent = window.CertainStatsChart.formatBps(snap.disk_write_bps);

          if (pubInpageDiskCharts['/']) {
            if (pubInpageDiskCharts['/'].usageChart && pubAllowedMetrics.includes('agent_disk_used')) {
              pubInpageDiskCharts['/'].usageChart.updateLivePoint(ts, { Used: snap.disk_used_bytes });
            }
            if (pubInpageDiskCharts['/'].ioChart) {
              const map = {};
              if (pubAllowedMetrics.includes('agent_disk_read_bytes')) map['Read'] = snap.disk_read_bps;
              if (pubAllowedMetrics.includes('agent_disk_write_bytes')) map['Write'] = snap.disk_write_bps;
              pubInpageDiskCharts['/'].ioChart.updateLivePoint(ts, map);
            }
          }
        }
      }
    }
  }

  function syncPublicDashboardMetadata(force) {
    const now = Date.now();
    if (!force && (now - lastPubSyncTime < PUB_METADATA_SYNC_INTERVAL_MS)) return;
    lastPubSyncTime = now;

    const publicPath = (document.body?.getAttribute('data-public-path') || '').replace(/\/+$/, '');
    if (!dashSlug) return;

    fetch(publicPath + '/api/public/dashboard/' + encodeURIComponent(dashSlug))
      .then(r => r.ok ? r.json() : null)
      .then(data => {
        if (!data || !data.agents || !Array.isArray(data.agents)) return;

        data.agents.forEach(fa => {
          let existing = pubAgentsData.find(a => a.public_id === fa.public_id);
          if (!existing) {
            existing = { public_id: fa.public_id };
            pubAgentsData.push(existing);
          }
          if (fa.display_name !== undefined) existing.display_name = fa.display_name;
          if (fa.cpu_model !== undefined) existing.cpu_model = fa.cpu_model;
          if (fa.linux_version !== undefined) existing.linux_version = fa.linux_version;
          if (fa.cpu_cores !== undefined) existing.cpu_cores = fa.cpu_cores;
          if (fa.ram_size !== undefined) existing.ram_size = fa.ram_size;
          if (fa.disk_size !== undefined) existing.disk_size = fa.disk_size;
          if (fa.swap_size !== undefined) existing.swap_size = fa.swap_size;
          if (fa.uptime !== undefined) existing.uptime = fa.uptime;
          if (fa.is_online !== undefined) {
            existing.is_online = fa.is_online;
            const isOnline = !!fa.is_online;
            const cardDot = document.getElementById('pub-dot-' + fa.public_id);
            if (cardDot) cardDot.className = 'status-dot ' + (isOnline ? 'online' : 'offline');
            const tdDot = document.getElementById('pub-td-dot-' + fa.public_id);
            if (tdDot) tdDot.className = 'status-dot ' + (isOnline ? 'online' : 'offline');
            const cardBadge = document.getElementById('pub-badge-' + fa.public_id);
            if (cardBadge) {
              cardBadge.className = 'badge ' + (isOnline ? 'badge-online' : 'badge-offline');
              cardBadge.textContent = isOnline ? 'Online' : 'Offline';
            }
            if (currentActivePubId === fa.public_id) {
              const inpageDot = document.getElementById('pub-inpage-dot');
              if (inpageDot) inpageDot.className = 'status-dot ' + (isOnline ? 'online' : 'offline');
              const inpageBadge = document.getElementById('pub-inpage-badge');
              if (inpageBadge) {
                inpageBadge.className = 'badge ' + (isOnline ? 'badge-online' : 'badge-offline');
                inpageBadge.textContent = isOnline ? 'Online' : 'Offline';
              }
            }
          }

          if (fa.net) {
            if (fa.net.total_rx_bytes !== undefined) existing.total_rx_bytes = fa.net.total_rx_bytes;
            if (fa.net.total_tx_bytes !== undefined) existing.total_tx_bytes = fa.net.total_tx_bytes;
          }
          if (fa.disks && Array.isArray(fa.disks)) {
            existing.disks = fa.disks.map(d => ({
              path: d.path,
              total_bytes: d.total_bytes ?? d.TotalBytes ?? 0,
              read_bytes: d.read_bytes ?? d.ReadBytes ?? 0,
              write_bytes: d.write_bytes ?? d.WriteBytes ?? 0
            }));
          }

          // Update in-page detail if currently viewed
          if (currentActivePubId === fa.public_id) {
            const elCpu = document.getElementById('pub-inpage-hw-cpu');
            if (elCpu && existing.cpu_cores !== undefined) elCpu.textContent = existing.cpu_cores || '-';
            const elRam = document.getElementById('pub-inpage-hw-ram');
            if (elRam && existing.ram_size !== undefined) elRam.textContent = window.CertainStatsChart.formatBytes(existing.ram_size);
            const elDisk = document.getElementById('pub-inpage-hw-disk');
            if (elDisk && existing.disk_size !== undefined) elDisk.textContent = window.CertainStatsChart.formatBytes(existing.disk_size);
            const elSwap = document.getElementById('pub-inpage-hw-swap');
            if (elSwap && existing.swap_size !== undefined) elSwap.textContent = window.CertainStatsChart.formatBytes(existing.swap_size);
            const elKernel = document.getElementById('pub-inpage-spec-kernel');
            if (elKernel && existing.linux_version !== undefined) elKernel.textContent = existing.linux_version || 'Linux';
            const elArch = document.getElementById('pub-inpage-spec-cpu');
            if (elArch && existing.cpu_model !== undefined) elArch.textContent = existing.cpu_model || 'Generic CPU';

            const odoNet = document.getElementById('pub-inpage-odo-net');
            if (odoNet) odoNet.innerHTML = '<span>↓ ' + window.CertainStatsChart.formatBytes(existing.total_rx_bytes || 0) + '</span> / <span>↑ ' + window.CertainStatsChart.formatBytes(existing.total_tx_bytes || 0) + '</span>';
            let dr = 0, dw = 0;
            if (existing.disks) existing.disks.forEach(d => { dr += d.read_bytes || 0; dw += d.write_bytes || 0; });
            const odoDisk = document.getElementById('pub-inpage-odo-disk');
            if (odoDisk) odoDisk.innerHTML = '<span>R: ' + window.CertainStatsChart.formatBytes(dr) + '</span> / <span>W: ' + window.CertainStatsChart.formatBytes(dw) + '</span>';
          }
        });

        // Recalculate and re-render cluster overview totals
        window.CertainStatsTelemetry.renderClusterStats('pub-', pubAgentsData, pubLiveMetricsStore);
      })
      .catch(() => {});
  }

  function init(options) {
    options = options || {};
    dashId = options.dashId || '';
    dashSlug = options.dashSlug || '';
    maxDays = options.maxDays || 30;
    pubAllowedMetrics = options.allowedMetrics || [];
    pubAgentsData = options.agents || [];

    // Initial calculation of cluster stats from static agent data
    window.CertainStatsTelemetry.renderClusterStats('pub-', pubAgentsData, pubLiveMetricsStore);

    const publicPath = (document.body?.getAttribute('data-public-path') || '').replace(/\/+$/, '');
    const wsUrl = (publicPath ? publicPath : '') + '/api/public/ws/' + encodeURIComponent(dashId);

    // 1. Handshake WebSocket immediately without waiting for DOMContentLoaded (0ms delay)
    window.CertainStatsTelemetry.initWebSocket(wsUrl, applyPublicTelemetryUpdates);

    // 2. Setup DOM-dependent UI on DOM Ready
    window.CertainStatsTelemetry.onReady(function() {
      // In-Place Public Router (BASE_PATH/{PUB_AGENT_ID})
      window.CertainStatsTelemetry.initRouter({
        basePath: getPublicBasePath(),
        onNavigate: function(pubId) {
          const overviewView = document.getElementById('public-overview-view');
          const detailView = document.getElementById('public-detail-view');

          if (!pubId) {
            if (detailView) detailView.style.display = 'none';
            if (overviewView) overviewView.style.display = 'block';
            currentActivePubId = null;
            window.scrollTo({ top: savedPubScrollY, behavior: 'instant' });
            return;
          }

          savedPubScrollY = window.scrollY;
          currentActivePubId = pubId;
          if (overviewView) overviewView.style.display = 'none';
          if (detailView) {
            detailView.style.display = 'block';
            window.scrollTo({ top: 0, behavior: 'instant' });

            renderPubInpageLiveState(pubId);

            window.CertainStatsTelemetry.initTimeRangeBar('pub-inpage-time-range-bar', {
              maxDays: maxDays,
              onSelect: function(h) {
                loadPublicDetailMetrics(pubId, h);
              }
            });
            let initPubHours = 6;
            try { initPubHours = parseInt(localStorage.getItem('certainstats_public_active_hours') || '6', 10); } catch (e) {}
            loadPublicDetailMetrics(pubId, isNaN(initPubHours) || initPubHours <= 0 ? 6 : initPubHours);
          }
        }
      });
    });

    setInterval(syncPublicDashboardMetadata, PUB_METADATA_SYNC_INTERVAL_MS);
  }

  // Export module namespace
  window.CertainStatsPublicDashboard = {
    init: init,
    handlePubItemClick: handlePubItemClick,
    navigateToPubAgent: navigateToPubAgent,
    closePublicDetail: closePublicDetail,
    setPublicViewMode: setPublicViewMode,
    filterPublicMonitors: filterPublicMonitors,
    syncPublicDashboardMetadata: syncPublicDashboardMetadata
  };

  // Backwards compatibility globals for inline onclick handlers
  window.handlePubItemClick = handlePubItemClick;
  window.navigateToPubAgent = navigateToPubAgent;
  window.closePublicDetail = closePublicDetail;
  window.setPublicViewMode = setPublicViewMode;
  window.filterPublicMonitors = filterPublicMonitors;
})();
