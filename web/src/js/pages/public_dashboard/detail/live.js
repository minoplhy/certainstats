import { state } from '../state.js';
import { safeId } from '../nav/dom.js';

// Applies live telemetry pulses to the cards and the open detail.

export function renderPubInpageLiveState(pubId) {
  if (!pubId) return;
  const agent = state.pubAgentsData.find(a => a.public_id === pubId) || {};
  const snap = window.CertainStatsTelemetry.normalizeSnapshot(state.pubLiveMetricsStore[pubId]) || agent.latest_snap;

  // Render multi-disks immediately from remembered state
  const inpageDisksGrid = document.getElementById('pub-inpage-disks-grid');
  if (inpageDisksGrid && state.pubAllowedMetrics.includes('agent_disk_used')) {
    const disks = (agent.disks && agent.disks.length > 0) ? agent.disks : [];
    const showIO = state.pubAllowedMetrics.includes('agent_disk_read_bytes') || state.pubAllowedMetrics.includes('agent_disk_write_bytes');
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
    window.CertainStatsTelemetry.renderPartitions(inpageDisksGrid, partitionCards, {showIO, showRead:state.pubAllowedMetrics.includes('agent_disk_read_bytes'), showWrite:state.pubAllowedMetrics.includes('agent_disk_write_bytes')});
  }

  if (snap) {
    const inpageCpu = document.getElementById('pub-inpage-hw-cpu-bar');
    const inpageRam = document.getElementById('pub-inpage-hw-ram-bar');
    const inpageDisk = document.getElementById('pub-inpage-hw-disk-bar');
    const inpageSwap = document.getElementById('pub-inpage-hw-swap-bar');

    const ramPct = (agent.ram_size && agent.ram_size > 0) ? (snap.ram_used_bytes / agent.ram_size) * 100 : 0;
    const diskPct = (agent.disk_size && agent.disk_size > 0 && snap.disk_used_bytes > 0) ? (snap.disk_used_bytes / agent.disk_size) * 100 : 0;
    const swapPct = (agent.swap_size && agent.swap_size > 0 && snap.ram_swap_used_bytes > 0) ? (snap.ram_swap_used_bytes / agent.swap_size) * 100 : 0;

    if (inpageCpu && state.pubAllowedMetrics.includes('agent_cpu_usage')) inpageCpu.style.width = Math.min(snap.cpu_usage_percent, 100) + '%';
    if (inpageRam && state.pubAllowedMetrics.includes('agent_ram_used')) inpageRam.style.width = Math.min(ramPct, 100) + '%';
    if (inpageDisk && state.pubAllowedMetrics.includes('agent_disk_used')) inpageDisk.style.width = Math.min(diskPct, 100) + '%';
    if (inpageSwap && state.pubAllowedMetrics.includes('agent_swap_used')) inpageSwap.style.width = Math.min(swapPct, 100) + '%';

    const fmt = window.CertainStatsChart.formatBytes;
    const tileCpu = document.getElementById('pub-inpage-tile-cpu'); if (tileCpu) tileCpu.textContent = (snap.cpu_usage_percent == null ? "Unavailable" : snap.cpu_usage_percent.toFixed(1) + "%");
    const tileRam = document.getElementById('pub-inpage-tile-ram'); if (tileRam) tileRam.textContent = fmt(snap.ram_used_bytes);
    const tileDisk = document.getElementById('pub-inpage-tile-disk'); if (tileDisk) tileDisk.textContent = fmt(snap.disk_used_bytes);
    const tileSwap = document.getElementById('pub-inpage-tile-swap'); if (tileSwap) tileSwap.textContent = fmt(snap.ram_swap_used_bytes);

    const inpageCpuUsr = document.getElementById('pub-inpage-live-cpu-usr'); if (inpageCpuUsr && state.pubAllowedMetrics.includes('agent_cpu_usage')) inpageCpuUsr.textContent = (snap.cpu_usage_percent == null ? "Unavailable" : snap.cpu_usage_percent.toFixed(1) + "%");
    const inpageCpuIo = document.getElementById('pub-inpage-live-cpu-io'); if (inpageCpuIo && state.pubAllowedMetrics.includes('agent_cpu_iowait')) inpageCpuIo.textContent = (snap.cpu_iowait_percent == null ? "Unavailable" : snap.cpu_iowait_percent.toFixed(1) + "%");
    const inpageCpuStl = document.getElementById('pub-inpage-live-cpu-stl'); if (inpageCpuStl && state.pubAllowedMetrics.includes('agent_cpu_steal')) inpageCpuStl.textContent = (snap.cpu_steal_percent == null ? "Unavailable" : snap.cpu_steal_percent.toFixed(1) + "%");
    const inpageRamUsed = document.getElementById('pub-inpage-live-ram-used'); if (inpageRamUsed && state.pubAllowedMetrics.includes('agent_ram_used')) inpageRamUsed.textContent = window.CertainStatsChart.formatBytes(snap.ram_used_bytes);
    const inpageRamSwap = document.getElementById('pub-inpage-live-ram-swap'); if (inpageRamSwap && state.pubAllowedMetrics.includes('agent_swap_used')) inpageRamSwap.textContent = window.CertainStatsChart.formatBytes(snap.ram_swap_used_bytes);
    const inpageNetRx = document.getElementById('pub-inpage-live-net-rx'); if (inpageNetRx && state.pubAllowedMetrics.includes('agent_rx_bytes')) inpageNetRx.textContent = '↓ ' + window.CertainStatsChart.formatBps(snap.rx_bps);
    const inpageNetTx = document.getElementById('pub-inpage-live-net-tx'); if (inpageNetTx && state.pubAllowedMetrics.includes('agent_tx_bytes')) inpageNetTx.textContent = '↑ ' + window.CertainStatsChart.formatBps(snap.tx_bps);

    if (snap.uptime != null) {
      const inpageUptimeEl = document.getElementById('pub-inpage-spec-uptime');
      if (inpageUptimeEl) inpageUptimeEl.textContent = window.CertainStatsTelemetry.formatUptime(snap.uptime);
    }
  }
}

