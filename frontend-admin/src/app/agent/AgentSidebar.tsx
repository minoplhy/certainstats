import { Link } from "react-router-dom";
import { UsageBar } from "../../lib/UsageBar";
import { Agent } from "../../types";

interface AgentSidebarProps {
  selectedId: string | null;
  provisioning: boolean;
  setShowTypeSelect: (val: boolean) => void;
  filter: string;
  setFilter: (val: string) => void;
  filteredAgents: Agent[];
  liveMetrics: Record<string, any>;
  fmtBytes: (b: number) => string;
  fmtBps: (v: number) => string;
  isExpanded: boolean;
  setIsExpanded: (val: boolean) => void;
}

export function AgentSidebar({
  selectedId,
  provisioning,
  setShowTypeSelect,
  filter,
  setFilter,
  filteredAgents,
  liveMetrics,
  fmtBytes,
  fmtBps,
  isExpanded,
  setIsExpanded
}: AgentSidebarProps) {
  return (
    <>
      {isExpanded && (
        <div
          className="sidebar-overlay mobile-only"
          style={{
            position: 'fixed',
            inset: 0,
            background: 'rgba(0,0,0,0.5)',
            backdropFilter: 'blur(4px)',
            zIndex: 250
          }}
          onClick={() => setIsExpanded(false)}
        />
      )}

      <aside
        className="sidebar-panel"
        style={{
          width: '280px',
          borderRight: '1px solid var(--border-color)',
          background: 'var(--bg-primary)',
          padding: '20px 0',
          display: isExpanded ? 'flex' : 'none',
          flexDirection: 'column',
          position: 'relative',
          zIndex: 300
        }}
      >
        <div style={{ padding: '0 20px', marginBottom: '20px', flexShrink: 0 }}>
          <div className="flex items-center justify-between" style={{ marginBottom: '16px' }}>
            <div className="flex items-center gap-2">
              <span className="text-xs font-bold text-muted uppercase tracking-wider">Agent</span>
            </div>
            <button onClick={() => setShowTypeSelect(true)} disabled={provisioning} style={{ color: 'var(--accent-primary)', background: 'none', border: 'none', cursor: 'pointer', display: 'flex', alignItems: 'center' }} title="Provision Node">
              <span className="material-symbols-outlined" style={{ fontSize: '20px' }}>add_circle</span>
            </button>
          </div>
          <input
            type="text"
            placeholder="Search nodes..."
            value={filter}
            onChange={(e) => setFilter(e.target.value)}
            className="input-field font-mono text-xs mobile-search-large"
            style={{ width: '100%', paddingLeft: '32px', backgroundImage: 'url("data:image/svg+xml,%3Csvg xmlns=\'http://www.w3.org/2000/svg\' height=\'24px\' viewBox=\'0 0 24 24\' width=\'24px\' fill=\'%235e606a\'%3E%3Cpath d=\'M0 0h24v24H0z\' fill=\'none\'/%3E%3Cpath d=\'M15.5 14h-.79l-.28-.27C15.41 12.59 16 11.11 16 9.5 16 5.91 13.09 3 9.5 3S3 5.91 3 9.5 5.91 16 9.5 16c1.61 0 3.09-.59 4.23-1.57l.27.28v.79l5 4.99L20.49 19l-4.99-5zm-6 0C7.01 14 5 11.99 5 9.5S7.01 5 9.5 5 14 7.01 14 9.5 11.99 14 9.5 14z\'/%3E%3C/svg%3E")', backgroundRepeat: 'no-repeat', backgroundPosition: '12px center', backgroundSize: '20px' }}
          />
        </div>
        <div style={{ flex: 1, overflowY: 'auto', padding: '0 12px' }}>
          {filteredAgents.length === 0 ? (
            <div style={{ padding: '32px 20px', textAlign: 'center', color: 'var(--text-muted)', fontSize: '13px' }}>No nodes found</div>
          ) : (
            filteredAgents.map((a) => {
              const snap = liveMetrics[a.agent_id];
              return (
                <Link
                  key={`${a.agent_id}-${snap?.Timestamp || 'initial'}`}
                  to={`/agent/${a.agent_id}`}
                  className={`mobile-list-item ${snap ? "pulse-flash" : ""}`}
                  style={{
                    width: '100%',
                    display: 'flex',
                    alignItems: 'center',
                    padding: '10px 12px',
                    borderRadius: '8px',
                    marginBottom: '4px',
                    background: a.agent_id === selectedId ? 'var(--glass-bg)' : 'transparent',
                    border: a.agent_id === selectedId ? '1px solid var(--border-color)' : '1px solid transparent',
                    color: a.agent_id === selectedId ? 'var(--text-primary)' : 'var(--text-secondary)',
                    transition: 'var(--transition-fast)',
                    textDecoration: 'none'
                  }}
                  onMouseOver={(e) => { if (a.agent_id !== selectedId) e.currentTarget.style.background = 'rgba(255,255,255,0.03)'; }}
                  onMouseOut={(e) => { if (a.agent_id !== selectedId) e.currentTarget.style.background = 'transparent'; }}
                >
                  <div className={`${a.is_online ? "status-dot-online" : "status-dot-offline"} mobile-status-dot`} style={{ marginRight: '12px', flexShrink: 0 }} />
                  <div style={{ flex: 1, textAlign: 'left', minWidth: 0, display: 'flex', flexDirection: 'column' }}>
                    <span style={{ fontSize: '13px', fontWeight: a.agent_id === selectedId ? '600' : '500', whiteSpace: 'nowrap', overflow: 'hidden', textOverflow: 'ellipsis' }}>
                      {a.nickname || a.agent_id}
                    </span>
                    {snap && a.is_online && (
                      <div style={{ display: 'flex', flexDirection: 'column', gap: '4px', marginTop: '8px' }}>
                        <UsageBar
                          label="CPU"
                          compact
                          segments={[
                            { label: 'Used', value: snap.CPUUsagePercent, color: '#3b82f6' },
                            { label: 'IO Wait', value: snap.CPUIOWaitPercent, color: '#fb923c' },
                            { label: 'Steal', value: snap.CPUStealPercent, color: '#ef4444' }
                          ]}
                        />
                        <UsageBar
                          label="RAM"
                          compact
                          segments={[
                            { label: 'Used', value: snap?.RAMUsagePercent || 0, color: '#14b8a6', displayValue: fmtBytes(snap?.RAMUsedBytes), totalDisplay: a.ram_size ? fmtBytes(a.ram_size) : undefined },
                            { label: 'Swap', value: snap?.RAMSwapUsagePercent || 0, color: '#4b5563', displayValue: fmtBytes(snap?.RAMSwapUsedBytes), totalDisplay: a.swap_size ? fmtBytes(a.swap_size) : undefined }
                          ]}
                        />
                        <UsageBar
                          label="Root Disk"
                          compact
                          segments={[
                            { label: 'Used', value: snap?.DiskUsagePercent || 0, color: '#a855f7', displayValue: fmtBytes(snap?.DiskUsedBytes), totalDisplay: a.disk_size ? fmtBytes(a.disk_size) : undefined }
                          ]}
                        />
                        <UsageBar
                          label="Network"
                          compact
                          segments={[
                            { label: 'RX', value: (snap?.RXBps + snap?.TXBps > 0) ? (snap.RXBps / (snap.RXBps + snap.TXBps)) * 100 : 0, color: '#1e40af', displayValue: fmtBps(snap?.RXBps || 0) },
                            { label: 'TX', value: (snap?.RXBps + snap?.TXBps > 0) ? (snap.TXBps / (snap.TXBps + snap.TXBps)) * 100 : 0, color: '#7e22ce', displayValue: fmtBps(snap?.TXBps || 0) }
                          ]}
                        />
                      </div>
                    )}
                  </div>
                </Link>
              );
            })
          )}
        </div>
      </aside>
    </>
  );
}
