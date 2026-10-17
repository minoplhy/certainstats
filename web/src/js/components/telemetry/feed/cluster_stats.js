import { normalizeSnapshot } from './snapshot.js';

// Cluster Stats Calculation & DOM Rendering (4 Dual-Metric Cards)
// Storage partition card used by both the admin and public detail views.
export function renderPartitions(container, cards, opts) {
  opts = opts || {};
  const existing = new Map(Array.from(container.children).map(node => [node.dataset.path, node]));
  const fmt = window.CertainStatsChart.formatBytes;
  for (const card of cards) {
    let node = existing.get(card.path);
    if (!node) {
      node = document.createElement('div');node.className='partition-card';node.dataset.path=card.path;
      const head=document.createElement('div');head.className='partition-head';
      const path=document.createElement('span');path.className='partition-path mono';path.textContent=card.path;
      const pct=document.createElement('span');pct.className='partition-pct mono';
      head.append(path,pct);
      const value=document.createElement('div');value.className='partition-val mono';
      const track=document.createElement('div');track.className='hw-card-progress-track';
      const bar=document.createElement('div');bar.className='hw-card-progress-fill seg-disk';track.append(bar);
      const io=document.createElement('div');io.className='partition-io mono';
      io.append(document.createElement('span'),document.createElement('span'));
      node.append(head,value,track,io);container.append(node);
    }
    existing.delete(card.path);
    const pct=card.total>0 && card.used!=null ? card.used/card.total*100 : null;
    node.querySelector('.partition-pct').textContent=pct==null ? 'Unavailable' : pct.toFixed(1)+'%';
    node.querySelector('.partition-val').textContent=fmt(card.used)+(card.total>0 ? ' of '+fmt(card.total) : '');
    const bar=node.querySelector('.hw-card-progress-fill');bar.style.width=Math.min(pct ?? 0,100)+'%';bar.classList.toggle('is-high',pct>=90);
    const io=node.querySelector('.partition-io');io.hidden=opts.showIO===false;
    io.children[0].hidden=opts.showRead===false;io.children[1].hidden=opts.showWrite===false;
    io.children[0].textContent='Read '+fmt(card.read);io.children[1].textContent='Written '+fmt(card.write);
  }
  for (const node of existing.values()) node.remove();
}