export function applyPublicTelemetryUpdates(snaps) {
  if (snaps && typeof snaps === 'object') {
    Object.assign(state.pubLiveMetricsStore, snaps);
  }
  window.CertainStatsTelemetry.renderClusterStats('pub-', state.pubAgentsData, state.pubLiveMetricsStore);

  for (const id in snaps) {
    const snap = window.CertainStatsTelemetry.normalizeSnapshot(snaps[id]);
    if (!snap) continue;

    const agent = state.pubAgentsData.find(a => a.public_id === id) || {};
    if (snap.is_online !== undefined) {agent.is_online=snap.is_online;document.querySelectorAll('[data-pub-id="' + id + '"]').forEach(node=>node.classList.toggle('is-offline',!snap.is_online));}
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
    const segUsr = document.getElementById('pub-seg-cpu-usr-' + id);
    const segIo = document.getElementById('pub-seg-cpu-io-' + id);
    const segStl = document.getElementById('pub-seg-cpu-stl-' + id);
    const valCpu = document.getElementById('pub-val-cpu-' + id);
    const tdSegCpu = document.getElementById('pub-td-seg-cpu-' + id);
    const tdCpu = document.getElementById('pub-td-cpu-' + id);

    if (valCpu) valCpu.textContent = (snap.cpu_usage_percent == null ? "Unavailable" : snap.cpu_usage_percent.toFixed(1) + "%");
    if (tdCpu) tdCpu.textContent = (snap.cpu_usage_percent == null ? "Unavailable" : snap.cpu_usage_percent.toFixed(1) + "%");
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

    const pctRam = document.getElementById('pub-pct-ram-' + id);
    const ramLabel = agent.ram_size ? Math.round(ramPct) + '%' : window.CertainStatsChart.formatBytes(snap.ram_used_bytes);
    if (pctRam) pctRam.textContent = ramLabel;
    if (valRam && agent.ram_size) valRam.textContent = 'of ' + window.CertainStatsChart.formatBytes(agent.ram_size);
    if (tdRam) tdRam.textContent = ramLabel;
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
    const diskPct = (pubDiskTotal > 0 && pubDiskUsed > 0 && state.pubAllowedMetrics.includes('agent_disk_used')) ? (pubDiskUsed / pubDiskTotal) * 100 : 0;

    if (diskBarsGroup && state.pubAllowedMetrics.includes('agent_disk_used') && pubDiskUsed > 0) {
      const segDisk = document.getElementById('pub-seg-disk-' + id);
      const valDisk = document.getElementById('pub-val-disk-' + id);
      const pctDisk = document.getElementById('pub-pct-disk-' + id);
      if (pctDisk) pctDisk.textContent = pubDiskTotal ? Math.round(diskPct) + '%' : window.CertainStatsChart.formatBytes(pubDiskUsed);
      if (valDisk && pubDiskTotal) valDisk.textContent = 'of ' + window.CertainStatsChart.formatBytes(pubDiskTotal);
      if (segDisk) {
        segDisk.style.width = Math.min(diskPct, 100) + '%';
        segDisk.classList.toggle('is-high', diskPct >= 90);
      }
    }
    if (state.pubAllowedMetrics.includes('agent_disk_used') && pubDiskUsed > 0) {
      const tdDisk = document.getElementById('pub-td-disk-' + id);
      const tdSegDisk = document.getElementById('pub-td-seg-disk-' + id);
      if (tdDisk) tdDisk.textContent = pubDiskTotal ? Math.round(diskPct) + '%' : window.CertainStatsChart.formatBytes(pubDiskUsed);
      if (tdSegDisk) {
        tdSegDisk.style.width = Math.min(diskPct, 100) + '%';
        tdSegDisk.classList.toggle('is-high', diskPct >= 90);
      }
    }

    if (state.pubAllowedMetrics.includes('agent_disk_used') && pubDiskUsed > 0) {
      const multi = snap.disks && snap.disks.length > 1;
      const rows = JSON.stringify(multi
        ? snap.disks.map(d => ({
          label: d.path ? `Disk (${d.path})` : 'Disk',
          val: window.CertainStatsChart.formatBytes(d.used_bytes) + (d.total_bytes ? ' / ' + window.CertainStatsChart.formatBytes(d.total_bytes) : ''),
          color: 'var(--s1)'
        }))
        : [{ label: 'Used', val: window.CertainStatsChart.formatBytes(pubDiskUsed) + (pubDiskTotal ? ' / ' + window.CertainStatsChart.formatBytes(pubDiskTotal) : ''), color: 'var(--s1)' }]);
      ['pub-track-disk-', 'pub-td-track-disk-'].forEach(prefix => {
        const pubTrackDisk = document.getElementById(prefix + id);
        if (!pubTrackDisk) return;
        pubTrackDisk.setAttribute('data-tooltip-rows', rows);
        pubTrackDisk.setAttribute('data-tooltip-header', multi ? 'Storage Partitions' : 'Disk');
      });
    }

    // Update Network bar
    const segRx = document.getElementById('pub-seg-net-rx-' + id);
    const segTx = document.getElementById('pub-seg-net-tx-' + id);
    const tdSegNetRx = document.getElementById('pub-td-seg-net-rx-' + id);
    const tdSegNetTx = document.getElementById('pub-td-seg-net-tx-' + id);
    const valNet = document.getElementById('pub-val-net-' + id);
    const netParts = [];
    if (state.pubAllowedMetrics.includes('agent_rx_bytes')) netParts.push('↓ ' + window.CertainStatsChart.formatBps(snap.rx_bps));
    if (state.pubAllowedMetrics.includes('agent_tx_bytes')) netParts.push('↑ ' + window.CertainStatsChart.formatBps(snap.tx_bps));
    if (valNet) valNet.textContent = netParts.join(' · ');
    const tdNet = document.getElementById('pub-td-net-' + id);
    if (tdNet) tdNet.textContent = netParts.join(' · ');
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
    const cpuRows = [];
    if (state.pubAllowedMetrics.includes('agent_cpu_usage')) cpuRows.push({ label: 'Used', val: (snap.cpu_usage_percent == null ? "Unavailable" : snap.cpu_usage_percent.toFixed(1) + "%"), color: 'var(--s1)' });
    if (state.pubAllowedMetrics.includes('agent_cpu_iowait')) cpuRows.push({ label: 'IO wait', val: (snap.cpu_iowait_percent == null ? "Unavailable" : snap.cpu_iowait_percent.toFixed(1) + "%"), color: 'var(--s2)' });
    if (state.pubAllowedMetrics.includes('agent_cpu_steal')) cpuRows.push({ label: 'Steal', val: (snap.cpu_steal_percent == null ? "Unavailable" : snap.cpu_steal_percent.toFixed(1) + "%"), color: 'var(--s3)' });
    ['pub-track-cpu-', 'pub-td-track-cpu-'].forEach(prefix => { const el = document.getElementById(prefix + id); if (el) el.setAttribute('data-tooltip-rows', JSON.stringify(cpuRows)); });
    const ramRows = [];
    if (state.pubAllowedMetrics.includes('agent_ram_used')) ramRows.push({ label: 'RAM used', val: window.CertainStatsChart.formatBytes(snap.ram_used_bytes), color: 'var(--s4)' });
    if (state.pubAllowedMetrics.includes('agent_swap_used')) ramRows.push({ label: 'Swap used', val: window.CertainStatsChart.formatBytes(snap.ram_swap_used_bytes), color: 'var(--s5)' });
    ['pub-track-ram-', 'pub-td-track-ram-'].forEach(prefix => { const el = document.getElementById(prefix + id); if (el) el.setAttribute('data-tooltip-rows', JSON.stringify(ramRows)); });
    const trackNet = document.getElementById('pub-track-net-' + id);
    if (trackNet) {
      const rows = [];
      if (state.pubAllowedMetrics.includes('agent_rx_bytes')) rows.push({ label: 'Download', val: window.CertainStatsChart.formatBps(snap.rx_bps), color: 'var(--rx)' });
      if (state.pubAllowedMetrics.includes('agent_tx_bytes')) rows.push({ label: 'Upload', val: window.CertainStatsChart.formatBps(snap.tx_bps), color: 'var(--tx)' });
      trackNet.setAttribute('data-tooltip-rows', JSON.stringify(rows));
    }
    const tdTrackNet = document.getElementById('pub-td-track-net-' + id);
    if (tdTrackNet) {
      const rows = [];
      if (state.pubAllowedMetrics.includes('agent_rx_bytes')) rows.push({ label: 'Download', val: window.CertainStatsChart.formatBps(snap.rx_bps), color: 'var(--rx)' });
      if (state.pubAllowedMetrics.includes('agent_tx_bytes')) rows.push({ label: 'Upload', val: window.CertainStatsChart.formatBps(snap.tx_bps), color: 'var(--tx)' });
      tdTrackNet.setAttribute('data-tooltip-rows', JSON.stringify(rows));
    }

    if (snap.uptime != null) {
      agent.uptime = snap.uptime;
      const uptimeEl = document.getElementById('pub-uptime-' + id);
      const tdUptimeEl = document.getElementById('pub-td-uptime-' + id);
      const formatted = window.CertainStatsTelemetry.formatUptime(snap.uptime);
      if (uptimeEl) uptimeEl.textContent = 'Up ' + formatted;
      if (tdUptimeEl) tdUptimeEl.textContent = formatted;
    }

    // Update in-page detail if currently viewed
    if (state.currentActivePubId === id) {
      const inpageCpu = document.getElementById('pub-inpage-hw-cpu-bar');
      const inpageRam = document.getElementById('pub-inpage-hw-ram-bar');
      const inpageDisk = document.getElementById('pub-inpage-hw-disk-bar');
      const inpageSwap = document.getElementById('pub-inpage-hw-swap-bar');

      if (inpageCpu && state.pubAllowedMetrics.includes('agent_cpu_usage')) inpageCpu.style.width = Math.min(snap.cpu_usage_percent, 100) + '%';
      if (inpageRam && state.pubAllowedMetrics.includes('agent_ram_used')) inpageRam.style.width = Math.min(ramPct, 100) + '%';
      if (inpageDisk && agent.disk_size > 0 && snap.disk_used_bytes > 0 && state.pubAllowedMetrics.includes('agent_disk_used')) {
        inpageDisk.style.width = Math.min(diskPct, 100) + '%';
      }
      if (inpageSwap && agent.swap_size > 0 && snap.ram_swap_used_bytes > 0 && state.pubAllowedMetrics.includes('agent_swap_used')) {
        inpageSwap.style.width = Math.min(swapPct, 100) + '%';
      }

      // Update in-page Disks Section and live specs
      renderPubInpageLiveState(id);

      const inpageCpuUsr = document.getElementById('pub-inpage-live-cpu-usr'); if (inpageCpuUsr && state.pubAllowedMetrics.includes('agent_cpu_usage')) inpageCpuUsr.textContent = (snap.cpu_usage_percent == null ? "Unavailable" : snap.cpu_usage_percent.toFixed(1) + "%");
      const inpageCpuIo = document.getElementById('pub-inpage-live-cpu-io'); if (inpageCpuIo && state.pubAllowedMetrics.includes('agent_cpu_iowait')) inpageCpuIo.textContent = (snap.cpu_iowait_percent == null ? "Unavailable" : snap.cpu_iowait_percent.toFixed(1) + "%");
      const inpageCpuStl = document.getElementById('pub-inpage-live-cpu-stl'); if (inpageCpuStl && state.pubAllowedMetrics.includes('agent_cpu_steal')) inpageCpuStl.textContent = (snap.cpu_steal_percent == null ? "Unavailable" : snap.cpu_steal_percent.toFixed(1) + "%");
      const inpageRamUsed = document.getElementById('pub-inpage-live-ram-used'); if (inpageRamUsed && state.pubAllowedMetrics.includes('agent_ram_used')) inpageRamUsed.textContent = window.CertainStatsChart.formatBytes(snap.ram_used_bytes);
      const inpageRamSwap = document.getElementById('pub-inpage-live-ram-swap'); if (inpageRamSwap && state.pubAllowedMetrics.includes('agent_swap_used')) inpageRamSwap.textContent = window.CertainStatsChart.formatBytes(snap.ram_swap_used_bytes);
      const inpageNetRx = document.getElementById('pub-inpage-live-net-rx'); if (inpageNetRx && state.pubAllowedMetrics.includes('agent_rx_bytes')) inpageNetRx.textContent = '↓ ' + window.CertainStatsChart.formatBps(snap.rx_bps);
      const inpageNetTx = document.getElementById('pub-inpage-live-net-tx'); if (inpageNetTx && state.pubAllowedMetrics.includes('agent_tx_bytes')) inpageNetTx.textContent = '↑ ' + window.CertainStatsChart.formatBps(snap.tx_bps);

      if (snap.uptime != null) {
        const inpageUptimeEl = document.getElementById('pub-inpage-spec-uptime');
        if (inpageUptimeEl) inpageUptimeEl.textContent = window.CertainStatsTelemetry.formatUptime(snap.uptime);
      }

      const ts = Date.now();
      if (state.pubInpageCpuChart) {
        const map = {};
        if (state.pubAllowedMetrics.includes('agent_cpu_usage')) map['Usr'] = snap.cpu_usage_percent;
        if (state.pubAllowedMetrics.includes('agent_cpu_iowait')) map['IO'] = snap.cpu_iowait_percent;
        if (state.pubAllowedMetrics.includes('agent_cpu_steal')) map['Stl'] = snap.cpu_steal_percent;
        state.pubInpageCpuChart.updateLivePoint(ts, map);
      }
      if (state.pubInpageRamChart) {
        const map = {};
        if (state.pubAllowedMetrics.includes('agent_ram_used')) map['RAM'] = snap.ram_used_bytes;
        if (state.pubAllowedMetrics.includes('agent_swap_used')) map['Swap'] = snap.ram_swap_used_bytes;
        state.pubInpageRamChart.updateLivePoint(ts, map);
      }
      if (state.pubInpageNetChart) {
        const map = {};
        if (state.pubAllowedMetrics.includes('agent_rx_bytes')) map['RX'] = snap.rx_bps;
        if (state.pubAllowedMetrics.includes('agent_tx_bytes')) map['TX'] = snap.tx_bps;
        state.pubInpageNetChart.updateLivePoint(ts, map);
      }

      if (snap.disks && snap.disks.length > 0) {
        snap.disks.forEach(d => {
          const path = d.path || '/';
          const safe = safeId(path);
          const elUsed = document.getElementById('pub-inpage-live-disk-used-' + safe); if (elUsed && state.pubAllowedMetrics.includes('agent_disk_used')) elUsed.textContent = window.CertainStatsChart.formatBytes(d.used_bytes);
          const elR = document.getElementById('pub-inpage-live-disk-read-' + safe); if (elR && state.pubAllowedMetrics.includes('agent_disk_read_bytes')) elR.textContent = window.CertainStatsChart.formatBps(d.read_bytes || 0);
          const elW = document.getElementById('pub-inpage-live-disk-write-' + safe); if (elW && state.pubAllowedMetrics.includes('agent_disk_write_bytes')) elW.textContent = window.CertainStatsChart.formatBps(d.write_bytes || 0);

          if (state.pubInpageDiskCharts[path]) {
            if (state.pubInpageDiskCharts[path].usageChart && state.pubAllowedMetrics.includes('agent_disk_used')) {
              state.pubInpageDiskCharts[path].usageChart.updateLivePoint(ts, { Used: d.used_bytes });
            }
            if (state.pubInpageDiskCharts[path].ioChart) {
              const map = {};
              if (state.pubAllowedMetrics.includes('agent_disk_read_bytes')) map['Read'] = d.read_bytes || 0;
              if (state.pubAllowedMetrics.includes('agent_disk_write_bytes')) map['Write'] = d.write_bytes || 0;
              state.pubInpageDiskCharts[path].ioChart.updateLivePoint(ts, map);
            }
          }
        });
      } else {
        const elUsed = document.getElementById('pub-inpage-live-disk-used-root'); if (elUsed && state.pubAllowedMetrics.includes('agent_disk_used')) elUsed.textContent = window.CertainStatsChart.formatBytes(snap.disk_used_bytes);
        const elR = document.getElementById('pub-inpage-live-disk-read-root'); if (elR && state.pubAllowedMetrics.includes('agent_disk_read_bytes')) elR.textContent = window.CertainStatsChart.formatBps(snap.disk_read_bps);
        const elW = document.getElementById('pub-inpage-live-disk-write-root'); if (elW && state.pubAllowedMetrics.includes('agent_disk_write_bytes')) elW.textContent = window.CertainStatsChart.formatBps(snap.disk_write_bps);

        if (state.pubInpageDiskCharts['/']) {
          if (state.pubInpageDiskCharts['/'].usageChart && state.pubAllowedMetrics.includes('agent_disk_used')) {
            state.pubInpageDiskCharts['/'].usageChart.updateLivePoint(ts, { Used: snap.disk_used_bytes });
          }
          if (state.pubInpageDiskCharts['/'].ioChart) {
            const map = {};
            if (state.pubAllowedMetrics.includes('agent_disk_read_bytes')) map['Read'] = snap.disk_read_bps;
            if (state.pubAllowedMetrics.includes('agent_disk_write_bytes')) map['Write'] = snap.disk_write_bps;
            state.pubInpageDiskCharts['/'].ioChart.updateLivePoint(ts, map);
          }
        }
      }
    }
  }
}
