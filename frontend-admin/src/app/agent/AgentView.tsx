import React, { useState, useEffect, useRef, FC } from "react";
import { createPortal } from "react-dom";
import { useNavigate, Link } from "react-router-dom";
import { getPanelPath } from "../../lib/env";
import { Agent, AgentSnapshot } from "../../types";
import { UsageBar } from "../../lib/UsageBar";
import { fmtUptime } from "../../lib/utils";

interface AgentViewProps {
  filteredAgents: Agent[];
  liveMetrics: Record<string, AgentSnapshot>;
  viewMode: 'grid' | 'list';
  setViewMode: (m: 'grid' | 'list') => void;
  gridDensity: 'detailed' | 'simplified';
  setGridDensity: (d: 'detailed' | 'simplified') => void;
  setShowTypeSelect: (s: boolean) => void;
  provisioning: boolean;
  filter: string;
  setFilter: (f: string) => void;
  fmtBytes: (b: number) => string;
  fmtBps: (v: number) => string;
  onInstall: (id: string) => void;
  onRename: (a: Agent) => void;
  onRevoke: (a: Agent) => void;
}

const ActionMenu: FC<{
  agent: Agent;
  onInstall: (id: string) => void;
  onRename: (a: Agent) => void;
  onRevoke: (a: Agent) => void;
}> = ({ agent, onInstall, onRename, onRevoke }) => {
  const [isOpen, setIsOpen] = useState(false);
  const [coords, setCoords] = useState({ top: 0, left: 0, openUp: false });
  const menuRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    if (!isOpen) return;
    const clickOutside = (e: MouseEvent) => {
      if (menuRef.current && !menuRef.current.contains(e.target as Node)) {
        setIsOpen(false);
      }
    };
    const handleScroll = () => setIsOpen(false);

    document.addEventListener('mousedown', clickOutside);
    window.addEventListener('scroll', handleScroll, true);
    return () => {
      document.removeEventListener('mousedown', clickOutside);
      window.removeEventListener('scroll', handleScroll, true);
    };
  }, [isOpen]);

  const handleToggle = (e: React.MouseEvent) => {
    e.preventDefault();
    e.stopPropagation();
    if (!isOpen) {
      const rect = e.currentTarget.getBoundingClientRect();
      const menuHeight = 220; // Estimated height for node management menu
      const spaceBelow = window.innerHeight - rect.bottom;
      const openUp = spaceBelow < menuHeight && rect.top > menuHeight;

      setCoords({
        top: openUp ? rect.top - 8 : rect.bottom + 8,
        left: Math.max(12, Math.min(window.innerWidth - 252, rect.right - 240)), // Keep 12px safe horizontal screen margins
        openUp
      });
    }
    setIsOpen(!isOpen);
  };

  return (
    <div style={{ display: 'flex' }}>
      <style>{`
        @keyframes menuExpand {
          from {
            opacity: 0;
            transform: scale(0.95) translateY(-4px);
          }
          to {
            opacity: 1;
            transform: scale(1) translateY(0);
          }
        }

        .action-menu-popover {
          background: var(--bg-secondary) !important;
          border: 1px solid var(--border-color) !important;
          border-radius: 12px !important;
          box-shadow: 0 16px 36px rgba(0, 0, 0, 0.3) !important;
          display: flex;
          flex-direction: column;
          gap: 2px;
          padding: 6px !important;
          animation: menuExpand 0.12s cubic-bezier(0.16, 1, 0.3, 1) forwards;
          transform-origin: top right;
        }
        .action-menu-header {
          padding: 8px 12px 6px;
        }
        .action-menu-header-text {
          font-size: 11px;
          font-weight: 700;
          color: var(--text-muted);
          text-transform: uppercase;
          letter-spacing: 0.05em;
        }
        .action-menu-item {
          display: flex;
          align-items: center;
          gap: 10px;
          width: 100%;
          text-align: left;
          padding: 10px 12px;
          border-radius: 8px;
          font-size: 13px;
          font-weight: 500;
          color: var(--text-secondary);
          background: transparent;
          border: none;
          cursor: pointer;
          transition: background 0.15s ease, color 0.15s ease;
        }
        .action-menu-item:hover {
          color: var(--text-primary);
          background: var(--bar-bg);
        }
        .action-menu-item .material-symbols-outlined {
          font-size: 18px;
          color: var(--text-muted);
          transition: color 0.15s ease;
        }
        .action-menu-item:hover .material-symbols-outlined {
          color: var(--text-primary);
        }
        .action-menu-divider {
          height: 1px;
          background: var(--border-color);
          margin: 6px 0;
        }
        .action-menu-item-danger {
          color: var(--status-offline);
        }
        .action-menu-item-danger:hover {
          color: var(--status-offline);
          background: rgba(239, 68, 68, 0.08);
        }
        .action-menu-item-danger .material-symbols-outlined {
          color: var(--status-offline);
          opacity: 0.8;
        }
        .action-menu-item-danger:hover .material-symbols-outlined {
          opacity: 1;
        }
      `}</style>

      <button
        onClick={handleToggle}
        className="text-muted hover:text-primary transition-colors"
        style={{ display: 'flex', padding: '6px', background: 'none', border: 'none', cursor: 'pointer' }}
      >
        <span className="material-symbols-outlined" style={{ fontSize: '24px' }}>more_vert</span>
      </button>

      {isOpen && createPortal(
        <div
          ref={menuRef}
          className="action-menu-popover"
          style={{
            position: 'fixed',
            top: coords.openUp ? 'auto' : `${coords.top}px`,
            bottom: coords.openUp ? `${window.innerHeight - coords.top}px` : 'auto',
            left: `${coords.left}px`,
            zIndex: 9999,
            width: '240px'
          }}
          onClick={e => e.stopPropagation()}
        >
          <div className="action-menu-header">
            <div className="action-menu-header-text">
              Actions
            </div>
          </div>

          <button
            onClick={() => { onRename(agent); setIsOpen(false); }}
            className="action-menu-item"
          >
            <span className="material-symbols-outlined">edit</span>
            <span>Rename</span>
          </button>

          <button
            onClick={() => { onInstall(agent.agent_id); setIsOpen(false); }}
            className="action-menu-item"
          >
            <span className="material-symbols-outlined">terminal</span>
            <span>Reinstall</span>
          </button>

          <div className="action-menu-divider" />

          <button
            onClick={() => { onRevoke(agent); setIsOpen(false); }}
            className="action-menu-item action-menu-item-danger"
          >
            <span className="material-symbols-outlined">delete_forever</span>
            <span style={{ fontWeight: '600' }}>Terminate</span>
          </button>
        </div>,
        document.body
      )}
    </div>
  );
};