export function renderClusterStats(prefix, agents, liveMetrics) {
  let liveRxBps = 0, liveTxBps = 0;
  let liveDiskReadBps = 0, liveDiskWriteBps = 0;
  let totalRxBytes = 0, totalTxBytes = 0;
  let totalDiskReadBytes = 0, totalDiskWriteBytes = 0;
  let onlineCount = 0;

  agents.forEach(a => {
    const id = a.agent_id || a.public_id;
    const rawSnap = liveMetrics[id];
    const snap = normalizeSnapshot(rawSnap);
    const isOnline = a.is_online != null ? a.is_online : a.IsOnline;
    if (isOnline) onlineCount++;

    if (snap) {
      liveRxBps += snap.rx_bps || 0;
      liveTxBps += snap.tx_bps || 0;
      liveDiskReadBps += snap.disk_read_bps || 0;
      liveDiskWriteBps += snap.disk_write_bps || 0;
    }

    if (a.net && (a.net.total_rx_bytes != null || a.net.TotalRxBytes != null)) {
      totalRxBytes += a.net.total_rx_bytes || a.net.TotalRxBytes || 0;
      totalTxBytes += a.net.total_tx_bytes || a.net.TotalTxBytes || 0;
    } else if (a.total_rx_bytes != null || a.TotalRxBytes != null) {
      totalRxBytes += a.total_rx_bytes || a.TotalRxBytes || 0;
      totalTxBytes += a.total_tx_bytes || a.TotalTxBytes || 0;
    }

    let agentDiskRead = a.total_disk_read_bytes || a.TotalDiskReadBytes || 0;
    let agentDiskWrite = a.total_disk_write_bytes || a.TotalDiskWriteBytes || 0;
    if (!agentDiskRead && !agentDiskWrite) {
      const disks = a.disks || a.Disks || [];
      disks.forEach(d => {
        agentDiskRead += d.read_bytes || d.ReadBytes || 0;
        agentDiskWrite += d.write_bytes || d.WriteBytes || 0;
      });
    }
    totalDiskReadBytes += agentDiskRead;
    totalDiskWriteBytes += agentDiskWrite;
  });

  // Update Public Header Status
  const overallDot = document.getElementById('public-overall-status-dot');
  const overallText = document.getElementById('public-overall-status-text');
  const total = agents.length;
  const offline = total - onlineCount;
const knownStatus=agents.some(a=>a.is_online !== undefined || a.IsOnline !== undefined);
  if (overallDot && overallText) {
    if (!knownStatus) {overallDot.className="status-dot";overallText.textContent="Status unavailable";} else if (total === 0) {
      overallDot.className = 'status-dot';
      overallText.textContent = 'No servers yet';
    } else if (offline === 0) {
      overallDot.className = 'status-dot online';
      overallText.textContent = 'Monitored servers reporting';
    } else {
      overallDot.className = 'status-dot offline';
      overallText.textContent = onlineCount + ' of ' + total + ' monitored servers reporting';
    }
  }

  // Public status banner (mirrors the server-rendered text in public_dashboard.html)
  const banner = document.getElementById('public-status-banner');
  if (banner && total > 0) {
    const bannerText = document.getElementById('public-status-banner-text');
    const bannerDot = document.getElementById('public-status-banner-dot');
    let cls = 'is-ok', text = 'All systems operational';
    if (offline > 0 && onlineCount === 0) {
      cls = 'is-bad';
      text = 'Major outage: all ' + total + ' servers offline';
    } else if (offline > 0) {
      cls = 'is-warn';
      text = 'Partial outage: ' + offline + ' of ' + total + (total === 1 ? ' server' : ' servers') + ' offline';
    }
    banner.className = 'banner ' + cls;
    if (bannerText) bannerText.textContent = text;
    if (bannerDot) bannerDot.className = 'status-dot ' + (offline === 0 ? 'online' : 'offline');
  }

  // Update Card 1: TOTAL BANDWIDTH (Live Rate)
  const elRx = document.getElementById(prefix + 'live-rx-val');
  const elTx = document.getElementById(prefix + 'live-tx-val');
  if (elRx) elRx.innerHTML = '↓ ' + window.CertainStatsChart.formatBps(liveRxBps);
  if (elTx) elTx.innerHTML = '↑ ' + window.CertainStatsChart.formatBps(liveTxBps);

  // Update Card 2: TOTAL DISK I/O (Live Rate)
  const elDiskR = document.getElementById(prefix + 'live-disk-read-val');
  const elDiskW = document.getElementById(prefix + 'live-disk-write-val');
  if (elDiskR) elDiskR.innerHTML = window.CertainStatsChart.formatBps(liveDiskReadBps);
  if (elDiskW) elDiskW.innerHTML = window.CertainStatsChart.formatBps(liveDiskWriteBps);

  // Update Card 3: TOTAL TRAFFIC (Cumulative Lifetime)
  const elTotRx = document.getElementById(prefix + 'total-rx-val');
  const elTotTx = document.getElementById(prefix + 'total-tx-val');
  if (elTotRx) elTotRx.innerHTML = '↓ ' + window.CertainStatsChart.formatBytes(totalRxBytes);
  if (elTotTx) elTotTx.innerHTML = '↑ ' + window.CertainStatsChart.formatBytes(totalTxBytes);

  // Update Card 4: TOTAL DISK READ/WRITE (Cumulative Lifetime)
  const elTotDiskR = document.getElementById(prefix + 'total-disk-read-val');
  const elTotDiskW = document.getElementById(prefix + 'total-disk-write-val');
  if (elTotDiskR) elTotDiskR.innerHTML = window.CertainStatsChart.formatBytes(totalDiskReadBytes);
  if (elTotDiskW) elTotDiskW.innerHTML = window.CertainStatsChart.formatBytes(totalDiskWriteBytes);
}
