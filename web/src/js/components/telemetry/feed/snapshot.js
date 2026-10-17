export function normalizeSnapshot(snap) {
  if (!snap) return null;
const status=snap.is_online ?? snap.IsOnline;const available=snap.available;
snap=snap.snapshot || snap;
  return {
is_online: status, available,
    cpu_usage_percent: snap.cpu_usage_percent ?? snap.CPUUsagePercent ?? snap.cpuUsagePercent ?? null,
    cpu_iowait_percent: snap.cpu_iowait_percent ?? snap.CPUIOWaitPercent ?? snap.cpuIOWaitPercent ?? null,
    cpu_steal_percent: snap.cpu_steal_percent ?? snap.CPUStealPercent ?? snap.cpuStealPercent ?? null,
    ram_used_bytes: snap.ram_used_bytes ?? snap.RAMUsedBytes ?? snap.ramUsedBytes ?? null,
    ram_swap_used_bytes: snap.ram_swap_used_bytes ?? snap.RAMSwapUsedBytes ?? snap.ramSwapUsedBytes ?? null,
    disk_used_bytes: snap.disk_used_bytes ?? snap.DiskUsedBytes ?? snap.diskUsedBytes ?? null,
    disk_total_bytes: snap.disk_total_bytes ?? snap.DiskTotalBytes ?? snap.diskTotalBytes ?? null,
    disks: (snap.disks || snap.Disks || []).map(d => ({
      path: d.path || d.Path || '',
      total_bytes: d.total_bytes ?? d.TotalBytes ?? null,
      used_bytes: d.used_bytes ?? d.UsedBytes ?? null,
      read_bytes: d.read_bytes ?? d.ReadBytes ?? null,
      write_bytes: d.write_bytes ?? d.WriteBytes ?? null
    })),
    rx_bytes: snap.rx_bytes ?? snap.RXBytes ?? snap.rxBytes ?? null,
    tx_bytes: snap.tx_bytes ?? snap.TXBytes ?? snap.txBytes ?? null,
    rx_bps: snap.rx_bps ?? snap.RXBps ?? snap.rxBps ?? null,
    tx_bps: snap.tx_bps ?? snap.TXBps ?? snap.txBps ?? null,
    disk_read_bps: snap.disk_read_bps ?? snap.DiskReadBps ?? snap.diskReadBps ?? null,
    disk_write_bps: snap.disk_write_bps ?? snap.DiskWriteBps ?? snap.diskWriteBps ?? null,
    uptime: snap.uptime ?? snap.Uptime ?? null
  };
}
