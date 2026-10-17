import { state, ADMIN_METADATA_SYNC_INTERVAL_MS } from '../state.js';
import { renderAgentVersion } from '../detail/version.js';

// Hub metadata sync, headline and card sparklines.

export function syncAdminAgentsMetadata(force) {
  const now = Date.now();
  if (!force && (now - state.lastAdminSyncTime < ADMIN_METADATA_SYNC_INTERVAL_MS)) return;
  state.lastAdminSyncTime = now;

  fetch(state.panelPath + '/api/agents')
    .then(r => r.ok ? r.json() : null)
    .then(freshAgents => {
      if (!freshAgents || !Array.isArray(freshAgents)) return;

      freshAgents.forEach(fa => {
        let existing = state.agentsData.find(a => a.agent_id === fa.agent_id);
        if (!existing) {
          existing = { agent_id: fa.agent_id };
          state.agentsData.push(existing);
        }
        existing.nickname = fa.nickname || existing.nickname;
        if (fa.extensions) existing.extensions = fa.extensions;
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
          if (state.currentActiveAgentId === fa.agent_id) {
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
        if (state.currentActiveAgentId === fa.agent_id) {
          renderAgentVersion(existing);
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
      window.CertainStatsTelemetry.renderClusterStats('admin-', state.agentsData, state.liveMetricsStore);
      renderHubHeadline();
    })
    .catch(() => {});
}

// Mirrors the server-rendered "hub_headline" template.
export function renderHubHeadline() {
  const el = document.getElementById('hub-headline');
  if (!el || state.agentsData.length === 0) return;
  const total = state.agentsData.length;
  const online = state.agentsData.filter(a => a.is_online === true).length;
  const offline = total - online;
  const bad = text => '<span class="is-bad">' + text + '</span>';
  let html;
  if (offline === 0) html = total === 1 ? 'Your monitored server is reporting.' : 'All ' + total + ' monitored servers reporting.';
  else if (online === 0) html = bad(total === 1 ? 'Your node is offline.' : 'All ' + total + ' nodes are offline.');
  else html = bad(offline + (offline === 1 ? ' node is' : ' nodes are') + ' offline.') + ' The other ' + online + (online === 1 ? ' is' : ' are') + ' reporting.';
  el.innerHTML = html;
}

// 24h CPU sparkline per card, fetched once at low resolution.
export function loadSparklines() {
  const canvases = document.querySelectorAll('canvas.agent-spark');
  const group = window.CertainStatsRequests.begin('agents-overview-view', loadSparklines, { silent: true });
  canvases.forEach((canvas, i) => {
    if (canvas._sparkPoints) return;
    const agentId = canvas.getAttribute('data-agent-id');
    const timer = setTimeout(() => {
      if (state.activeRoute !== 'overview' || document.hidden || group.signal.aborted) return;
      fetch(state.panelPath + '/api/metrics?agent_id=' + encodeURIComponent(agentId) + '&metric=agent_cpu_usage&hours=24', {signal: group.signal})
        .then(r => r.ok ? r.json() : null)
        .then(res => {
          if (group.signal.aborted) return;
          const points = res && res.series && res.series[0] ? res.series[0].data : [];
          canvas._sparkPoints = points;
          window.CertainStatsChart.drawSparkline(canvas, points, { color: '--s1', max: 100 });
        })
        .catch(() => {});
    }, i * 60);
    group.signal.addEventListener('abort', () => clearTimeout(timer), { once: true });
  });
  if (state.sparklineListenersReady) return;
  state.sparklineListenersReady = true;
  window.addEventListener('certainstats_theme_change', redrawSparklines);
  let t;
  window.addEventListener('resize', () => { clearTimeout(t); t = setTimeout(redrawSparklines, 150); });
}

// Canvases inside a hidden grid have no size, so sparklines fetched in list view are drawn once the grid is shown.
export function redrawSparklines() {
  document.querySelectorAll('canvas.agent-spark').forEach(c => { if (c._sparkPoints) window.CertainStatsChart.drawSparkline(c, c._sparkPoints, { color: '--s1', max: 100 }); });
}
