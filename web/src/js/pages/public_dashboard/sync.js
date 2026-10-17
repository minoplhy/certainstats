import { state, PUB_METADATA_SYNC_INTERVAL_MS } from './state.js';

// Periodic metadata refresh for the dashboard.

export function syncPublicDashboardMetadata(force) {
  const now = Date.now();
  if (!force && (now - state.lastPubSyncTime < PUB_METADATA_SYNC_INTERVAL_MS)) return;
  state.lastPubSyncTime = now;

  const publicPath = (document.body?.getAttribute('data-public-path') || '').replace(/\/+$/, '');
  if (!state.dashSlug) return;

  fetch(publicPath + '/api/public/dashboard/' + encodeURIComponent(state.dashSlug))
    .then(r => r.ok ? r.json() : null)
    .then(data => {
      if (!data || !data.agents || !Array.isArray(data.agents)) return;

      data.agents.forEach(fa => {
        let existing = state.pubAgentsData.find(a => a.public_id === fa.public_id);
        if (!existing) {
          existing = { public_id: fa.public_id };
          state.pubAgentsData.push(existing);
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
          if (cardDot) cardDot.className = 'status-dot ' + (isOnline === undefined || isOnline === null ? '' : isOnline ? 'online' : 'offline');
          const tdDot = document.getElementById('pub-td-dot-' + fa.public_id);
          if (tdDot) tdDot.className = 'status-dot ' + (isOnline === undefined || isOnline === null ? '' : isOnline ? 'online' : 'offline');
          ['pub-badge-', 'pub-td-badge-'].forEach(prefix => {
            const badge = document.getElementById(prefix + fa.public_id);
            if (badge) {
              badge.className = 'badge ' + (isOnline === undefined || isOnline === null ? '' : isOnline ? 'badge-online' : 'badge-offline');
              badge.textContent = isOnline ? 'Online' : 'Offline';
            }
          });
          document.querySelectorAll('[data-pub-id="' + CSS.escape(fa.public_id) + '"]').forEach(el => el.classList.toggle('is-offline', !isOnline));
          if (state.currentActivePubId === fa.public_id) {
            const inpageDot = document.getElementById('pub-inpage-dot');
            if (inpageDot) inpageDot.className = 'status-dot ' + (isOnline === undefined || isOnline === null ? '' : isOnline ? 'online' : 'offline');
            const inpageBadge = document.getElementById('pub-inpage-badge');
            if (inpageBadge) {
              inpageBadge.className = 'badge ' + (isOnline === undefined || isOnline === null ? '' : isOnline ? 'badge-online' : 'badge-offline');
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
        if (state.currentActivePubId === fa.public_id) {
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
          if (odoNet) odoNet.innerHTML = '<span>↓ ' + window.CertainStatsChart.formatBytes(existing.total_rx_bytes || 0) + '</span><span>↑ ' + window.CertainStatsChart.formatBytes(existing.total_tx_bytes || 0) + '</span>';
          let dr = 0, dw = 0;
          if (existing.disks) existing.disks.forEach(d => { dr += d.read_bytes || 0; dw += d.write_bytes || 0; });
          const odoDisk = document.getElementById('pub-inpage-odo-disk');
          if (odoDisk) odoDisk.innerHTML = '<span>R ' + window.CertainStatsChart.formatBytes(dr) + '</span><span>W ' + window.CertainStatsChart.formatBytes(dw) + '</span>';
        }
      });

      // Recalculate and re-render cluster overview totals
      window.CertainStatsTelemetry.renderClusterStats('pub-', state.pubAgentsData, state.pubLiveMetricsStore);
    })
    .catch(() => {});
}
