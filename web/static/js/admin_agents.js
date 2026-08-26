(function () {
  'use strict';

  let panelPath = '';
  let agentsData = [];
  let currentActiveAgentId = null;
  let inpageTimePicker = null;
  let inpageCustomRange = null;
  let inpageCpuChart = null, inpageRamChart = null, inpageNetChart = null;
  let inpageDiskCharts = {};
  let liveMetricsStore = {};
  let savedScrollY = 0;

  const ADMIN_METADATA_SYNC_INTERVAL_MS = 300000;
  let lastAdminSyncTime = Date.now();

  function safeId(p) {
    return (!p || p === '/') ? 'root' : p.replace(/[^a-zA-Z0-9_-]/g, '_');
  }

  function handleAgentItemClick(event, agentId) {
    if (event.ctrlKey || event.metaKey || event.button === 1) {
      return; // Allow standard new tab opening
    }
    event.preventDefault();
    navigateToAgent(agentId);
  }

  function navigateToAgent(agentId) {
    const targetUrl = (panelPath ? panelPath : '') + '/' + agentId;
    if (window.location.pathname !== targetUrl) {
      history.pushState({ agentId: agentId }, '', targetUrl);
    }
    window.dispatchEvent(new PopStateEvent('popstate'));
  }

  function closeAgentDetail() {
    const targetUrl = panelPath ? (panelPath + '/') : '/';
    if (window.location.pathname !== targetUrl) {
      history.pushState({}, '', targetUrl);
    }
    window.dispatchEvent(new PopStateEvent('popstate'));
  }

  function setAgentViewMode(mode) {
    const gridContainer = document.getElementById('agents-grid-container');
    const listContainer = document.getElementById('agents-list-container');
    const btnGrid = document.getElementById('btn-view-grid');
    const btnList = document.getElementById('btn-view-list');

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
    try { localStorage.setItem('certainstats_view_mode', mode); } catch (e) {}
  }

  function setAgentDensity(density) {
    const btnDetailed = document.getElementById('btn-density-detailed');
    const btnSimplified = document.getElementById('btn-density-simplified');
    const detailFields = document.querySelectorAll('.detail-only-field');

    if (density === 'simplified') {
      if (btnDetailed) btnDetailed.classList.remove('active');
      if (btnSimplified) btnSimplified.classList.add('active');
      detailFields.forEach(el => el.style.display = 'none');
    } else {
      if (btnDetailed) btnDetailed.classList.add('active');
      if (btnSimplified) btnSimplified.classList.remove('active');
      detailFields.forEach(el => el.style.display = 'block');
    }
    try { localStorage.setItem('certainstats_density', density); } catch (e) {}
  }

  function filterAgentsList() {
    const input = document.getElementById('agent-search-input');
    const query = (input ? input.value : '').toLowerCase();
    document.querySelectorAll('.agent-card-item, .agent-row-item').forEach(el => {
      const name = (el.getAttribute('data-agent-name') || '').toLowerCase();
      const cpu = (el.getAttribute('data-cpu-model') || '').toLowerCase();
      const id = (el.getAttribute('data-agent-id') || '').toLowerCase();
      if (name.includes(query) || cpu.includes(query) || id.includes(query)) {
        el.style.display = '';
      } else {
        el.style.display = 'none';
      }
    });
  }

  function showInpageReinstallModal() {
    if (!currentActiveAgentId) return;
    const agent = agentsData.find(a => a.agent_id === currentActiveAgentId);
    window.CertainStatsTelemetry.openReinstallModal({
      agentId: currentActiveAgentId,
      nickname: agent ? (agent.nickname || agent.agent_id) : currentActiveAgentId
    });
  }

  function showInpageUninstallModal() {
    if (!currentActiveAgentId) return;
    const agent = agentsData.find(a => a.agent_id === currentActiveAgentId);
    window.CertainStatsTelemetry.openUninstallModal({
      agentId: currentActiveAgentId,
      nickname: agent ? (agent.nickname || agent.agent_id) : currentActiveAgentId
    });
  }

  function isLongNote(note) {
    return !!(note && (note.length > 40 || note.includes('\n')));
  }

  function syncInpageNotesUI(note) {
    const isLong = isLongNote(note);
    const headerReadEl = document.getElementById('inpage-header-notes-read');
    const headerEditEl = document.getElementById('inpage-header-notes-edit');
    const headerTextEl = document.getElementById('inpage-header-notes-text');
    const expandedSection = document.getElementById('inpage-agent-notes-section');
    const expandedReadEl = document.getElementById('inpage-expanded-notes-read');
    const expandedEditEl = document.getElementById('inpage-expanded-notes-edit');
    const expandedTextEl = document.getElementById('inpage-expanded-notes-text');
    const expandedTextarea = document.getElementById('inpage-expanded-notes-textarea');
    const headerInput = document.getElementById('inpage-header-notes-input');

    if (headerReadEl) headerReadEl.style.display = 'inline-flex';
    if (headerEditEl) headerEditEl.style.display = 'none';

    if (headerTextEl) {
      if (isLong) {
        headerTextEl.textContent = 'See below';
        headerTextEl.title = 'Click to view full notes below';
        headerTextEl.style.color = 'var(--text-secondary)';
      } else {
        headerTextEl.textContent = note || 'No notes';
        headerTextEl.title = note ? `${note} (Click to edit)` : 'No notes yet. Click to add private notes.';
        headerTextEl.style.color = 'var(--text-primary)';
      }
    }

    if (headerInput) headerInput.value = note || '';

    if (expandedSection) {
      expandedSection.style.display = isLong ? 'block' : 'none';
      if (expandedReadEl) expandedReadEl.style.display = 'block';
      if (expandedEditEl) expandedEditEl.style.display = 'none';
      if (expandedTextEl) expandedTextEl.textContent = note || '';
      if (expandedTextarea) expandedTextarea.value = note || '';
    }
  }

  function handleInpageHeaderNoteClick() {
    if (!currentActiveAgentId) return;
    const agent = agentsData.find(a => a.agent_id === currentActiveAgentId);
    const note = agent ? (agent.note || '') : '';
    if (isLongNote(note)) {
      const el = document.getElementById('inpage-agent-notes-section');
      if (el) el.scrollIntoView({ behavior: 'smooth' });
    } else {
      const readEl = document.getElementById('inpage-header-notes-read');
      const editEl = document.getElementById('inpage-header-notes-edit');
      const input = document.getElementById('inpage-header-notes-input');
      if (readEl) readEl.style.display = 'none';
      if (editEl) editEl.style.display = 'inline-flex';
      if (input) {
        input.value = note;
        input.focus();
      }
    }
  }

  function cancelInpageHeaderNoteEdit() {
    const readEl = document.getElementById('inpage-header-notes-read');
    const editEl = document.getElementById('inpage-header-notes-edit');
    if (editEl) editEl.style.display = 'none';
    if (readEl) readEl.style.display = 'inline-flex';
  }

  function saveInpageHeaderInlineNote() {
    const input = document.getElementById('inpage-header-notes-input');
    const noteText = input ? input.value.trim() : '';
    saveInpageNote(noteText);
  }

  function startInpageExpandedNotesEdit() {
    if (!currentActiveAgentId) return;
    const agent = agentsData.find(a => a.agent_id === currentActiveAgentId);
    const readEl = document.getElementById('inpage-expanded-notes-read');
    const editEl = document.getElementById('inpage-expanded-notes-edit');
    const textarea = document.getElementById('inpage-expanded-notes-textarea');
    if (readEl) readEl.style.display = 'none';
    if (editEl) editEl.style.display = 'flex';
    if (textarea) {
      textarea.value = agent ? (agent.note || '') : '';
      textarea.focus();
    }
  }

  function cancelInpageExpandedNotesEdit() {
    if (!currentActiveAgentId) return;
    const agent = agentsData.find(a => a.agent_id === currentActiveAgentId);
    const readEl = document.getElementById('inpage-expanded-notes-read');
    const editEl = document.getElementById('inpage-expanded-notes-edit');
    const textarea = document.getElementById('inpage-expanded-notes-textarea');
    if (textarea) textarea.value = agent ? (agent.note || '') : '';
    if (editEl) editEl.style.display = 'none';
    if (readEl) readEl.style.display = 'block';
  }

  function saveInpageExpandedNotes() {
    const textarea = document.getElementById('inpage-expanded-notes-textarea');
    const saveBtn = document.getElementById('inpage-btn-save-expanded-notes');
    const noteText = textarea ? textarea.value : '';
    saveInpageNote(noteText, saveBtn);
  }

  function saveInpageNote(noteText, btnEl) {
    if (!currentActiveAgentId) return;
    if (btnEl) {
      btnEl.disabled = true;
      btnEl.textContent = 'Saving...';
    }

    fetch(panelPath + '/api/agent', {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ agent_id: currentActiveAgentId, note: noteText })
    }).then(r => {
      if (btnEl) {
        btnEl.disabled = false;
        btnEl.textContent = 'Save Note';
      }
      if (r.ok) {
        const agent = agentsData.find(a => a.agent_id === currentActiveAgentId);
        if (agent) agent.note = noteText;
        syncInpageNotesUI(noteText);
        window.CertainStatsTelemetry.showToast('Note saved successfully', true);
      } else {
        window.CertainStatsTelemetry.showToast('Failed to save note', false);
      }
    }).catch(() => {
      if (btnEl) {
        btnEl.disabled = false;
        btnEl.textContent = 'Save Note';
      }
      window.CertainStatsTelemetry.showToast('Network error saving note', false);
    });
  }

  function handleInpageZoom(startMs, endMs) {
    inpageCustomRange = { start: startMs, end: endMs };
    if (inpageTimePicker) {
      inpageTimePicker.setCustomRange(startMs, endMs);
    }
  }

  function renderInpageLiveState(agentId) {
    if (!agentId) return;
    const agent = agentsData.find(a => a.agent_id === agentId) || {};
    const snap = window.CertainStatsTelemetry.normalizeSnapshot(liveMetricsStore[agentId]) || agent.latest_snap;

    // Render disks immediately from remembered state
    const inpageDisksGrid = document.getElementById('inpage-disks-grid');
    if (inpageDisksGrid) {
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
      const inpageCpu = document.getElementById('inpage-hw-cpu-bar');
      const inpageRam = document.getElementById('inpage-hw-ram-bar');
      const inpageDisk = document.getElementById('inpage-hw-disk-bar');
      const inpageSwap = document.getElementById('inpage-hw-swap-bar');
      const ramPct = (agent.ram_size && agent.ram_size > 0) ? (snap.ram_used_bytes / agent.ram_size) * 100 : 0;
      const diskPct = (agent.disk_size && agent.disk_size > 0 && snap.disk_used_bytes > 0) ? (snap.disk_used_bytes / agent.disk_size) * 100 : 0;
      const swapPct = (agent.swap_size && agent.swap_size > 0 && snap.ram_swap_used_bytes > 0) ? (snap.ram_swap_used_bytes / agent.swap_size) * 100 : 0;

      if (inpageCpu) inpageCpu.style.width = Math.min(snap.cpu_usage_percent, 100) + '%';
      if (inpageRam) inpageRam.style.width = Math.min(ramPct, 100) + '%';
      if (inpageDisk && diskPct > 0) inpageDisk.style.width = Math.min(diskPct, 100) + '%';
      if (inpageSwap && swapPct > 0) inpageSwap.style.width = Math.min(swapPct, 100) + '%';

      const inpageCpuUsr = document.getElementById('inpage-live-cpu-usr'); if (inpageCpuUsr) inpageCpuUsr.textContent = snap.cpu_usage_percent.toFixed(1) + '%';
      const inpageCpuIo = document.getElementById('inpage-live-cpu-io'); if (inpageCpuIo) inpageCpuIo.textContent = snap.cpu_iowait_percent.toFixed(1) + '%';
      const inpageCpuStl = document.getElementById('inpage-live-cpu-stl'); if (inpageCpuStl) inpageCpuStl.textContent = snap.cpu_steal_percent.toFixed(1) + '%';
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

  function loadDetailMetrics(agentId, hours, customRange) {
    const agent = agentsData.find(a => a.agent_id === agentId);
    const queryStr = customRange 
      ? 'start=' + customRange.start + '&end=' + customRange.end 
      : 'hours=' + hours;
    const qEnd = customRange ? customRange.end : Date.now();

    if (agent) {
      const isOnline = agent.is_online === true || (agent.last_seen && (Date.now() - new Date(agent.last_seen).getTime()) < 120000);
      const dotEl = document.getElementById('detail-active-dot');
      const badgeEl = document.getElementById('detail-active-badge');
      if (dotEl) dotEl.className = 'status-dot ' + (isOnline ? 'online' : 'offline');
      if (badgeEl) {
        badgeEl.className = 'badge ' + (isOnline ? 'badge-online' : 'badge-offline');
        badgeEl.textContent = isOnline ? 'Online' : 'Offline';
      }

      const elName = document.getElementById('detail-active-name'); if (elName) elName.textContent = agent.nickname || agent.agent_id;
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
      if (odoNet) odoNet.innerHTML = '<span>↓ ' + window.CertainStatsChart.formatBytes(agent.total_rx_bytes || 0) + '</span> / <span>↑ ' + window.CertainStatsChart.formatBytes(agent.total_tx_bytes || 0) + '</span>';

      let totalDiskRead = agent.total_disk_read_bytes || 0;
      let totalDiskWrite = agent.total_disk_write_bytes || 0;
      if (!totalDiskRead && !totalDiskWrite && agent.disks) {
        agent.disks.forEach(d => {
          totalDiskRead += (d.read_bytes || 0);
          totalDiskWrite += (d.write_bytes || 0);
        });
      }
      const odoDisk = document.getElementById('inpage-odo-disk');
      if (odoDisk) odoDisk.innerHTML = '<span>R: ' + window.CertainStatsChart.formatBytes(totalDiskRead) + '</span> / <span>W: ' + window.CertainStatsChart.formatBytes(totalDiskWrite) + '</span>';

      // Populate action form IDs and notes preview
      const elDeleteId = document.getElementById('inpage-delete-id'); if (elDeleteId) elDeleteId.value = agent.agent_id;
      syncInpageNotesUI(agent.note || '');
    }

    // 1. Fetch CPU (Usr, IO, Steal)
    Promise.all([
      fetch(panelPath + '/api/metrics?agent_id=' + agentId + '&metric=agent_cpu_usage&' + queryStr).then(r => r.json()).catch(() => ({ series: [] })),
      fetch(panelPath + '/api/metrics?agent_id=' + agentId + '&metric=agent_cpu_iowait&' + queryStr).then(r => r.json()).catch(() => ({ series: [] })),
      fetch(panelPath + '/api/metrics?agent_id=' + agentId + '&metric=agent_cpu_steal&' + queryStr).then(r => r.json()).catch(() => ({ series: [] }))
    ]).then(([resUsr, resIO, resStl]) => {
      const ptsUsr = (resUsr && resUsr.series && resUsr.series[0]) ? resUsr.series[0].data : [];
      const ptsIO = (resIO && resIO.series && resIO.series[0]) ? resIO.series[0].data : [];
      const ptsStl = (resStl && resStl.series && resStl.series[0]) ? resStl.series[0].data : [];

      const lastUsr = ptsUsr.length ? ptsUsr[ptsUsr.length - 1][1] : 0;
      const lastIO = ptsIO.length ? ptsIO[ptsIO.length - 1][1] : 0;
      const lastStl = ptsStl.length ? ptsStl[ptsStl.length - 1][1] : 0;
      const elUsr = document.getElementById('inpage-live-cpu-usr'); if (elUsr) elUsr.textContent = lastUsr.toFixed(1) + '%';
      const elIO = document.getElementById('inpage-live-cpu-io'); if (elIO) elIO.textContent = lastIO.toFixed(1) + '%';
      const elStl = document.getElementById('inpage-live-cpu-stl'); if (elStl) elStl.textContent = lastStl.toFixed(1) + '%';

      const seriesList = [
        { label: 'Usr', color: '#6366f1', fill: true, data: ptsUsr.map(p => ({ timestamp: p[0], value: p[1] })) },
        { label: 'IO', color: '#94a3b8', fill: false, data: ptsIO.map(p => ({ timestamp: p[0], value: p[1] })) },
        { label: 'Stl', color: '#ef4444', fill: false, data: ptsStl.map(p => ({ timestamp: p[0], value: p[1] })) }
      ];

      if (!inpageCpuChart) {
        inpageCpuChart = window.CertainStatsChart.renderMultiChart('inpage-chart-cpu', {
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
        inpageCpuChart.updateSeries(seriesList, hours, null, qEnd, customRange, 1);
      }
    });

    // 2. Fetch RAM & Swap
    Promise.all([
      fetch(panelPath + '/api/metrics?agent_id=' + agentId + '&metric=agent_ram_used&' + queryStr).then(r => r.json()).catch(() => ({ series: [] })),
      fetch(panelPath + '/api/metrics?agent_id=' + agentId + '&metric=agent_swap_used&' + queryStr).then(r => r.json()).catch(() => ({ series: [] }))
    ]).then(([resRam, resSwap]) => {
      const ptsRam = (resRam && resRam.series && resRam.series[0]) ? resRam.series[0].data : [];
      const ptsSwap = (resSwap && resSwap.series && resSwap.series[0]) ? resSwap.series[0].data : [];
      const totalRam = agent ? agent.ram_size : 0;

      const lastRam = ptsRam.length ? ptsRam[ptsRam.length - 1][1] : 0;
      const lastSwap = ptsSwap.length ? ptsSwap[ptsSwap.length - 1][1] : 0;
      const elRam = document.getElementById('inpage-live-ram-used'); if (elRam) elRam.textContent = window.CertainStatsChart.formatBytes(lastRam);
      const elSwap = document.getElementById('inpage-live-ram-swap'); if (elSwap) elSwap.textContent = window.CertainStatsChart.formatBytes(lastSwap);

      const seriesList = [
        { label: 'RAM', color: '#14b8a6', fill: true, data: ptsRam.map(p => ({ timestamp: p[0], value: p[1] })) },
        { label: 'Swap', color: '#94a3b8', fill: false, data: ptsSwap.map(p => ({ timestamp: p[0], value: p[1] })) }
      ];

      if (!inpageRamChart) {
        inpageRamChart = window.CertainStatsChart.renderMultiChart('inpage-chart-ram', {
          seriesList: seriesList,
          formatter: window.CertainStatsChart.formatBytes,
          yMax: totalRam > 0 ? totalRam : null,
          hours: hours,
          customRange: customRange,
          queryEndTime: qEnd,
          onZoom: handleInpageZoom
        });
      } else {
        inpageRamChart.updateSeries(seriesList, hours, totalRam > 0 ? totalRam : null, qEnd, customRange);
      }
    });

    // 3. Fetch Network (RX / TX)
    Promise.all([
      fetch(panelPath + '/api/metrics?agent_id=' + agentId + '&metric=agent_rx_bytes&' + queryStr).then(r => r.json()).catch(() => ({ series: [] })),
      fetch(panelPath + '/api/metrics?agent_id=' + agentId + '&metric=agent_tx_bytes&' + queryStr).then(r => r.json()).catch(() => ({ series: [] }))
    ]).then(([resRx, resTx]) => {
      const ptsRx = (resRx && resRx.series && resRx.series[0]) ? resRx.series[0].data : [];
      const ptsTx = (resTx && resTx.series && resTx.series[0]) ? resTx.series[0].data : [];

      const rateRx = window.CertainStatsChart.convertDeltaToRate(ptsRx);
      const rateTx = window.CertainStatsChart.convertDeltaToRate(ptsTx);
      const lastRx = rateRx.length ? rateRx[rateRx.length - 1][1] : 0;
      const lastTx = rateTx.length ? rateTx[rateTx.length - 1][1] : 0;
      const elRx = document.getElementById('inpage-live-net-rx'); if (elRx) elRx.textContent = '↓ ' + window.CertainStatsChart.formatBps(lastRx);
      const elTx = document.getElementById('inpage-live-net-tx'); if (elTx) elTx.textContent = '↑ ' + window.CertainStatsChart.formatBps(lastTx);

      const seriesList = [
        { label: 'RX', color: '#1e40af', fill: false, data: rateRx.map(p => ({ timestamp: p[0], value: p[1] })) },
        { label: 'TX', color: '#7e22ce', fill: false, data: rateTx.map(p => ({ timestamp: p[0], value: p[1] })) }
      ];

      if (!inpageNetChart) {
        inpageNetChart = window.CertainStatsChart.renderMultiChart('inpage-chart-net', {
          seriesList: seriesList,
          formatter: window.CertainStatsChart.formatBps,
          hours: hours,
          customRange: customRange,
          queryEndTime: qEnd,
          onZoom: handleInpageZoom
        });
      } else {
        inpageNetChart.updateSeries(seriesList, hours, undefined, qEnd, customRange);
      }
    });

    // 4. Fetch Disk Usage & Disk I/O Rates (Separated Per Disk Partition)
    Promise.all([
      fetch(panelPath + '/api/metrics?agent_id=' + agentId + '&metric=agent_disk_used&' + queryStr).then(r => r.json()).catch(() => ({ series: [] })),
      fetch(panelPath + '/api/metrics?agent_id=' + agentId + '&metric=agent_disk_read_bytes&' + queryStr).then(r => r.json()).catch(() => ({ series: [] })),
      fetch(panelPath + '/api/metrics?agent_id=' + agentId + '&metric=agent_disk_write_bytes&' + queryStr).then(r => r.json()).catch(() => ({ series: [] }))
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
      const container = document.getElementById('inpage-disk-charts-grid');
      if (!container) return;

      const expectedIds = paths.map(p => 'inpage-disk-card-usage-' + safeId(p)).join(',');
      const currentIds = Array.from(container.children).map(c => c.id).filter(id => id.startsWith('inpage-disk-card-usage-')).join(',');

      if (expectedIds !== currentIds) {
        Object.values(inpageDiskCharts).forEach(dc => {
          if (dc.usageChart && dc.usageChart.destroy) dc.usageChart.destroy();
          if (dc.ioChart && dc.ioChart.destroy) dc.ioChart.destroy();
        });
        inpageDiskCharts = {};

        let html = '';
        paths.forEach(p => {
          const safe = safeId(p);
          html += `
            <div class="card" id="inpage-disk-card-usage-${safe}">
              <div class="chart-header-row">
                <h3 class="chart-header-title">Disk Usage (${p})</h3>
                <div class="chart-legend-pills">
                  <span class="chart-legend-item"><span class="chart-legend-dot" style="background-color: #8b5cf6;"></span>Used: <span class="chart-legend-val" id="inpage-live-disk-used-${safe}">0 B</span></span>
                </div>
              </div>
              <div class="chart-container" style="height: 220px;">
                <canvas id="inpage-chart-disk-${safe}" class="chart-canvas"></canvas>
              </div>
            </div>
            <div class="card" id="inpage-disk-card-io-${safe}">
              <div class="chart-header-row">
                <h3 class="chart-header-title">Disk I/O Rate (${p})</h3>
                <div class="chart-legend-pills">
                  <span class="chart-legend-item"><span class="chart-legend-dot" style="background-color: #fb923c;"></span>Read: <span class="chart-legend-val" id="inpage-live-disk-read-${safe}">0 B/s</span></span>
                  <span class="chart-legend-item"><span class="chart-legend-dot" style="background-color: #ef4444;"></span>Write: <span class="chart-legend-val" id="inpage-live-disk-write-${safe}">0 B/s</span></span>
                </div>
              </div>
              <div class="chart-container" style="height: 220px;">
                <canvas id="inpage-chart-disk-io-${safe}" class="chart-canvas"></canvas>
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

        const elUsed = document.getElementById('inpage-live-disk-used-' + safe); if (elUsed) elUsed.textContent = window.CertainStatsChart.formatBytes(lastUsed);
        const elR = document.getElementById('inpage-live-disk-read-' + safe); if (elR) elR.textContent = window.CertainStatsChart.formatBps(lastR);
        const elW = document.getElementById('inpage-live-disk-write-' + safe); if (elW) elW.textContent = window.CertainStatsChart.formatBps(lastW);

        const usageSeries = [{
          label: 'Used',
          color: '#8b5cf6',
          fill: true,
          data: ptsUsed.map(pt => ({ timestamp: pt[0], value: pt[1] }))
        }];

        const ioSeries = [
          { label: 'Read', color: '#fb923c', fill: false, data: rateRead.map(pt => ({ timestamp: pt[0], value: pt[1] })) },
          { label: 'Write', color: '#ef4444', fill: false, data: rateWrite.map(pt => ({ timestamp: pt[0], value: pt[1] })) }
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

        if (!inpageDiskCharts[p]) {
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
          inpageDiskCharts[p] = { usageChart, ioChart };
        } else {
          inpageDiskCharts[p].usageChart.updateSeries(usageSeries, hours, diskTotal > 0 ? diskTotal : null, qEnd, customRange);
          inpageDiskCharts[p].ioChart.updateSeries(ioSeries, hours, undefined, qEnd, customRange);
        }
      });
    });
  }

  function applyTelemetryUpdates(snaps) {
    if (snaps && typeof snaps === 'object') {
      Object.assign(liveMetricsStore, snaps);
    }
    window.CertainStatsTelemetry.renderClusterStats('admin-', agentsData, liveMetricsStore);

    for (const id in snaps) {
      const snap = window.CertainStatsTelemetry.normalizeSnapshot(snaps[id]);
      if (!snap) continue;

      const agent = agentsData.find(a => a.agent_id === id) || {};
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
      const segUsr = document.getElementById('seg-cpu-usr-' + id);
      const segIo = document.getElementById('seg-cpu-io-' + id);
      const segStl = document.getElementById('seg-cpu-stl-' + id);
      const valCpu = document.getElementById('val-cpu-' + id);
      const tdSegCpu = document.getElementById('td-seg-cpu-' + id);
      const tdCpu = document.getElementById('td-cpu-' + id);

      if (valCpu) valCpu.textContent = snap.cpu_usage_percent.toFixed(1) + '%';
      if (tdCpu) tdCpu.textContent = snap.cpu_usage_percent.toFixed(1) + '%';
      if (segUsr) segUsr.style.width = Math.min(snap.cpu_usage_percent, 100) + '%';
      if (segIo) segIo.style.width = Math.min(snap.cpu_iowait_percent, 100) + '%';
      if (segStl) segStl.style.width = Math.min(snap.cpu_steal_percent, 100) + '%';
      if (tdSegCpu) tdSegCpu.style.width = Math.min(snap.cpu_usage_percent, 100) + '%';

      // Update RAM stacked bar & values
      const segRam = document.getElementById('seg-ram-used-' + id);
      const segSwap = document.getElementById('seg-ram-swap-' + id);
      const valRam = document.getElementById('val-ram-' + id);
      const tdSegRam = document.getElementById('td-seg-ram-' + id);
      const tdRam = document.getElementById('td-ram-' + id);

      const ramPct = (agent.ram_size && agent.ram_size > 0) ? (snap.ram_used_bytes / agent.ram_size) * 100 : 0;
      const swapPct = (agent.swap_size && agent.swap_size > 0) ? (snap.ram_swap_used_bytes / agent.swap_size) * 100 : 0;

      if (valRam) valRam.textContent = window.CertainStatsChart.formatBytes(snap.ram_used_bytes) + (agent.ram_size ? ' / ' + window.CertainStatsChart.formatBytes(agent.ram_size) : '');
      if (tdRam) tdRam.textContent = window.CertainStatsChart.formatBytes(snap.ram_used_bytes);
      if (segRam) segRam.style.width = Math.min(ramPct, 100) + '%';
      if (segSwap) segSwap.style.width = Math.min(swapPct, 100) + '%';
      if (tdSegRam) tdSegRam.style.width = Math.min(ramPct, 100) + '%';

      // Update Disk bars (Dynamic Multi-Disk)
      const diskBarsGroup = document.getElementById('disk-bars-group-' + id);
      let diskUsed = snap.disk_used_bytes || 0;
      let diskTotal = agent.disk_size || snap.disk_total_bytes || 0;
      if (snap.disks && snap.disks.length > 0) {
        let sumUsed = 0, sumTotal = 0;
        snap.disks.forEach(d => {
          sumUsed += d.used_bytes || 0;
          sumTotal += d.total_bytes || 0;
        });
        if (sumUsed > 0) diskUsed = sumUsed;
        if (sumTotal > 0 && (!diskTotal || diskTotal < sumTotal)) diskTotal = sumTotal;
      }
      const diskPct = (diskTotal > 0 && diskUsed > 0) ? (diskUsed / diskTotal) * 100 : 0;

      if (diskBarsGroup) {
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
          const segDisk = document.getElementById('seg-disk-' + id);
          const valDisk = document.getElementById('val-disk-' + id);
          if (valDisk && diskUsed > 0) {
            valDisk.textContent = window.CertainStatsChart.formatBytes(diskUsed) + (diskTotal ? ' / ' + window.CertainStatsChart.formatBytes(diskTotal) : '');
          }
          if (segDisk && diskPct > 0) {
            segDisk.style.width = Math.min(diskPct, 100) + '%';
          }
        }
      }

      const trackDisk = document.getElementById('track-disk-' + id);
      if (trackDisk && snap.disks && snap.disks.length > 1) {
        const rows = snap.disks.map(d => ({
          label: d.path ? `Disk (${d.path})` : 'Disk',
          val: window.CertainStatsChart.formatBytes(d.used_bytes) + (d.total_bytes ? ' / ' + window.CertainStatsChart.formatBytes(d.total_bytes) : ''),
          color: '#8b5cf6'
        }));
        trackDisk.setAttribute('data-tooltip-rows', JSON.stringify(rows));
        trackDisk.setAttribute('data-tooltip-header', 'Storage Partitions');
      }

      // Update Network bar
      const segRx = document.getElementById('seg-net-rx-' + id);
      const segTx = document.getElementById('seg-net-tx-' + id);
      const tdSegNetRx = document.getElementById('td-seg-net-rx-' + id);
      const tdSegNetTx = document.getElementById('td-seg-net-tx-' + id);
      const valNet = document.getElementById('val-net-' + id);
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

      // Update Tooltip attributes
      const trackCpu = document.getElementById('track-cpu-' + id);
      if (trackCpu) {
        trackCpu.setAttribute('data-tooltip-rows', JSON.stringify([
          { label: 'Used', val: snap.cpu_usage_percent.toFixed(1) + '%', color: '#3b82f6' },
          { label: 'IO Wait', val: snap.cpu_iowait_percent.toFixed(1) + '%', color: '#fb923c' },
          { label: 'Steal', val: snap.cpu_steal_percent.toFixed(1) + '%', color: '#ef4444' }
        ]));
      }
      const trackRam = document.getElementById('track-ram-' + id);
      if (trackRam) {
        trackRam.setAttribute('data-tooltip-rows', JSON.stringify([
          { label: 'RAM Used', val: window.CertainStatsChart.formatBytes(snap.ram_used_bytes), color: '#14b8a6' },
          { label: 'Swap Used', val: window.CertainStatsChart.formatBytes(snap.ram_swap_used_bytes), color: '#4b5563' }
        ]));
      }
      const trackNet = document.getElementById('track-net-' + id);
      if (trackNet) {
        trackNet.setAttribute('data-tooltip-rows', JSON.stringify([
          { label: 'Download (RX)', val: window.CertainStatsChart.formatBps(snap.rx_bps), color: '#1e40af' },
          { label: 'Upload (TX)', val: window.CertainStatsChart.formatBps(snap.tx_bps), color: '#7e22ce' }
        ]));
      }
      const tdTrackNet = document.getElementById('td-track-net-' + id);
      if (tdTrackNet) {
        tdTrackNet.setAttribute('data-tooltip-rows', JSON.stringify([
          { label: 'Download (RX)', val: window.CertainStatsChart.formatBps(snap.rx_bps), color: '#1e40af' },
          { label: 'Upload (TX)', val: window.CertainStatsChart.formatBps(snap.tx_bps), color: '#7e22ce' }
        ]));
      }

      if (snap.uptime != null) {
        agent.uptime = snap.uptime;
        const uptimeEl = document.getElementById('uptime-' + id);
        const tdUptimeEl = document.getElementById('td-uptime-' + id);
        const formatted = window.CertainStatsTelemetry.formatUptime(snap.uptime);
        if (uptimeEl) uptimeEl.innerHTML = 'Uptime: <strong class="mono" style="color: var(--text-secondary);">' + formatted + '</strong>';
        if (tdUptimeEl) tdUptimeEl.textContent = formatted;
      }

      // Update active in-page detail HW progress bars & live charts if this agent is viewed
      if (currentActiveAgentId === id) {
        const inpageCpu = document.getElementById('inpage-hw-cpu-bar');
        const inpageRam = document.getElementById('inpage-hw-ram-bar');
        const inpageDisk = document.getElementById('inpage-hw-disk-bar');
        const inpageSwap = document.getElementById('inpage-hw-swap-bar');
        if (inpageCpu) inpageCpu.style.width = Math.min(snap.cpu_usage_percent, 100) + '%';
        if (inpageRam) inpageRam.style.width = Math.min(ramPct, 100) + '%';
        if (inpageDisk && diskPct > 0) inpageDisk.style.width = Math.min(diskPct, 100) + '%';
        if (inpageSwap && swapPct > 0) inpageSwap.style.width = Math.min(swapPct, 100) + '%';

        // Update in-page Disks Section and live specs
        renderInpageLiveState(id);

        // Update in-page live chart legend pill values
        const inpageCpuUsr = document.getElementById('inpage-live-cpu-usr'); if (inpageCpuUsr) inpageCpuUsr.textContent = snap.cpu_usage_percent.toFixed(1) + '%';
        const inpageCpuIo = document.getElementById('inpage-live-cpu-io'); if (inpageCpuIo) inpageCpuIo.textContent = snap.cpu_iowait_percent.toFixed(1) + '%';
        const inpageCpuStl = document.getElementById('inpage-live-cpu-stl'); if (inpageCpuStl) inpageCpuStl.textContent = snap.cpu_steal_percent.toFixed(1) + '%';
        const inpageRamUsed = document.getElementById('inpage-live-ram-used'); if (inpageRamUsed) inpageRamUsed.textContent = window.CertainStatsChart.formatBytes(snap.ram_used_bytes);
        const inpageRamSwap = document.getElementById('inpage-live-ram-swap'); if (inpageRamSwap) inpageRamSwap.textContent = window.CertainStatsChart.formatBytes(snap.ram_swap_used_bytes);
        const inpageNetRx = document.getElementById('inpage-live-net-rx'); if (inpageNetRx) inpageNetRx.textContent = '↓ ' + window.CertainStatsChart.formatBps(snap.rx_bps);
        const inpageNetTx = document.getElementById('inpage-live-net-tx'); if (inpageNetTx) inpageNetTx.textContent = '↑ ' + window.CertainStatsChart.formatBps(snap.tx_bps);
        const inpageDiskUsed = document.getElementById('inpage-live-disk-used'); if (inpageDiskUsed) inpageDiskUsed.textContent = window.CertainStatsChart.formatBytes(snap.disk_used_bytes);
        const inpageDiskRead = document.getElementById('inpage-live-disk-read'); if (inpageDiskRead) inpageDiskRead.textContent = window.CertainStatsChart.formatBps(snap.disk_read_bps);
        const inpageDiskWrite = document.getElementById('inpage-live-disk-write'); if (inpageDiskWrite) inpageDiskWrite.textContent = window.CertainStatsChart.formatBps(snap.disk_write_bps);

        if (snap.uptime != null) {
          const inpageUptimeEl = document.getElementById('inpage-spec-uptime');
          if (inpageUptimeEl) inpageUptimeEl.textContent = window.CertainStatsTelemetry.formatUptime(snap.uptime);
        }

        if (!inpageCustomRange) {
          const ts = Date.now();
          if (inpageCpuChart) inpageCpuChart.updateLivePoint(ts, { Usr: snap.cpu_usage_percent, IO: snap.cpu_iowait_percent, Stl: snap.cpu_steal_percent });
          if (inpageRamChart) inpageRamChart.updateLivePoint(ts, { RAM: snap.ram_used_bytes, Swap: snap.ram_swap_used_bytes });
          if (inpageNetChart) inpageNetChart.updateLivePoint(ts, { RX: snap.rx_bps, TX: snap.tx_bps });

          if (snap.disks && snap.disks.length > 0) {
            snap.disks.forEach(d => {
              const path = d.path || '/';
              const safe = safeId(path);
              const elUsed = document.getElementById('inpage-live-disk-used-' + safe); if (elUsed) elUsed.textContent = window.CertainStatsChart.formatBytes(d.used_bytes);
              const elR = document.getElementById('inpage-live-disk-read-' + safe); if (elR) elR.textContent = window.CertainStatsChart.formatBps(d.read_bytes || 0);
              const elW = document.getElementById('inpage-live-disk-write-' + safe); if (elW) elW.textContent = window.CertainStatsChart.formatBps(d.write_bytes || 0);

              if (inpageDiskCharts[path]) {
                if (inpageDiskCharts[path].usageChart) inpageDiskCharts[path].usageChart.updateLivePoint(ts, { Used: d.used_bytes });
                if (inpageDiskCharts[path].ioChart) inpageDiskCharts[path].ioChart.updateLivePoint(ts, { Read: d.read_bytes || 0, Write: d.write_bytes || 0 });
              }
            });
          } else {
            const elUsed = document.getElementById('inpage-live-disk-used-root'); if (elUsed) elUsed.textContent = window.CertainStatsChart.formatBytes(snap.disk_used_bytes);
            const elR = document.getElementById('inpage-live-disk-read-root'); if (elR) elR.textContent = window.CertainStatsChart.formatBps(snap.disk_read_bps);
            const elW = document.getElementById('inpage-live-disk-write-root'); if (elW) elW.textContent = window.CertainStatsChart.formatBps(snap.disk_write_bps);

            if (inpageDiskCharts['/']) {
              if (inpageDiskCharts['/'].usageChart) inpageDiskCharts['/'].usageChart.updateLivePoint(ts, { Used: snap.disk_used_bytes });
              if (inpageDiskCharts['/'].ioChart) inpageDiskCharts['/'].ioChart.updateLivePoint(ts, { Read: snap.disk_read_bps, Write: snap.disk_write_bps });
            }
          }
        }
      }
    }
  }

  function syncAdminAgentsMetadata(force) {
    const now = Date.now();
    if (!force && (now - lastAdminSyncTime < ADMIN_METADATA_SYNC_INTERVAL_MS)) return;
    lastAdminSyncTime = now;

    fetch(panelPath + '/api/agents')
      .then(r => r.ok ? r.json() : null)
      .then(freshAgents => {
        if (!freshAgents || !Array.isArray(freshAgents)) return;

        freshAgents.forEach(fa => {
          let existing = agentsData.find(a => a.agent_id === fa.agent_id);
          if (!existing) {
            existing = { agent_id: fa.agent_id };
            agentsData.push(existing);
          }
          existing.nickname = fa.nickname || existing.nickname;
          existing.cpu_model = fa.cpu_model || existing.cpu_model;
          existing.linux_version = fa.linux_version || existing.linux_version;
          existing.cpu_cores = fa.cpu_cores ?? existing.cpu_cores;
          existing.ram_size = fa.ram_size ?? existing.ram_size;
          existing.disk_size = fa.disk_size ?? existing.disk_size;
          if (fa.net) {
            if (fa.net.total_rx_bytes !== undefined) existing.total_rx_bytes = fa.net.total_rx_bytes;
            if (fa.net.total_tx_bytes !== undefined) existing.total_tx_bytes = fa.net.total_tx_bytes;
          } else {
            if (fa.total_rx_bytes !== undefined) existing.total_rx_bytes = fa.total_rx_bytes;
            if (fa.total_tx_bytes !== undefined) existing.total_tx_bytes = fa.total_tx_bytes;
          }

          if (fa.disks && Array.isArray(fa.disks)) {
            existing.disks = fa.disks.map(d => ({
              path: d.path,
              total_bytes: d.total_bytes ?? d.TotalBytes ?? 0,
              read_bytes: d.read_bytes ?? d.ReadBytes ?? 0,
              write_bytes: d.write_bytes ?? d.WriteBytes ?? 0
            }));
            let dr = 0, dw = 0;
            existing.disks.forEach(d => { dr += d.read_bytes || 0; dw += d.write_bytes || 0; });
            existing.total_disk_read_bytes = dr;
            existing.total_disk_write_bytes = dw;
          } else {
            if (fa.total_disk_read_bytes !== undefined) existing.total_disk_read_bytes = fa.total_disk_read_bytes;
            if (fa.total_disk_write_bytes !== undefined) existing.total_disk_write_bytes = fa.total_disk_write_bytes;
          }

          if (fa.uptime != null) existing.uptime = fa.uptime;
          if (fa.is_online !== undefined) {
            existing.is_online = fa.is_online;
            const isOnline = !!fa.is_online;
            const cardDot = document.getElementById('dot-' + fa.agent_id);
            if (cardDot) cardDot.className = 'status-dot ' + (isOnline ? 'online' : 'offline');
            const tdDot = document.getElementById('td-dot-' + fa.agent_id);
            if (tdDot) tdDot.className = 'status-dot ' + (isOnline ? 'online' : 'offline');
            const cardBadge = document.getElementById('badge-' + fa.agent_id);
            if (cardBadge) {
              cardBadge.className = 'badge ' + (isOnline ? 'badge-online' : 'badge-offline');
              cardBadge.textContent = isOnline ? 'Online' : 'Offline';
            }
            if (currentActiveAgentId === fa.agent_id) {
              const activeDot = document.getElementById('detail-active-dot');
              if (activeDot) activeDot.className = 'status-dot ' + (isOnline ? 'online' : 'offline');
              const activeBadge = document.getElementById('detail-active-badge');
              if (activeBadge) {
                activeBadge.className = 'badge ' + (isOnline ? 'badge-online' : 'badge-offline');
                activeBadge.textContent = isOnline ? 'Online' : 'Offline';
              }
            }
          }

          // Update in-page detail if currently viewed
          if (currentActiveAgentId === fa.agent_id) {
            const elCpu = document.getElementById('inpage-hw-cpu');
            if (elCpu) elCpu.textContent = existing.cpu_cores || '-';
            const elRam = document.getElementById('inpage-hw-ram');
            if (elRam) elRam.textContent = window.CertainStatsChart.formatBytes(existing.ram_size);
            const elDisk = document.getElementById('inpage-hw-disk');
            if (elDisk) elDisk.textContent = window.CertainStatsChart.formatBytes(existing.disk_size);
            const elSwap = document.getElementById('inpage-hw-swap');
            if (elSwap) elSwap.textContent = window.CertainStatsChart.formatBytes(existing.swap_size);
            const elKernel = document.getElementById('inpage-spec-kernel');
            if (elKernel) elKernel.textContent = existing.linux_version || 'Linux';
            const elArch = document.getElementById('inpage-spec-cpu');
            if (elArch) elArch.textContent = existing.cpu_model || 'Generic CPU';

            const odoNet = document.getElementById('inpage-odo-net');
            if (odoNet) odoNet.innerHTML = '<span>↓ ' + window.CertainStatsChart.formatBytes(existing.total_rx_bytes || 0) + '</span> / <span>↑ ' + window.CertainStatsChart.formatBytes(existing.total_tx_bytes || 0) + '</span>';
            const odoDisk = document.getElementById('inpage-odo-disk');
            if (odoDisk) odoDisk.innerHTML = '<span>R: ' + window.CertainStatsChart.formatBytes(existing.total_disk_read_bytes || 0) + '</span> / <span>W: ' + window.CertainStatsChart.formatBytes(existing.total_disk_write_bytes || 0) + '</span>';
          }
        });

        // Recalculate and re-render cluster overview totals
        window.CertainStatsTelemetry.renderClusterStats('admin-', agentsData, liveMetricsStore);
      })
      .catch(() => {});
  }

  function init(options) {
    options = options || {};
    panelPath = options.panelPath || window.CertainStatsTelemetry.getPanelPath();
    agentsData = options.agents || [];

    // Initial calculation of cluster stats from static agent data
    window.CertainStatsTelemetry.renderClusterStats('admin-', agentsData, liveMetricsStore);

    // 1. Initiate WebSocket handshake immediately without waiting for DOMContentLoaded (0ms delay)
    window.CertainStatsTelemetry.initWebSocket(panelPath + '/api/ws', applyTelemetryUpdates);

    // 2. Setup DOM-dependent UI on DOM Ready
    window.CertainStatsTelemetry.onReady(function() {
      let viewMode = 'grid';
      let density = 'detailed';
      try {
        viewMode = localStorage.getItem('certainstats_view_mode') || 'grid';
        density = localStorage.getItem('certainstats_density') || 'detailed';
      } catch (e) {}
      setAgentViewMode(viewMode);
      setAgentDensity(density);

      // In-Place SPA Router (BASE_PATH/{AGENT_ID})
      window.CertainStatsTelemetry.initRouter({
        basePath: panelPath,
        onNavigate: function(agentId) {
          const overviewView = document.getElementById('agents-overview-view');
          const detailView = document.getElementById('agents-detail-view');

          if (!agentId) {
            if (detailView) detailView.style.display = 'none';
            if (overviewView) overviewView.style.display = 'block';
            currentActiveAgentId = null;
            window.scrollTo({ top: savedScrollY, behavior: 'instant' });
            return;
          }

          savedScrollY = window.scrollY;
          currentActiveAgentId = agentId;
          if (overviewView) overviewView.style.display = 'none';
          if (detailView) {
            detailView.style.display = 'block';
            window.scrollTo({ top: 0, behavior: 'instant' });

            renderInpageLiveState(agentId);
            
            let activeHours = 6;
            try { activeHours = parseInt(localStorage.getItem('certainstats_active_hours') || '6', 10); } catch (e) {}
            inpageTimePicker = window.CertainStatsTelemetry.initCustomTimePicker('inpage-detail-time-picker-container', {
              activeHours: activeHours,
              onApply: function(opts) {
                inpageCustomRange = opts.customRange;
                loadDetailMetrics(agentId, opts.hours, opts.customRange);
              }
            });

            loadDetailMetrics(agentId, activeHours, inpageCustomRange);
          }
        }
      });

      // Inpage Header Notes Enter/Escape Key Handler
      const inpageHeaderInput = document.getElementById('inpage-header-notes-input');
      if (inpageHeaderInput) {
        inpageHeaderInput.addEventListener('keydown', function(e) {
          if (e.key === 'Enter') saveInpageHeaderInlineNote();
          if (e.key === 'Escape') cancelInpageHeaderNoteEdit();
        });
      }
    });

    setInterval(syncAdminAgentsMetadata, ADMIN_METADATA_SYNC_INTERVAL_MS);
  }

  // Export module namespace
  window.CertainStatsAdminAgents = {
    init: init,
    handleAgentItemClick: handleAgentItemClick,
    navigateToAgent: navigateToAgent,
    closeAgentDetail: closeAgentDetail,
    setAgentViewMode: setAgentViewMode,
    setAgentDensity: setAgentDensity,
    filterAgentsList: filterAgentsList,
    showInpageReinstallModal: showInpageReinstallModal,
    showInpageUninstallModal: showInpageUninstallModal,
    syncInpageNotesUI: syncInpageNotesUI,
    handleInpageHeaderNoteClick: handleInpageHeaderNoteClick,
    cancelInpageHeaderNoteEdit: cancelInpageHeaderNoteEdit,
    saveInpageHeaderInlineNote: saveInpageHeaderInlineNote,
    startInpageExpandedNotesEdit: startInpageExpandedNotesEdit,
    cancelInpageExpandedNotesEdit: cancelInpageExpandedNotesEdit,
    saveInpageExpandedNotes: saveInpageExpandedNotes,
    syncAdminAgentsMetadata: syncAdminAgentsMetadata
  };

  // Backwards compatibility globals for inline onclick handlers
  window.handleAgentItemClick = handleAgentItemClick;
  window.navigateToAgent = navigateToAgent;
  window.closeAgentDetail = closeAgentDetail;
  window.setAgentViewMode = setAgentViewMode;
  window.setAgentDensity = setAgentDensity;
  window.filterAgentsList = filterAgentsList;
  window.showInpageReinstallModal = showInpageReinstallModal;
  window.showInpageUninstallModal = showInpageUninstallModal;
  window.handleInpageHeaderNoteClick = handleInpageHeaderNoteClick;
  window.cancelInpageHeaderNoteEdit = cancelInpageHeaderNoteEdit;
  window.saveInpageHeaderInlineNote = saveInpageHeaderInlineNote;
  window.startInpageExpandedNotesEdit = startInpageExpandedNotesEdit;
  window.cancelInpageExpandedNotesEdit = cancelInpageExpandedNotesEdit;
  window.saveInpageExpandedNotes = saveInpageExpandedNotes;
})();
