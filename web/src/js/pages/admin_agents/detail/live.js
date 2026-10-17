import { state } from '../state.js';
import { renderInpageLiveState } from './load.js';
import { safeId } from './dom.js';

// Applies live telemetry pulses to the hub and the open detail.

export function applyTelemetryUpdates(snaps) {
  if (snaps && typeof snaps === 'object') {
    Object.assign(state.liveMetricsStore, snaps);
  }
  window.CertainStatsTelemetry.renderClusterStats('admin-', state.agentsData, state.liveMetricsStore);

  for (const id in snaps) {
    const snap = window.CertainStatsTelemetry.normalizeSnapshot(snaps[id]);
    if (!snap) continue;

    const agent = state.agentsData.find(a => a.agent_id === id) || {};
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
    const tdDisk = document.getElementById('td-disk-' + id);
    const tdSegDisk = document.getElementById('td-seg-disk-' + id);
    if (diskUsed > 0) {
      if (tdDisk) tdDisk.textContent = diskTotal ? Math.round(diskPct) + '%' : window.CertainStatsChart.formatBytes(diskUsed);
      if (tdSegDisk) {
        tdSegDisk.style.width = Math.min(diskPct, 100) + '%';
        tdSegDisk.classList.toggle('is-high', diskPct >= 90);
      }
    }

    ['track-disk-', 'td-track-disk-'].forEach(prefix => {
      const trackDisk = document.getElementById(prefix + id);
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
    });

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
    const cpuRows = JSON.stringify([
      { label: 'Used', val: (snap.cpu_usage_percent == null ? "Unavailable" : snap.cpu_usage_percent.toFixed(1) + "%"), color: 'var(--s1)' },
      { label: 'IO wait', val: (snap.cpu_iowait_percent == null ? "Unavailable" : snap.cpu_iowait_percent.toFixed(1) + "%"), color: 'var(--s2)' },
      { label: 'Steal', val: (snap.cpu_steal_percent == null ? "Unavailable" : snap.cpu_steal_percent.toFixed(1) + "%"), color: 'var(--s3)' }
    ]);
    ['track-cpu-', 'td-track-cpu-'].forEach(prefix => { const el = document.getElementById(prefix + id); if (el) el.setAttribute('data-tooltip-rows', cpuRows); });
    const ramRows = JSON.stringify([
      { label: 'RAM used', val: window.CertainStatsChart.formatBytes(snap.ram_used_bytes), color: 'var(--s4)' },
      { label: 'Swap used', val: window.CertainStatsChart.formatBytes(snap.ram_swap_used_bytes), color: 'var(--s5)' }
    ]);
    ['track-ram-', 'td-track-ram-'].forEach(prefix => { const el = document.getElementById(prefix + id); if (el) el.setAttribute('data-tooltip-rows', ramRows); });
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
    if (state.currentActiveAgentId === id) {
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

      if (!state.inpageCustomRange) {
        const ts = Date.now();
        if (state.inpageCpuChart) state.inpageCpuChart.updateLivePoint(ts, { Usr: snap.cpu_usage_percent, IO: snap.cpu_iowait_percent, Stl: snap.cpu_steal_percent });
        if (state.inpageRamChart) state.inpageRamChart.updateLivePoint(ts, { RAM: snap.ram_used_bytes, Swap: snap.ram_swap_used_bytes });
        if (state.inpageNetChart) state.inpageNetChart.updateLivePoint(ts, { RX: snap.rx_bps, TX: snap.tx_bps });

        if (snap.disks && snap.disks.length > 0) {
          snap.disks.forEach(d => {
            const path = d.path || '/';
            const safe = safeId(path);
            const elUsed = document.getElementById('inpage-live-disk-used-' + safe); if (elUsed) elUsed.textContent = window.CertainStatsChart.formatBytes(d.used_bytes);
            const elR = document.getElementById('inpage-live-disk-read-' + safe); if (elR) elR.textContent = window.CertainStatsChart.formatBps(d.read_bytes || 0);
            const elW = document.getElementById('inpage-live-disk-write-' + safe); if (elW) elW.textContent = window.CertainStatsChart.formatBps(d.write_bytes || 0);

            if (state.inpageDiskCharts[path]) {
              if (state.inpageDiskCharts[path].usageChart) state.inpageDiskCharts[path].usageChart.updateLivePoint(ts, { Used: d.used_bytes });
              if (state.inpageDiskCharts[path].ioChart) state.inpageDiskCharts[path].ioChart.updateLivePoint(ts, { Read: d.read_bytes || 0, Write: d.write_bytes || 0 });
            }
          });
        } else {
          const elUsed = document.getElementById('inpage-live-disk-used-root'); if (elUsed) elUsed.textContent = window.CertainStatsChart.formatBytes(snap.disk_used_bytes);
          const elR = document.getElementById('inpage-live-disk-read-root'); if (elR) elR.textContent = window.CertainStatsChart.formatBps(snap.disk_read_bps);
          const elW = document.getElementById('inpage-live-disk-write-root'); if (elW) elW.textContent = window.CertainStatsChart.formatBps(snap.disk_write_bps);

          if (state.inpageDiskCharts['/']) {
            if (state.inpageDiskCharts['/'].usageChart) state.inpageDiskCharts['/'].usageChart.updateLivePoint(ts, { Used: snap.disk_used_bytes });
            if (state.inpageDiskCharts['/'].ioChart) state.inpageDiskCharts['/'].ioChart.updateLivePoint(ts, { Read: snap.disk_read_bps, Write: snap.disk_write_bps });
          }
        }
      }
    }
  }
}
