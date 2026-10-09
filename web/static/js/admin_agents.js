(function () {
  'use strict';

  let panelPath = '';
  let agentsData = [];
  let currentActiveAgentId = null;
  let nodeHistory = null;
  let inpageTimePicker = null;
  let inpageCustomRange = null;
  let inpageCpuChart = null, inpageRamChart = null, inpageNetChart = null;
  let inpageDiskCharts = {};
  let liveMetricsStore = {};
  let savedScrollY = 0;

  const ADMIN_METADATA_SYNC_INTERVAL_MS = 300000;
  let lastAdminSyncTime = Date.now();

  function safeId(p) {
    return window.CertainStatsDiskDOM.pathId(p);
  }

  function handleAgentItemClick(event, agentId) {
    if (event.ctrlKey || event.metaKey || event.shiftKey || event.altKey || event.button !== 0) {
      return; // Allow standard new tab opening
    }
    event.preventDefault();
    navigateToAgent(agentId);
  }

  function navigateToAgent(agentId) {
    resetInpageEditStates();
    const targetUrl = (panelPath ? panelPath : '') + '/' + agentId;
    if (window.location.pathname !== targetUrl) {
      history.pushState({ agentId: agentId }, '', targetUrl);
    }
    window.dispatchEvent(new PopStateEvent('popstate'));
  }

  function closeAgentDetail() {
    resetInpageEditStates();
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

    const isList = mode === 'list';
    if (gridContainer) gridContainer.hidden = isList;
    if (listContainer) listContainer.hidden = !isList;
    if (btnGrid) { btnGrid.classList.toggle('active', !isList); btnGrid.setAttribute('aria-pressed', String(!isList)); }
    if (btnList) { btnList.classList.toggle('active', isList); btnList.setAttribute('aria-pressed', String(isList)); }
    try { localStorage.setItem('certainstats_view_mode', mode); } catch (e) {}
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
    const matches=new Set(Array.from(document.querySelectorAll('.agent-card-item, .agent-row-item')).filter(node=>node.style.display!=='none').map(node=>node.getAttribute('data-agent-id')));
    let count=document.getElementById('agent-search-input-count');if (!count) {count=document.createElement('p');count.id='agent-search-input-count';count.className='muted';count.setAttribute('role','status');document.getElementById('agent-search-input').parentElement.after(count);}
    count.textContent=matches.size ? matches.size+' matching servers' : 'No matching servers.';

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

    if (headerReadEl) headerReadEl.hidden = false;
    if (headerEditEl) headerEditEl.hidden = true;

    if (headerTextEl) {
      if (isLong) {
        headerTextEl.textContent = 'Notes below';
        headerTextEl.title = 'Jump to notes';
      } else {
        headerTextEl.textContent = note || 'Add a note';
        headerTextEl.title = note ? 'Edit note' : 'Add a private note';
      }
      headerTextEl.classList.toggle('is-empty', !note);
    }

    if (headerInput) headerInput.value = note || '';

    if (expandedSection) {
      expandedSection.hidden = !isLong;
      if (expandedReadEl) expandedReadEl.hidden = false;
      if (expandedEditEl) expandedEditEl.hidden = true;
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
      if (readEl) readEl.hidden = true;
      if (editEl) editEl.hidden = false;
      if (input) {
        input.value = note;
        input.focus();
      }
    }
  }

  function cancelInpageHeaderNoteEdit() {
    const readEl = document.getElementById('inpage-header-notes-read');
    const editEl = document.getElementById('inpage-header-notes-edit');
    if (editEl) editEl.hidden = true;
    if (readEl) readEl.hidden = false;
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
    if (readEl) readEl.hidden = true;
    if (editEl) editEl.hidden = false;
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
    if (editEl) editEl.hidden = true;
    if (readEl) readEl.hidden = false;
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

  function startInpageAgentNameEdit() {
    if (!currentActiveAgentId) return;
    const agent = agentsData.find(a => a.agent_id === currentActiveAgentId);
    const readEl = document.getElementById('detail-name-read-container');
    const editEl = document.getElementById('detail-name-edit-container');
    const input = document.getElementById('detail-name-input');
    if (readEl) readEl.hidden = true;
    if (editEl) editEl.hidden = false;
    if (input) {
      input.value = agent ? (agent.nickname || agent.agent_id) : '';
      input.focus();
      input.select();
    }
  }

  function cancelInpageAgentNameEdit() {
    const readEl = document.getElementById('detail-name-read-container');
    const editEl = document.getElementById('detail-name-edit-container');
    if (editEl) editEl.hidden = true;
    if (readEl) readEl.hidden = false;
  }

  function saveInpageAgentName() {
    if (!currentActiveAgentId) return;
    const input = document.getElementById('detail-name-input');
    const saveBtn = document.getElementById('detail-name-save-btn');
    const newNickname = input ? input.value.trim() : '';

    if (!newNickname) {
      window.CertainStatsTelemetry.showToast('Nickname cannot be empty', false);
      if (input) input.focus();
      return;
    }
    if (newNickname.length > 64) {
      window.CertainStatsTelemetry.showToast('Nickname cannot exceed 64 characters', false);
      if (input) input.focus();
      return;
    }

    if (saveBtn) {
      saveBtn.disabled = true;
      saveBtn.textContent = 'Saving...';
    }

    fetch(panelPath + '/api/agent', {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ agent_id: currentActiveAgentId, nickname: newNickname })
    }).then(r => {
      if (saveBtn) {
        saveBtn.disabled = false;
        saveBtn.textContent = 'Save';
      }
      if (r.ok) {
        const agent = agentsData.find(a => a.agent_id === currentActiveAgentId);
        if (agent) {
          agent.nickname = newNickname;
        }

        // Update active name in header
        const activeNameEl = document.getElementById('detail-active-name');
        if (activeNameEl) activeNameEl.textContent = newNickname;

        // Update overview grid card
        document.querySelectorAll('.agent-card-item[data-agent-id="' + currentActiveAgentId + '"]').forEach(card => {
          card.setAttribute('data-agent-name', newNickname);
          const nameSpan = card.querySelector('.agent-name span:not(.status-dot)');
          if (nameSpan) nameSpan.textContent = newNickname;
        });

        // Update overview list row
        document.querySelectorAll('.agent-row-item[data-agent-id="' + currentActiveAgentId + '"]').forEach(row => {
          row.setAttribute('data-agent-name', newNickname);
          const nameStrong = row.querySelector('.agent-name strong');
          if (nameStrong) nameStrong.textContent = newNickname;
        });

        cancelInpageAgentNameEdit();
        window.CertainStatsTelemetry.showToast('Agent renamed to ' + newNickname, true);
      } else {
        r.json().then(errData => {
          window.CertainStatsTelemetry.showToast(errData.error || 'Failed to rename agent', false);
        }).catch(() => {
          window.CertainStatsTelemetry.showToast('Failed to rename agent', false);
        });
      }
    }).catch(() => {
      if (saveBtn) {
        saveBtn.disabled = false;
        saveBtn.textContent = 'Save';
      }
      window.CertainStatsTelemetry.showToast('Network error renaming agent', false);
    });
  }

  function resetInpageEditStates() {
    cancelInpageAgentNameEdit();
    cancelInpageHeaderNoteEdit();
    cancelInpageExpandedNotesEdit();

    // Close any open modals
    ['reinstall-modal', 'uninstall-modal', 'add-agent-modal'].forEach(id => window.CertainStatsModal.close(id));
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

  function loadDetailMetrics(agentId, hours, customRange) {
    const requestGroup = window.CertainStatsRequests.begin('agents-detail-view', () => loadDetailMetrics(agentId, hours, customRange));
    const agent = agentsData.find(a => a.agent_id === agentId);
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
      requestGroup.json(panelPath + '/api/metrics?agent_id=' + agentId + '&metric=agent_cpu_usage&' + queryStr).catch(() => ({ series: [] })),
      requestGroup.json(panelPath + '/api/metrics?agent_id=' + agentId + '&metric=agent_cpu_iowait&' + queryStr).catch(() => ({ series: [] })),
      requestGroup.json(panelPath + '/api/metrics?agent_id=' + agentId + '&metric=agent_cpu_steal&' + queryStr).catch(() => ({ series: [] }))
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
      requestGroup.json(panelPath + '/api/metrics?agent_id=' + agentId + '&metric=agent_ram_used&' + queryStr).catch(() => ({ series: [] })),
      requestGroup.json(panelPath + '/api/metrics?agent_id=' + agentId + '&metric=agent_swap_used&' + queryStr).catch(() => ({ series: [] }))
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
      requestGroup.json(panelPath + '/api/metrics?agent_id=' + agentId + '&metric=agent_rx_bytes&' + queryStr).catch(() => ({ series: [] })),
      requestGroup.json(panelPath + '/api/metrics?agent_id=' + agentId + '&metric=agent_tx_bytes&' + queryStr).catch(() => ({ series: [] }))
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
      requestGroup.json(panelPath + '/api/metrics?agent_id=' + agentId + '&metric=agent_disk_used&' + queryStr).catch(() => ({ series: [] })),
      requestGroup.json(panelPath + '/api/metrics?agent_id=' + agentId + '&metric=agent_disk_read_bytes&' + queryStr).catch(() => ({ series: [] })),
      requestGroup.json(panelPath + '/api/metrics?agent_id=' + agentId + '&metric=agent_disk_write_bytes&' + queryStr).catch(() => ({ series: [] }))
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
        Object.values(inpageDiskCharts).forEach(dc => {
          if (dc.usageChart && dc.usageChart.destroy) dc.usageChart.destroy();
          if (dc.ioChart && dc.ioChart.destroy) dc.ioChart.destroy();
        });
        inpageDiskCharts = {};

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
      if (snap.is_online !== undefined) {agent.is_online=snap.is_online;document.querySelectorAll('[data-agent-id="' + id + '"]').forEach(node=>node.classList.toggle('is-offline',!snap.is_online));}
      if (snap.available===false) continue;
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

      if (valCpu) valCpu.textContent = (snap.cpu_usage_percent == null ? "Unavailable" : snap.cpu_usage_percent.toFixed(1) + "%");
      if (tdCpu) tdCpu.textContent = (snap.cpu_usage_percent == null ? "Unavailable" : snap.cpu_usage_percent.toFixed(1) + "%");
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

      const pctRam = document.getElementById('pct-ram-' + id);
      if (pctRam) pctRam.textContent = agent.ram_size ? Math.round(ramPct) + '%' : window.CertainStatsChart.formatBytes(snap.ram_used_bytes);
      if (valRam) valRam.textContent = agent.ram_size ? 'of ' + window.CertainStatsChart.formatBytes(agent.ram_size) : 'used';
      if (tdRam) tdRam.textContent = agent.ram_size ? Math.round(ramPct) + '%' : window.CertainStatsChart.formatBytes(snap.ram_used_bytes);
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
        const segDisk = document.getElementById('seg-disk-' + id);
        const valDisk = document.getElementById('val-disk-' + id);
        const pctDisk = document.getElementById('pct-disk-' + id);
        if (diskUsed > 0) {
          if (valDisk) valDisk.textContent = diskTotal ? 'of ' + window.CertainStatsChart.formatBytes(diskTotal) : 'used';
          if (pctDisk) pctDisk.textContent = diskTotal ? Math.round(diskPct) + '%' : window.CertainStatsChart.formatBytes(diskUsed);
          if (segDisk) {
            segDisk.style.width = Math.min(diskPct, 100) + '%';
            segDisk.classList.toggle('is-high', diskPct >= 90);
          }
        }
      }

      const trackDisk = document.getElementById('track-disk-' + id);
      if (trackDisk && diskUsed > 0) {
        trackDisk.setAttribute('data-tooltip-header', 'Disk');
        trackDisk.setAttribute('data-tooltip-rows', JSON.stringify([{ label: 'Used', val: window.CertainStatsChart.formatBytes(diskUsed) + (diskTotal ? ' / ' + window.CertainStatsChart.formatBytes(diskTotal) : ''), color: 'var(--s1)' }]));
      }
      if (trackDisk && snap.disks && snap.disks.length > 1) {
        const rows = snap.disks.map(d => ({
          label: d.path ? `Disk (${d.path})` : 'Disk',
          val: window.CertainStatsChart.formatBytes(d.used_bytes) + (d.total_bytes ? ' / ' + window.CertainStatsChart.formatBytes(d.total_bytes) : ''),
          color: 'var(--s1)'
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
      const netText = '↓ ' + window.CertainStatsChart.formatBps(snap.rx_bps) + ' · ↑ ' + window.CertainStatsChart.formatBps(snap.tx_bps);
      if (valNet) valNet.textContent = netText;
      const tdNet = document.getElementById('td-net-' + id);
      if (tdNet) tdNet.textContent = netText;
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
          { label: 'Used', val: (snap.cpu_usage_percent == null ? "Unavailable" : snap.cpu_usage_percent.toFixed(1) + "%"), color: 'var(--s1)' },
          { label: 'IO wait', val: (snap.cpu_iowait_percent == null ? "Unavailable" : snap.cpu_iowait_percent.toFixed(1) + "%"), color: 'var(--s2)' },
          { label: 'Steal', val: (snap.cpu_steal_percent == null ? "Unavailable" : snap.cpu_steal_percent.toFixed(1) + "%"), color: 'var(--s3)' }
        ]));
      }
      const trackRam = document.getElementById('track-ram-' + id);
      if (trackRam) {
        trackRam.setAttribute('data-tooltip-rows', JSON.stringify([
          { label: 'RAM used', val: window.CertainStatsChart.formatBytes(snap.ram_used_bytes), color: 'var(--s4)' },
          { label: 'Swap used', val: window.CertainStatsChart.formatBytes(snap.ram_swap_used_bytes), color: 'var(--s5)' }
        ]));
      }
      const trackNet = document.getElementById('track-net-' + id);
      if (trackNet) {
        trackNet.setAttribute('data-tooltip-rows', JSON.stringify([
          { label: 'Download', val: window.CertainStatsChart.formatBps(snap.rx_bps), color: 'var(--rx)' },
          { label: 'Upload', val: window.CertainStatsChart.formatBps(snap.tx_bps), color: 'var(--tx)' }
        ]));
      }
      const tdTrackNet = document.getElementById('td-track-net-' + id);
      if (tdTrackNet) {
        tdTrackNet.setAttribute('data-tooltip-rows', JSON.stringify([
          { label: 'Download', val: window.CertainStatsChart.formatBps(snap.rx_bps), color: 'var(--rx)' },
          { label: 'Upload', val: window.CertainStatsChart.formatBps(snap.tx_bps), color: 'var(--tx)' }
        ]));
      }

      if (snap.uptime != null) {
        agent.uptime = snap.uptime;
        const uptimeEl = document.getElementById('uptime-' + id);
        const tdUptimeEl = document.getElementById('td-uptime-' + id);
        const formatted = window.CertainStatsTelemetry.formatUptime(snap.uptime);
        if (uptimeEl) uptimeEl.textContent = 'Up ' + formatted;
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
        if (inpageDisk) inpageDisk.style.width = Math.min(diskPct, 100) + '%';
        if (inpageSwap) inpageSwap.style.width = Math.min(swapPct, 100) + '%';

        // Update in-page Disks Section and live specs
        renderInpageLiveState(id);

        // Update in-page live chart legend pill values
        const inpageCpuUsr = document.getElementById('inpage-live-cpu-usr'); if (inpageCpuUsr) inpageCpuUsr.textContent = (snap.cpu_usage_percent == null ? "Unavailable" : snap.cpu_usage_percent.toFixed(1) + "%");
        const inpageCpuIo = document.getElementById('inpage-live-cpu-io'); if (inpageCpuIo) inpageCpuIo.textContent = (snap.cpu_iowait_percent == null ? "Unavailable" : snap.cpu_iowait_percent.toFixed(1) + "%");
        const inpageCpuStl = document.getElementById('inpage-live-cpu-stl'); if (inpageCpuStl) inpageCpuStl.textContent = (snap.cpu_steal_percent == null ? "Unavailable" : snap.cpu_steal_percent.toFixed(1) + "%");
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
            if (cardDot) cardDot.className = 'status-dot ' + (isOnline === undefined || isOnline === null ? '' : isOnline ? 'online' : 'offline');
            const tdDot = document.getElementById('td-dot-' + fa.agent_id);
            if (tdDot) tdDot.className = 'status-dot ' + (isOnline === undefined || isOnline === null ? '' : isOnline ? 'online' : 'offline');
            const cardBadge = document.getElementById('badge-' + fa.agent_id);
            if (cardBadge) {
              cardBadge.className = 'badge ' + (isOnline === undefined || isOnline === null ? '' : isOnline ? 'badge-online' : 'badge-offline');
              cardBadge.textContent = isOnline ? 'Online' : 'Offline';
            }
            const card = document.querySelector('.agent-card-item[data-agent-id="' + CSS.escape(fa.agent_id) + '"]');
            if (card) card.classList.toggle('is-offline', !isOnline);
            if (currentActiveAgentId === fa.agent_id) {
              const activeDot = document.getElementById('detail-active-dot');
              if (activeDot) activeDot.className = 'status-dot ' + (isOnline === undefined || isOnline === null ? '' : isOnline ? 'online' : 'offline');
              const activeBadge = document.getElementById('detail-active-badge');
              if (activeBadge) {
                activeBadge.className = 'badge ' + (isOnline === undefined || isOnline === null ? '' : isOnline ? 'badge-online' : 'badge-offline');
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
            if (odoNet) odoNet.innerHTML = '<span>↓ ' + window.CertainStatsChart.formatBytes(existing.total_rx_bytes || 0) + '</span><span>↑ ' + window.CertainStatsChart.formatBytes(existing.total_tx_bytes || 0) + '</span>';
            const odoDisk = document.getElementById('inpage-odo-disk');
            if (odoDisk) odoDisk.innerHTML = '<span>R ' + window.CertainStatsChart.formatBytes(existing.total_disk_read_bytes || 0) + '</span><span>W ' + window.CertainStatsChart.formatBytes(existing.total_disk_write_bytes || 0) + '</span>';
          }
        });

        // Recalculate and re-render cluster overview totals
        window.CertainStatsTelemetry.renderClusterStats('admin-', agentsData, liveMetricsStore);
        renderHubHeadline();
      })
      .catch(() => {});
  }

  // Mirrors the server-rendered "hub_headline" template.
  function renderHubHeadline() {
    const el = document.getElementById('hub-headline');
    if (!el || agentsData.length === 0) return;
    const total = agentsData.length;
    const online = agentsData.filter(a => a.is_online === true).length;
    const offline = total - online;
    const bad = text => '<span class="is-bad">' + text + '</span>';
    let html;
    if (offline === 0) html = total === 1 ? 'Your monitored server is reporting.' : 'All ' + total + ' monitored servers reporting.';
    else if (online === 0) html = bad(total === 1 ? 'Your node is offline.' : 'All ' + total + ' nodes are offline.');
    else html = bad(offline + (offline === 1 ? ' node is' : ' nodes are') + ' offline.') + ' The other ' + online + (online === 1 ? ' is' : ' are') + ' reporting.';
    el.innerHTML = html;
  }

  // 24h CPU sparkline per card, fetched once at low resolution.
  function loadSparklines() {
    const canvases = document.querySelectorAll('canvas.agent-spark');
    canvases.forEach((canvas, i) => {
      const agentId = canvas.getAttribute('data-agent-id');
      setTimeout(() => {
        fetch(panelPath + '/api/metrics?agent_id=' + encodeURIComponent(agentId) + '&metric=agent_cpu_usage&hours=24')
          .then(r => r.ok ? r.json() : null)
          .then(res => {
            const points = res && res.series && res.series[0] ? res.series[0].data : [];
            canvas._sparkPoints = points;
            window.CertainStatsChart.drawSparkline(canvas, points, { color: '--s1', max: 100 });
          })
          .catch(() => {});
      }, i * 60);
    });
    const redraw = () => canvases.forEach(c => { if (c._sparkPoints) window.CertainStatsChart.drawSparkline(c, c._sparkPoints, { color: '--s1', max: 100 }); });
    window.addEventListener('certainstats_theme_change', redraw);
    let t;
    window.addEventListener('resize', () => { clearTimeout(t); t = setTimeout(redraw, 150); });
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
      try {
        viewMode = localStorage.getItem('certainstats_view_mode') || 'grid';
      } catch (e) {}
      setAgentViewMode(viewMode);
      loadSparklines();

      const historyRoot = document.querySelector('[data-incident-history]');
      if (historyRoot) nodeHistory = window.CertainStatsIncidentHistory.mount(historyRoot, { panelPath, nodeMode: true });

      // In-Place SPA Router (BASE_PATH/{AGENT_ID})
      window.CertainStatsTelemetry.initRouter({
        basePath: panelPath,
        onNavigate: function(agentId) {
          const overviewView = document.getElementById('agents-overview-view');
          const detailView = document.getElementById('agents-detail-view');

          resetInpageEditStates();

          if (!agentId || !agentsData.some(agent => agent.agent_id === agentId)) {
            if (detailView) detailView.hidden = true;
            if (overviewView) overviewView.hidden = false;
            currentActiveAgentId = null;
            if (nodeHistory) nodeHistory.setNode(null);
            window.scrollTo({ top: savedScrollY, behavior: 'instant' });
            return;
          }

          savedScrollY = window.scrollY;
          currentActiveAgentId = agentId;
          if (nodeHistory) nodeHistory.setNode(agentId);
          if (overviewView) overviewView.hidden = true;
          if (detailView) {
            detailView.hidden = false;
 const heading=detailView.querySelector("h1,h2");if (heading) {heading.setAttribute("tabindex","-1");heading.focus({preventScroll:true});}
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

      // Inpage Header Agent Name Enter/Escape Key Handler
      const inpageNameInput = document.getElementById('detail-name-input');
      if (inpageNameInput) {
        inpageNameInput.addEventListener('keydown', function(e) {
          if (e.key === 'Enter') saveInpageAgentName();
          if (e.key === 'Escape') cancelInpageAgentNameEdit();
        });
      }
    });

    const metadataTimer=setInterval(syncAdminAgentsMetadata, ADMIN_METADATA_SYNC_INTERVAL_MS);window.addEventListener("pagehide",()=>clearInterval(metadataTimer),{once:true});
  }

  // Export module namespace
  window.CertainStatsAdminAgents = {
    init: init,
    handleAgentItemClick: handleAgentItemClick,
    navigateToAgent: navigateToAgent,
    closeAgentDetail: closeAgentDetail,
    resetInpageEditStates: resetInpageEditStates,
    setAgentViewMode: setAgentViewMode,
    filterAgentsList: filterAgentsList,
    showInpageReinstallModal: showInpageReinstallModal,
    showInpageUninstallModal: showInpageUninstallModal,
    syncInpageNotesUI: syncInpageNotesUI,
    handleInpageHeaderNoteClick: handleInpageHeaderNoteClick,
    cancelInpageHeaderNoteEdit: cancelInpageHeaderNoteEdit,
    saveInpageHeaderInlineNote: saveInpageHeaderInlineNote,
    startInpageAgentNameEdit: startInpageAgentNameEdit,
    cancelInpageAgentNameEdit: cancelInpageAgentNameEdit,
    saveInpageAgentName: saveInpageAgentName,
    startInpageExpandedNotesEdit: startInpageExpandedNotesEdit,
    cancelInpageExpandedNotesEdit: cancelInpageExpandedNotesEdit,
    saveInpageExpandedNotes: saveInpageExpandedNotes,
    syncAdminAgentsMetadata: syncAdminAgentsMetadata
  };

  // Backwards compatibility globals for inline onclick handlers
  window.handleAgentItemClick = handleAgentItemClick;
  window.navigateToAgent = navigateToAgent;
  window.closeAgentDetail = closeAgentDetail;
  window.resetInpageEditStates = resetInpageEditStates;
  window.setAgentViewMode = setAgentViewMode;
  window.filterAgentsList = filterAgentsList;
  window.showInpageReinstallModal = showInpageReinstallModal;
  window.showInpageUninstallModal = showInpageUninstallModal;
  window.handleInpageHeaderNoteClick = handleInpageHeaderNoteClick;
  window.cancelInpageHeaderNoteEdit = cancelInpageHeaderNoteEdit;
  window.saveInpageHeaderInlineNote = saveInpageHeaderInlineNote;
  window.startInpageAgentNameEdit = startInpageAgentNameEdit;
  window.cancelInpageAgentNameEdit = cancelInpageAgentNameEdit;
  window.saveInpageAgentName = saveInpageAgentName;
  window.startInpageExpandedNotesEdit = startInpageExpandedNotesEdit;
  window.cancelInpageExpandedNotesEdit = cancelInpageExpandedNotesEdit;
  window.saveInpageExpandedNotes = saveInpageExpandedNotes;
})();