export const AgentView: FC<AgentViewProps> = ({
  filteredAgents,
  liveMetrics,
  viewMode,
  setViewMode,
  gridDensity,
  setGridDensity,
  setShowTypeSelect,
  provisioning,
  filter,
  setFilter,
  fmtBytes,
  fmtBps,
  onInstall,
  onRename,
  onRevoke
}) => {
  const navigate = useNavigate();
  const base = getPanelPath().replace(/\/$/, "");

  return (
    <div style={{ maxWidth: '1200px', margin: '0 auto', padding: '0 32px' }}>
      <div style={{ display: 'flex', flexDirection: 'column', gap: '32px' }}>
        <div style={{
          display: 'flex',
          flexDirection: 'column',
          gap: '24px',
          marginBottom: '16px'
        }}>
          {/* Main Title Row */}
          <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'flex-end', flexWrap: 'wrap', gap: '20px' }}>
            <div className="flex items-center gap-4">
              <h1 className="font-display" style={{ fontSize: '40px', fontWeight: '900', letterSpacing: '-0.04em', color: 'var(--text-primary)' }}>Agent Hub</h1>
              <div style={{
                background: 'var(--accent-glow)',
                color: 'var(--accent-primary)',
                padding: '4px 12px',
                borderRadius: '20px',
                fontSize: '14px',
                fontWeight: '800',
                marginTop: '4px'
              }}>
                {filteredAgents.length} Nodes
              </div>
            </div>

            <button
              onClick={(e) => {
                e.preventDefault();
                e.stopPropagation();
                e.currentTarget.blur();
                setShowTypeSelect(true);
              }}
              disabled={provisioning}
              className="btn-primary"
              style={{
                padding: '12px 28px',
                fontSize: '14px',
                borderRadius: '14px',
                fontWeight: '800',
                display: 'flex',
                alignItems: 'center',
                gap: '8px'
              }}
            >
              <span className="material-symbols-outlined" style={{ fontSize: '20px' }}>add_circle</span>
              Add Agent
            </button>
          </div>

          {/* Unified Toolbar Row */}
          <div style={{
            display: 'flex',
            alignItems: 'center',
            gap: '12px',
            flexWrap: 'wrap',
            background: 'var(--bg-secondary)',
            padding: '8px',
            borderRadius: '16px',
            border: '1px solid var(--border-color)',
            boxShadow: '0 4px 12px rgba(0,0,0,0.1)'
          }}>
            <div style={{ position: 'relative', flex: '1', minWidth: '240px' }}>
              <span className="material-symbols-outlined" style={{ position: 'absolute', left: '16px', top: '50%', transform: 'translateY(-50%)', fontSize: '20px', color: 'var(--text-muted)', pointerEvents: 'none' }}>search</span>
              <input
                type="text"
                placeholder="Search by nickname, agent or OS..."
                value={filter}
                onChange={(e) => setFilter(e.target.value)}
                style={{
                  width: '100%',
                  background: 'transparent',
                  border: 'none',
                  padding: '10px 16px 10px 48px',
                  fontSize: '14px',
                  color: 'var(--text-primary)',
                  outline: 'none'
                }}
              />
            </div>

            <div style={{ height: '24px', width: '1px', background: 'var(--border-color)', margin: '0 4px' }} className="mobile-hide" />

            <div style={{ display: 'flex', gap: '6px', alignItems: 'center' }}>
              {viewMode === 'grid' && (
                <div style={{ display: 'flex', gap: '2px', background: 'var(--bg-primary)', padding: '3px', borderRadius: '10px', border: '1px solid var(--border-color)' }}>
                  <button
                    onClick={() => setGridDensity("detailed")}
                    title="Detailed View"
                    style={{
                      display: 'flex', padding: '6px 10px', borderRadius: '8px',
                      background: gridDensity === "detailed" ? 'var(--accent-primary)' : 'transparent',
                      color: gridDensity === "detailed" ? '#fff' : 'var(--text-muted)',
                      transition: 'var(--transition-fast)'
                    }}
                  >
                    <span className="material-symbols-outlined" style={{ fontSize: '18px' }}>density_medium</span>
                  </button>
                  <button
                    onClick={() => setGridDensity("simplified")}
                    title="Simplified View"
                    style={{
                      display: 'flex', padding: '6px 10px', borderRadius: '8px',
                      background: gridDensity === "simplified" ? 'var(--accent-primary)' : 'transparent',
                      color: gridDensity === "simplified" ? '#fff' : 'var(--text-muted)',
                      transition: 'var(--transition-fast)'
                    }}
                  >
                    <span className="material-symbols-outlined" style={{ fontSize: '18px' }}>density_small</span>
                  </button>
                </div>
              )}

              <div style={{ display: 'flex', gap: '2px', background: 'var(--bg-primary)', padding: '3px', borderRadius: '10px', border: '1px solid var(--border-color)' }}>
                <button
                  onClick={() => setViewMode("grid")}
                  title="Grid"
                  style={{
                    display: 'flex', padding: '6px 10px', borderRadius: '8px',
                    background: viewMode === "grid" ? 'var(--bg-secondary)' : 'transparent',
                    color: viewMode === "grid" ? 'var(--accent-primary)' : 'var(--text-muted)',
                    transition: 'var(--transition-fast)',
                    boxShadow: viewMode === "grid" ? '0 2px 8px rgba(0,0,0,0.2)' : 'none'
                  }}
                >
                  <span className="material-symbols-outlined" style={{ fontSize: '18px' }}>grid_view</span>
                </button>
                <button
                  onClick={() => setViewMode("list")}
                  title="List"
                  style={{
                    display: 'flex', padding: '6px 10px', borderRadius: '8px',
                    background: viewMode === "list" ? 'var(--bg-secondary)' : 'transparent',
                    color: viewMode === "list" ? 'var(--accent-primary)' : 'var(--text-muted)',
                    transition: 'var(--transition-fast)',
                    boxShadow: viewMode === "list" ? '0 2px 8px rgba(0,0,0,0.2)' : 'none'
                  }}
                >
                  <span className="material-symbols-outlined" style={{ fontSize: '18px' }}>view_list</span>
                </button>
              </div>
            </div>
          </div>
        </div>

        {filteredAgents.length === 0 ? (
          <div style={{ textAlign: 'center', padding: '80px 20px', background: 'var(--bg-secondary)', borderRadius: '16px', border: '1px dashed var(--border-color)' }}>
            <div style={{ marginBottom: '24px', opacity: 0.5 }}>
              <span className="material-symbols-outlined" style={{ fontSize: '64px', color: 'var(--text-muted)' }}>cloud_off</span>
            </div>
            <h2 style={{ fontSize: '20px', fontWeight: '700', marginBottom: '8px' }}>No Nodes Found</h2>
            <p style={{ color: 'var(--text-muted)', marginBottom: '32px' }}>Get started by adding your first monitoring agent.</p>
            <button
              onClick={(e) => {
                e.preventDefault();
                e.stopPropagation();
                e.currentTarget.blur();
                setShowTypeSelect(true);
              }}
              className="btn-primary"
              style={{ display: 'inline-flex', alignItems: 'center', gap: '8px', padding: '12px 24px' }}
            >
              <span className="material-symbols-outlined" style={{ fontSize: '32px', color: 'var(--accent-primary)' }}>add_circle</span>
              <span style={{ fontSize: '14px', fontWeight: '700', color: 'var(--text-secondary)' }}>Add New Node</span>
            </button>
          </div>
        ) : (
          viewMode === "grid" ? (
            <div style={{
              display: 'grid',
              gridTemplateColumns: gridDensity === 'detailed'
                ? 'repeat(auto-fill, minmax(min(100%, 360px), 1fr))'
                : 'repeat(auto-fill, minmax(min(100%, 320px), 1fr))',
              gap: '24px'
            }}>
              {filteredAgents.map(a => {
                const snap = liveMetrics[a.agent_id];
                const isDetailed = gridDensity === 'detailed';

                return (
                  <Link
                    key={a.agent_id}
                    to={`/agent/${a.agent_id}`}
                    className={`animate-fade-in ${snap ? 'pulse-flash' : ''}`}
                    style={{
                      display: 'flex',
                      flexDirection: 'column',
                      textAlign: 'left',
                      padding: isDetailed ? '28px' : '18px',
                      cursor: 'pointer',
                      transition: 'all 0.3s cubic-bezier(0.4, 0, 0.2, 1)',
                      background: 'var(--bg-secondary)',
                      border: '1px solid var(--border-color)',
                      borderRadius: '24px',
                      height: '100%',
                      boxShadow: 'var(--card-shadow)',
                      position: 'relative',
                      overflow: 'hidden',
                      textDecoration: 'none'
                    }}
                    onMouseOver={(e) => {
                      e.currentTarget.style.borderColor = 'var(--accent-primary)';
                      e.currentTarget.style.transform = 'translateY(-4px)';
                      e.currentTarget.style.boxShadow = '0 12px 32px rgba(0,0,0,0.15)';
                    }}
                    onMouseOut={(e) => {
                      e.currentTarget.style.borderColor = 'var(--border-color)';
                      e.currentTarget.style.transform = 'translateY(0)';
                      e.currentTarget.style.boxShadow = 'var(--card-shadow)';
                    }}
                  >
                    <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', marginBottom: isDetailed ? '20px' : '12px' }}>
                      <div style={{ display: 'flex', alignItems: 'center', gap: '12px', minWidth: 0 }}>
                        <div style={{ position: 'relative', display: 'flex', flexShrink: 0 }}>
                          <div className={a.is_online ? "status-dot-online" : "status-dot-offline"} style={{ width: '10px', height: '10px' }} />
                          {a.is_online && <div style={{ position: 'absolute', top: 0, left: 0, right: 0, bottom: 0, borderRadius: '50%', background: 'var(--status-online)', filter: 'blur(4px)', opacity: 0.6 }} />}
                        </div>
                        <h3 style={{
                          fontSize: '18px',
                          fontWeight: '800',
                          color: 'var(--text-primary)',
                          whiteSpace: 'nowrap',
                          overflow: 'hidden',
                          textOverflow: 'ellipsis',
                          letterSpacing: '-0.01em',
                          fontFamily: 'var(--font-display)'
                        }}>
                          {a.nickname || a.agent_id}
                        </h3>
                      </div>
                      <div className="flex items-center gap-3">
                        <ActionMenu agent={a} onInstall={onInstall} onRename={onRename} onRevoke={onRevoke} />
                      </div>
                    </div>

                    {isDetailed && (
                      <div style={{ display: 'flex', flexDirection: 'column', gap: '6px', marginBottom: '8px', fontSize: '12px', color: 'var(--text-primary)', opacity: 0.8, letterSpacing: '0.01em' }}>
                        {a.cpu_model && a.cpu_model !== "Waiting for data..." && (
                          <div style={{ whiteSpace: 'nowrap', overflow: 'hidden', textOverflow: 'ellipsis', fontWeight: '700' }}>{a.cpu_model}</div>
                        )}
                        {(a.linux_version && a.linux_version !== "Pending connection...") && (
                          <div style={{ fontFamily: 'var(--font-mono)', fontSize: '10px', opacity: 0.9, whiteSpace: 'nowrap', overflow: 'hidden', textOverflow: 'ellipsis' }}>{a.linux_version}</div>
                        )}
                      </div>
                    )}

                    <div style={{ display: 'flex', flexDirection: 'column', gap: isDetailed ? '14px' : '6px' }}>
                      {snap && a.is_online ? (
                        <>
                          <UsageBar label="CPU" compact={!isDetailed} segments={[
                            { label: 'Used', value: snap.CPUUsagePercent, color: '#3b82f6' },
                            { label: 'IO Wait', value: snap.CPUIOWaitPercent, color: '#fb923c' },
                            { label: 'Steal', value: snap.CPUStealPercent, color: '#ef4444' }
                          ]} />
                          <UsageBar label="RAM" compact={!isDetailed} segments={[
                            { label: 'Used', value: snap?.RAMUsagePercent || 0, color: '#14b8a6', displayValue: fmtBytes(snap?.RAMUsedBytes), totalDisplay: a.ram_size ? fmtBytes(a.ram_size) : undefined },
                            { label: 'Swap', value: snap?.RAMSwapUsagePercent || 0, color: '#4b5563', displayValue: fmtBytes(snap?.RAMSwapUsedBytes), totalDisplay: a.swap_size ? fmtBytes(a.swap_size) : undefined }
                          ]} />
                          {snap.Disks && snap.Disks.length > 0 ? snap.Disks.map((d: any, idx: number) => {
                            const pct = d.total_bytes > 0 ? (d.used_bytes / d.total_bytes) * 100 : 0;
                            const path = d.path || 'Unknown';
                            return (
                              <UsageBar
                                key={idx}
                                label={path === '/' ? 'DISK' : `DISK (${path})`}
                                compact={!isDetailed}
                                segments={[
                                  { label: 'Used', value: pct, color: '#a855f7', displayValue: fmtBytes(d.used_bytes), totalDisplay: d.total_bytes ? fmtBytes(d.total_bytes) : undefined }
                                ]}
                              />
                            );
                          }) : (
                            <UsageBar
                              label="DISK"
                              compact={!isDetailed}
                              segments={[
                                { label: 'Used', value: snap?.DiskUsagePercent || 0, color: '#a855f7', displayValue: fmtBytes(snap?.DiskUsedBytes), totalDisplay: a.disk_size ? fmtBytes(a.disk_size) : undefined }
                              ]}
                            />
                          )}
                          <UsageBar label="Network" compact={!isDetailed} unit="" segments={[
                            { label: 'RX', value: (snap?.RXBps + snap?.TXBps > 0) ? (snap.RXBps / (snap.RXBps + snap.TXBps)) * 100 : 0, color: '#1e40af', displayValue: fmtBps(snap?.RXBps || 0) },
                            { label: 'TX', value: (snap?.RXBps + snap?.TXBps > 0) ? (snap.TXBps / (snap.RXBps + snap.TXBps)) * 100 : 0, color: '#7e22ce', displayValue: fmtBps(snap?.TXBps || 0) }
                          ]} />
                          <UsageBar
                            label="DISK IO"
                            compact={!isDetailed}
                            unit=""
                            segments={[
                              { label: 'Read', value: (snap?.DiskReadBps + snap?.DiskWriteBps > 0) ? (snap.DiskReadBps / (snap.DiskReadBps + snap.DiskWriteBps)) * 100 : 0, color: '#f59e0b', displayValue: fmtBps(snap?.DiskReadBps || 0) },
                              { label: 'Write', value: (snap?.DiskReadBps + snap?.DiskWriteBps > 0) ? (snap.DiskWriteBps / (snap.DiskReadBps + snap.DiskWriteBps)) * 100 : 0, color: '#ef4444', displayValue: fmtBps(snap?.DiskWriteBps || 0) }
                            ]}
                          />
                        </>
                      ) : (
                        <div style={{ display: 'flex', flexDirection: 'column', gap: isDetailed ? '16px' : '8px', padding: isDetailed ? '10px 0' : '4px 0' }}>
                          <div style={{ height: isDetailed ? '32px' : '4px', background: 'var(--bar-bg)', borderRadius: isDetailed ? '6px' : '2px', border: isDetailed ? '1px dashed var(--border-color)' : 'none' }} />
                          <div style={{ height: isDetailed ? '32px' : '4px', background: 'var(--bar-bg)', borderRadius: isDetailed ? '6px' : '2px', border: isDetailed ? '1px dashed var(--border-color)' : 'none' }} />
                          <div style={{ height: isDetailed ? '32px' : '4px', background: 'var(--bar-bg)', borderRadius: isDetailed ? '6px' : '2px', border: isDetailed ? '1px dashed var(--border-color)' : 'none' }} />
                        </div>
                      )}
                    </div>

                    <div style={{
                      display: 'flex',
                      alignItems: 'center',
                      justifyContent: 'space-between',
                      fontSize: '11px',
                      color: 'var(--text-muted)',
                      fontFamily: 'var(--font-mono)',
                      marginTop: 'auto',
                      paddingTop: '16px',
                      borderTop: '1px solid var(--border-color)'
                    }}>
                      {a.uptime ? (
                        <div style={{ display: 'flex', alignItems: 'center', gap: '6px' }}>
                          <span className="material-symbols-outlined" style={{ fontSize: '16px' }}>schedule</span>
                          {fmtUptime(a.uptime)}
                        </div>
                      ) : <div />}
                      <div style={{ display: 'flex', alignItems: 'center', gap: '6px' }}>
                        <span className="material-symbols-outlined" style={{ fontSize: '16px' }}>dns</span>
                        <span style={{ fontWeight: '600', textTransform: 'uppercase' }}>{a.agent_type}</span>
                      </div>
                    </div>
                  </Link>
                );
              })}
            </div>
          ) : (
            <div className="card" style={{ padding: '0', overflowX: 'auto', border: '1px solid var(--border-color)', borderRadius: '12px', background: 'var(--bg-secondary)' }}>
              <table style={{ width: '100%', minWidth: '700px', borderCollapse: 'collapse', textAlign: 'left' }}>
                <thead>
                  <tr style={{ background: 'var(--bar-bg)', borderBottom: '1px solid var(--border-color)' }}>
                    <th style={{ padding: '16px 24px', fontSize: '11px', textTransform: 'uppercase', letterSpacing: '0.05em', color: 'var(--text-muted)' }}>Status</th>
                    <th style={{ padding: '16px 24px', fontSize: '11px', textTransform: 'uppercase', letterSpacing: '0.05em', color: 'var(--text-muted)' }}>Node</th>
                    <th style={{ padding: '16px 24px', fontSize: '11px', textTransform: 'uppercase', letterSpacing: '0.05em', color: 'var(--text-muted)' }}>Current Load</th>
                    <th style={{ padding: '16px 24px', fontSize: '11px', textTransform: 'uppercase', letterSpacing: '0.05em', color: 'var(--text-muted)' }}>CPU</th>
                    <th style={{ padding: '16px 24px', fontSize: '11px', textTransform: 'uppercase', letterSpacing: '0.05em', color: 'var(--text-muted)' }}>OS</th>
                    <th style={{ padding: '16px 24px', fontSize: '11px', textTransform: 'uppercase', letterSpacing: '0.05em', color: 'var(--text-muted)' }}>RAM</th>
                    <th style={{ padding: '16px 24px', fontSize: '11px', textTransform: 'uppercase', letterSpacing: '0.05em', color: 'var(--text-muted)' }}>Disk</th>
                    <th style={{ padding: '16px 24px', fontSize: '11px', textTransform: 'uppercase', letterSpacing: '0.05em', color: 'var(--text-muted)' }}>Type</th>
                    <th style={{ padding: '16px 24px', fontSize: '11px', textTransform: 'uppercase', letterSpacing: '0.05em', color: 'var(--text-muted)', textAlign: 'right' }}>Actions</th>
                  </tr>
                </thead>
                <tbody>
                  {filteredAgents.map(a => {
                    const snap = liveMetrics[a.agent_id];
                    return (
                      <tr
                        key={`list-${a.agent_id}-${snap?.Timestamp || 'initial'}`}
                        onClick={(e) => {
                          if (e.ctrlKey || e.metaKey) {
                            window.open(`${window.location.origin}${base}/agent/${a.agent_id}`, '_blank');
                          } else {
                            navigate(`/agent/${a.agent_id}`);
                          }
                        }}
                        onAuxClick={(e) => {
                          if (e.button === 1) { // Middle click
                            window.open(`${window.location.origin}${base}/agent/${a.agent_id}`, '_blank');
                          }
                        }}
                        className={snap ? "pulse-flash" : ""}
                        style={{ borderBottom: '1px solid var(--border-color)', cursor: 'pointer', transition: 'background 0.2s ease' }}
                        onMouseOver={(e) => e.currentTarget.style.background = 'var(--bar-bg)'}
                        onMouseOut={(e) => e.currentTarget.style.background = 'transparent'}
                      >
                        <td style={{ padding: '16px 24px', verticalAlign: 'middle' }}>
                          <div className={a.is_online ? "status-dot-online" : "status-dot-offline"} />
                        </td>
                        <td style={{ padding: '16px 24px', fontSize: '14px', fontWeight: '600', color: 'var(--text-primary)', verticalAlign: 'middle' }}>{a.nickname || a.agent_id}</td>
                        <td style={{ padding: '16px 24px', minWidth: '220px', verticalAlign: 'middle' }}>
                          {snap && a.is_online ? (
                            <div style={{ display: 'flex', flexDirection: 'column', gap: '8px' }}>
                              <UsageBar label="CPU" compact segments={[
                                { label: 'Used', value: snap.CPUUsagePercent, color: '#3b82f6' },
                                { label: 'IO Wait', value: snap.CPUIOWaitPercent, color: '#fb923c' },
                                { label: 'Steal', value: snap.CPUStealPercent, color: '#ef4444' }
                              ]} />
                              <UsageBar label="RAM" compact segments={[
                                { label: 'Used', value: snap?.RAMUsagePercent || 0, color: '#14b8a6', displayValue: fmtBytes(snap?.RAMUsedBytes), totalDisplay: a.ram_size ? fmtBytes(a.ram_size) : undefined },
                                { label: 'Swap', value: snap?.RAMSwapUsagePercent || 0, color: '#4b5563', displayValue: fmtBytes(snap?.RAMSwapUsedBytes), totalDisplay: a.swap_size ? fmtBytes(a.swap_size) : undefined }
                              ]} />
                              <UsageBar
                                label="DISK"
                                compact
                                segments={[
                                  { label: 'Used', value: snap?.DiskUsagePercent || 0, color: '#a855f7', displayValue: fmtBytes(snap?.DiskUsedBytes), totalDisplay: a.disk_size ? fmtBytes(a.disk_size) : undefined }
                                ]}
                              />
                              <UsageBar label="Network" compact unit="" segments={[
                                { label: 'RX', value: (snap?.RXBps + snap?.TXBps > 0) ? (snap.RXBps / (snap.RXBps + snap.TXBps)) * 100 : 0, color: '#1e40af', displayValue: fmtBps(snap?.RXBps || 0) },
                                { label: 'TX', value: (snap?.RXBps + snap?.TXBps > 0) ? (snap.TXBps / (snap.RXBps + snap.TXBps)) * 100 : 0, color: '#7e22ce', displayValue: fmtBps(snap?.TXBps || 0) }
                              ]} />
                              <UsageBar
                                label="DISK IO"
                                compact
                                unit=""
                                segments={[
                                  { label: 'Read', value: (snap?.DiskReadBps + snap?.DiskWriteBps > 0) ? (snap.DiskReadBps / (snap.DiskReadBps + snap.DiskWriteBps)) * 100 : 0, color: '#f59e0b', displayValue: fmtBps(snap?.DiskReadBps || 0) },
                                  { label: 'Write', value: (snap?.DiskReadBps + snap?.DiskWriteBps > 0) ? (snap.DiskWriteBps / (snap.DiskReadBps + snap.DiskWriteBps)) * 100 : 0, color: '#ef4444', displayValue: fmtBps(snap?.DiskWriteBps || 0) }
                                ]}
                              />
                            </div>
                          ) : <span style={{ color: 'var(--text-muted)', fontSize: '12px' }}>-</span>}
                        </td>
                        <td style={{ padding: '16px 24px', verticalAlign: 'middle' }}>
                          {a.cpu_model ? (
                            <div style={{ display: 'flex', flexDirection: 'column', gap: '2px' }}>
                              <span style={{ fontSize: '13px', fontWeight: '600', color: 'var(--text-primary)', whiteSpace: 'nowrap', overflow: 'hidden', textOverflow: 'ellipsis', maxWidth: '180px' }}>{a.cpu_model}</span>
                              <span style={{ fontSize: '11px', fontWeight: '700', color: 'var(--text-primary)', opacity: 0.8 }}>{a.cpu_cores || "?"} Cores</span>
                            </div>
                          ) : <span style={{ color: 'var(--text-muted)' }}>-</span>}
                        </td>
                        <td style={{ padding: '16px 24px', verticalAlign: 'middle' }}>
                          {a.linux_version ? (
                            <div style={{ display: 'flex', alignItems: 'center', gap: '6px', fontSize: '13px', color: 'var(--text-secondary)' }}>
                              <span className="material-symbols-outlined" style={{ fontSize: '16px', color: 'var(--text-muted)' }}>terminal</span>
                              <span>{a.linux_version.split(' ')[0] || "Linux"}</span>
                            </div>
                          ) : <span style={{ color: 'var(--text-muted)' }}>-</span>}
                        </td>
                        <td style={{ padding: '16px 24px', fontSize: '13px', color: 'var(--text-secondary)', verticalAlign: 'middle' }}>{a.ram_size ? fmtBytes(a.ram_size) : "-"}</td>
                        <td style={{ padding: '16px 24px', fontSize: '13px', color: 'var(--text-secondary)', verticalAlign: 'middle' }}>{a.disk_size ? fmtBytes(a.disk_size) : "-"}</td>
                        <td style={{ padding: '16px 24px', verticalAlign: 'middle' }}>
                          <span style={{ fontSize: '11px', fontWeight: '800', color: 'var(--text-muted)', textTransform: 'uppercase', letterSpacing: '0.05em' }}>{a.agent_type}</span>
                        </td>
                        <td style={{ padding: '16px 24px', verticalAlign: 'middle', textAlign: 'right' }}>
                          <ActionMenu agent={a} onInstall={onInstall} onRename={onRename} onRevoke={onRevoke} />
                        </td>
                      </tr>
                    );
                  })}
                </tbody>
              </table>
            </div>
          )
        )}
      </div>
    </div>
  );
};
