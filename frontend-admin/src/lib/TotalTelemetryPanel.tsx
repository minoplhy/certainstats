import { FC } from 'react';

export interface TotalTelemetryPanelProps {
  filteredAgents: any[];
  liveMetrics: Record<string, any>;
  allowedMetrics?: string[]; // Gated metrics (undefined = allow all)
  fmtBytes: (b: number) => string;
  fmtBps: (v: number) => string;
  isPublic?: boolean;
}

export const TotalTelemetryPanel: FC<TotalTelemetryPanelProps> = ({
  filteredAgents,
  liveMetrics,
  allowedMetrics,
  fmtBytes,
  fmtBps,
  isPublic = false
}) => {
  // Determine if specific metrics are allowed (default to true for admin/private panel)
  const showRx = !allowedMetrics || allowedMetrics.includes('agent_rx_bytes');
  const showTx = !allowedMetrics || allowedMetrics.includes('agent_tx_bytes');
  const showDiskRead = !allowedMetrics || allowedMetrics.includes('agent_disk_read_bytes');
  const showDiskWrite = !allowedMetrics || allowedMetrics.includes('agent_disk_write_bytes');

  if (filteredAgents.length === 0 || (!showRx && !showTx && !showDiskRead && !showDiskWrite)) {
    return null;
  }

  // Calculate cluster stats for all visible agents
  const clusterStats = filteredAgents.reduce((acc, a) => {
    const key = isPublic ? a.public_id : a.agent_id;
    const snap = liveMetrics[key];
    
    // Real-time rates for online agents
    if (a.is_online && snap) {
      if (showRx) acc.liveRxBps += snap.RXBps || 0;
      if (showTx) acc.liveTxBps += snap.TXBps || 0;
      if (showDiskRead) acc.liveDiskReadBps += snap.DiskReadBps || 0;
      if (showDiskWrite) acc.liveDiskWriteBps += snap.DiskWriteBps || 0;
    }
    
    // Cumulative volume totals
    if (a.net) {
      if (showRx) acc.totalRxBytes += a.net.total_rx_bytes || 0;
      if (showTx) acc.totalTxBytes += a.net.total_tx_bytes || 0;
    }
    if (a.disks) {
      a.disks.forEach((d: any) => {
        if (showDiskRead) acc.totalDiskReadBytes += d.read_bytes || 0;
        if (showDiskWrite) acc.totalDiskWriteBytes += d.write_bytes || 0;
      });
    }
    
    return acc;
  }, {
    liveRxBps: 0, liveTxBps: 0,
    liveDiskReadBps: 0, liveDiskWriteBps: 0,
    totalRxBytes: 0, totalTxBytes: 0,
    totalDiskReadBytes: 0, totalDiskWriteBytes: 0
  });

  return (
    <div style={{
      display: 'grid',
      gridTemplateColumns: 'repeat(auto-fit, minmax(260px, 1fr))',
      gap: '16px',
      marginBottom: '16px',
      width: '100%',
      boxSizing: 'border-box'
    }}>
      {/* Card 1: TOTAL BANDWIDTH */}
      {(showRx || showTx) && (
        <div style={{
          display: 'flex',
          flexDirection: 'column',
          gap: '12px',
          padding: '16px 20px',
          borderRadius: '16px',
          background: 'var(--bg-secondary)',
          border: '1px solid var(--border-color)',
          boxShadow: 'var(--card-shadow)',
          position: 'relative',
          overflow: 'hidden'
        }}>
          <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
            <span style={{ fontSize: '11px', textTransform: 'uppercase', letterSpacing: '0.05em', color: 'var(--text-muted)', fontWeight: '700' }}>TOTAL BANDWIDTH</span>
            <span className="material-symbols-outlined" style={{ fontSize: '18px', color: 'var(--accent-primary)', opacity: 0.8 }}>swap_vertical_circle</span>
          </div>
          <div style={{ display: 'flex', gap: '16px', alignItems: 'center', flexWrap: 'nowrap' }}>
            {showRx && (
              <div style={{ flex: 1, minWidth: 0 }}>
                <div style={{ fontSize: '11px', color: 'var(--text-muted)', marginBottom: '2px', whiteSpace: 'nowrap' }}>Download</div>
                <div style={{ fontSize: '16px', fontWeight: '800', color: 'var(--text-primary)', fontFamily: 'var(--font-mono)', whiteSpace: 'nowrap', overflow: 'hidden', textOverflow: 'ellipsis' }}>↓ {fmtBps(clusterStats.liveRxBps)}</div>
              </div>
            )}
            {showRx && showTx && (
              <div style={{ width: '1px', height: '24px', background: 'var(--border-color)', flexShrink: 0 }} />
            )}
            {showTx && (
              <div style={{ flex: 1, minWidth: 0 }}>
                <div style={{ fontSize: '11px', color: 'var(--text-muted)', marginBottom: '2px', whiteSpace: 'nowrap' }}>Upload</div>
                <div style={{ fontSize: '16px', fontWeight: '800', color: 'var(--text-primary)', fontFamily: 'var(--font-mono)', whiteSpace: 'nowrap', overflow: 'hidden', textOverflow: 'ellipsis' }}>↑ {fmtBps(clusterStats.liveTxBps)}</div>
              </div>
            )}
          </div>
          <div style={{ position: 'absolute', bottom: 0, left: 0, right: 0, height: '3px', background: 'linear-gradient(90deg, var(--accent-primary) 0%, transparent 100%)', borderRadius: '0 0 16px 16px', opacity: 0.6 }} />
        </div>
      )}

      {/* Card 2: TOTAL DISK I/O */}
      {(showDiskRead || showDiskWrite) && (
        <div style={{
          display: 'flex',
          flexDirection: 'column',
          gap: '12px',
          padding: '16px 20px',
          borderRadius: '16px',
          background: 'var(--bg-secondary)',
          border: '1px solid var(--border-color)',
          boxShadow: 'var(--card-shadow)',
          position: 'relative',
          overflow: 'hidden'
        }}>
          <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
            <span style={{ fontSize: '11px', textTransform: 'uppercase', letterSpacing: '0.05em', color: 'var(--text-muted)', fontWeight: '700' }}>TOTAL DISK I/O</span>
            <span className="material-symbols-outlined" style={{ fontSize: '18px', color: '#fb923c', opacity: 0.8 }}>storage</span>
          </div>
          <div style={{ display: 'flex', gap: '16px', alignItems: 'center', flexWrap: 'nowrap' }}>
            {showDiskRead && (
              <div style={{ flex: 1, minWidth: 0 }}>
                <div style={{ fontSize: '11px', color: 'var(--text-muted)', marginBottom: '2px', whiteSpace: 'nowrap' }}>Read</div>
                <div style={{ fontSize: '16px', fontWeight: '800', color: 'var(--text-primary)', fontFamily: 'var(--font-mono)', whiteSpace: 'nowrap', overflow: 'hidden', textOverflow: 'ellipsis' }}>{fmtBps(clusterStats.liveDiskReadBps)}</div>
              </div>
            )}
            {showDiskRead && showDiskWrite && (
              <div style={{ width: '1px', height: '24px', background: 'var(--border-color)', flexShrink: 0 }} />
            )}
            {showDiskWrite && (
              <div style={{ flex: 1, minWidth: 0 }}>
                <div style={{ fontSize: '11px', color: 'var(--text-muted)', marginBottom: '2px', whiteSpace: 'nowrap' }}>Write</div>
                <div style={{ fontSize: '16px', fontWeight: '800', color: 'var(--text-primary)', fontFamily: 'var(--font-mono)', whiteSpace: 'nowrap', overflow: 'hidden', textOverflow: 'ellipsis' }}>{fmtBps(clusterStats.liveDiskWriteBps)}</div>
              </div>
            )}
          </div>
          <div style={{ position: 'absolute', bottom: 0, left: 0, right: 0, height: '3px', background: 'linear-gradient(90deg, #fb923c 0%, transparent 100%)', borderRadius: '0 0 16px 16px', opacity: 0.6 }} />
        </div>
      )}

      {/* Card 3: Total Traffic */}
      {(showRx || showTx) && (
        <div style={{
          display: 'flex',
          flexDirection: 'column',
          gap: '12px',
          padding: '16px 20px',
          borderRadius: '16px',
          background: 'var(--bg-secondary)',
          border: '1px solid var(--border-color)',
          boxShadow: 'var(--card-shadow)',
          position: 'relative',
          overflow: 'hidden'
        }}>
          <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
            <span style={{ fontSize: '11px', textTransform: 'uppercase', letterSpacing: '0.05em', color: 'var(--text-muted)', fontWeight: '700' }}>Total Traffic</span>
            <span className="material-symbols-outlined" style={{ fontSize: '18px', color: '#14b8a6', opacity: 0.8 }}>cloud_sync</span>
          </div>
          <div style={{ display: 'flex', gap: '16px', alignItems: 'center', flexWrap: 'nowrap' }}>
            {showRx && (
              <div style={{ flex: 1, minWidth: 0 }}>
                <div style={{ fontSize: '11px', color: 'var(--text-muted)', marginBottom: '2px', whiteSpace: 'nowrap' }}>Received</div>
                <div style={{ fontSize: '16px', fontWeight: '800', color: 'var(--text-primary)', fontFamily: 'var(--font-mono)', whiteSpace: 'nowrap', overflow: 'hidden', textOverflow: 'ellipsis' }}>↓ {fmtBytes(clusterStats.totalRxBytes)}</div>
              </div>
            )}
            {showRx && showTx && (
              <div style={{ width: '1px', height: '24px', background: 'var(--border-color)', flexShrink: 0 }} />
            )}
            {showTx && (
              <div style={{ flex: 1, minWidth: 0 }}>
                <div style={{ fontSize: '11px', color: 'var(--text-muted)', marginBottom: '2px', whiteSpace: 'nowrap' }}>Sent</div>
                <div style={{ fontSize: '16px', fontWeight: '800', color: 'var(--text-primary)', fontFamily: 'var(--font-mono)', whiteSpace: 'nowrap', overflow: 'hidden', textOverflow: 'ellipsis' }}>↑ {fmtBytes(clusterStats.totalTxBytes)}</div>
              </div>
            )}
          </div>
          <div style={{ position: 'absolute', bottom: 0, left: 0, right: 0, height: '3px', background: 'linear-gradient(90deg, #14b8a6 0%, transparent 100%)', borderRadius: '0 0 16px 16px', opacity: 0.6 }} />
        </div>
      )}

      {/* Card 4: Total Disk Read/Write */}
      {(showDiskRead || showDiskWrite) && (
        <div style={{
          display: 'flex',
          flexDirection: 'column',
          gap: '12px',
          padding: '16px 20px',
          borderRadius: '16px',
          background: 'var(--bg-secondary)',
          border: '1px solid var(--border-color)',
          boxShadow: 'var(--card-shadow)',
          position: 'relative',
          overflow: 'hidden'
        }}>
          <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
            <span style={{ fontSize: '11px', textTransform: 'uppercase', letterSpacing: '0.05em', color: 'var(--text-muted)', fontWeight: '700' }}>Total Disk Read/Write</span>
            <span className="material-symbols-outlined" style={{ fontSize: '18px', color: '#ef4444', opacity: 0.8 }}>database</span>
          </div>
          <div style={{ display: 'flex', gap: '16px', alignItems: 'center', flexWrap: 'nowrap' }}>
            {showDiskRead && (
              <div style={{ flex: 1, minWidth: 0 }}>
                <div style={{ fontSize: '11px', color: 'var(--text-muted)', marginBottom: '2px', whiteSpace: 'nowrap' }}>Read Vol</div>
                <div style={{ fontSize: '16px', fontWeight: '800', color: 'var(--text-primary)', fontFamily: 'var(--font-mono)', whiteSpace: 'nowrap', overflow: 'hidden', textOverflow: 'ellipsis' }}>{fmtBytes(clusterStats.totalDiskReadBytes)}</div>
              </div>
            )}
            {showDiskRead && showDiskWrite && (
              <div style={{ width: '1px', height: '24px', background: 'var(--border-color)', flexShrink: 0 }} />
            )}
            {showDiskWrite && (
              <div style={{ flex: 1, minWidth: 0 }}>
                <div style={{ fontSize: '11px', color: 'var(--text-muted)', marginBottom: '2px', whiteSpace: 'nowrap' }}>Write Vol</div>
                <div style={{ fontSize: '16px', fontWeight: '800', color: 'var(--text-primary)', fontFamily: 'var(--font-mono)', whiteSpace: 'nowrap', overflow: 'hidden', textOverflow: 'ellipsis' }}>{fmtBytes(clusterStats.totalDiskWriteBytes)}</div>
              </div>
            )}
          </div>
          <div style={{ position: 'absolute', bottom: 0, left: 0, right: 0, height: '3px', background: 'linear-gradient(90deg, #ef4444 0%, transparent 100%)', borderRadius: '0 0 16px 16px', opacity: 0.6 }} />
        </div>
      )}
    </div>
  );
};
