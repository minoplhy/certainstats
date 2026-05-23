import { useEffect, useState } from "react";
import { useNavigate, Link } from "react-router-dom";
import { fetchAPI } from "../../lib/api";
import { Alert, AlertHistory } from "../../types";
import DeleteConfirmModal from "../common/DeleteConfirmModal";

function isUnauthorized(err: unknown): boolean {
  if (typeof err === "object" && err !== null && "status" in err && (err as any).status === 401) {
    return true;
  }
  return false;
}

const TRIGGER_LABELS: Record<string, string> = {
  agent_down: "Node Offline",
  cpu_usage: "CPU Usage",
  cpu_iowait: "CPU IO Wait",
  cpu_steal: "CPU Steal",
  ram_usage: "RAM Usage",
  swap_usage: "Swap Usage",
  disk_usage: "Disk Usage",
  net_rx: "Network In",
  net_tx: "Network Out",
  disk_read: "Disk Read",
  disk_write: "Disk Write",
};

const TRIGGER_ICONS: Record<string, string> = {
  agent_down: "cloud_off",
  cpu_usage: "memory",
  cpu_iowait: "hourglass_empty",
  cpu_steal: "gavel",
  ram_usage: "memory_alt",
  swap_usage: "swap_horiz",
  disk_usage: "hard_drive",
  net_rx: "download",
  net_tx: "upload",
  disk_read: "read_more",
  disk_write: "save",
};

const getSuffix = (type: string) => {
  if (type === "agent_down") return "";
  if (["net_rx", "net_tx", "disk_read", "disk_write"].includes(type)) return " KB/s";
  return "%";
};

const getAlertConditionString = (trigger: any) => {
  if (trigger.type === "agent_down") {
    return TRIGGER_LABELS[trigger.type] || "Node Offline";
  }
  return `${TRIGGER_LABELS[trigger.type] || trigger.type} ${trigger.operator} ${trigger.threshold}${getSuffix(trigger.type)}`;
};

export default function AlertsPanel({ onSelectNode }: { onSelectNode?: (id: string) => void }) {
  const navigate = useNavigate();
  const [alerts, setAlerts] = useState<Alert[]>([]);
  const [history, setHistory] = useState<AlertHistory[]>([]);
  const [total, setTotal] = useState(0);
  const [page, setPage] = useState(1);
  const [limit, setLimit] = useState(15);
  const [totalPages, setTotalPages] = useState(0);
  const [isHistoryLoading, setIsHistoryLoading] = useState(false);
  const [isLoading, setIsLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [deletingId, setDeletingId] = useState<string | null>(null);
  const [deleteConfirmAlertId, setDeleteConfirmAlertId] = useState<string | null>(null);

  // Search & Filter State
  const [searchInputValue, setSearchInputValue] = useState("");
  const [historySearch, setHistorySearch] = useState("");
  const [historyStatus, setHistoryStatus] = useState("all");

  // Debounce search input
  useEffect(() => {
    const timer = setTimeout(() => {
      setHistorySearch(searchInputValue);
    }, 350);
    return () => clearTimeout(timer);
  }, [searchInputValue]);

  const fetchHistory = async (targetPage: number, targetLimit: number, query: string, status: string) => {
    setIsHistoryLoading(true);
    try {
      const res = await fetchAPI<{ data: AlertHistory[], total: number, page: number, limit: number, total_pages: number }>(
        `/api/alerts/history?page=${targetPage}&limit=${targetLimit}&q=${encodeURIComponent(query)}&status=${status}`
      );
      setHistory(res.data || []);
      setTotal(res.total || 0);
      setPage(res.page || 1);
      setLimit(res.limit || 15);
      setTotalPages(res.total_pages || 0);
    } catch (err) {
      console.error("Failed to load alert history", err);
    } finally {
      setIsHistoryLoading(false);
    }
  };

  // 1. Initial alerts configuration load
  useEffect(() => {
    setIsLoading(true);
    fetchAPI<Alert[]>("/api/alerts")
      .then((alertsData) => {
        setAlerts(alertsData || []);
      })
      .catch((err) => {
        if (isUnauthorized(err)) {
          navigate("/login", { replace: true });
        } else {
          setError(err.message);
        }
      })
      .finally(() => {
        setIsLoading(false);
      });
  }, [navigate]);

  // 2. Reactive history fetching on page/search/status changes
  useEffect(() => {
    fetchHistory(page, limit, historySearch, historyStatus);
  }, [page, limit, historySearch, historyStatus]);

  const handleSearchChange = (val: string) => {
    setSearchInputValue(val);
    setPage(1);
  };

  const handleStatusChange = (val: string) => {
    setHistoryStatus(val);
    setPage(1);
  };

  const handleDelete = (id: string) => {
    setDeleteConfirmAlertId(id);
  };

  const handleDeleteExecute = async (id: string) => {
    setDeletingId(id);
    try {
      await fetchAPI(`/api/alerts/${id}`, { method: "DELETE" });
      setAlerts((prev) => prev.filter((a) => a.alert_id !== id));
    } catch (err: unknown) {
      if (isUnauthorized(err)) {
        navigate("/login", { replace: true });
      } else {
        alert(err instanceof Error ? err.message : "Failed to delete");
      }
    } finally {
      setDeletingId(null);
    }
  };

  const toggleEnabled = async (alertItem: Alert) => {
    const updated = { ...alertItem, enabled: !alertItem.enabled };
    try {
      // We need to send the full updated alert for PUT
      // But the API expects agents as []string (ID only)
      const agentIDs = alertItem.agents.map(a => a.agent_id);
      await fetchAPI(`/api/alerts/${alertItem.alert_id}`, {
        method: "PUT",
        body: JSON.stringify({
          nickname: updated.nickname,
          enabled: updated.enabled,
          trigger: updated.trigger,
          action: updated.action,
          agents: agentIDs
        })
      });
      setAlerts(prev => prev.map(a => a.alert_id === alertItem.alert_id ? updated : a));
    } catch (err) {
      alert("Failed to update alert status");
    }
  };

  return (
    <div className="mobile-p-sm" style={{ padding: '32px' }}>
      <div className="animate-fade-in mobile-gap-sm" style={{ maxWidth: '1000px', margin: '0 auto' }}>

        <div className="mobile-stack" style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: '32px', gap: '16px', flexWrap: 'wrap' }}>
          <div>
            <h1 className="font-display mobile-text-lg" style={{ fontSize: '28px', fontWeight: '700', marginBottom: '8px' }}>Alerting</h1>
            <p style={{ color: 'var(--text-secondary)', fontSize: '14px' }}>Configure thresholds and notifications for your agent.</p>
          </div>
          <Link to="/alerts/create" className="btn-primary mobile-full" style={{ gap: '8px', justifyContent: 'center' }}>
            <span className="material-symbols-outlined" style={{ fontSize: '18px' }}>add_alert</span>
            New Alert
          </Link>
        </div>

        {isLoading ? (
          <div style={{ padding: '64px', textAlign: 'center', color: 'var(--text-muted)' }}>
            <span className="material-symbols-outlined" style={{ fontSize: '32px', animation: 'spin 1s linear infinite' }}>sync</span>
            <p style={{ marginTop: '16px', fontSize: '14px', fontWeight: '500' }}>Loading alerts...</p>
          </div>
        ) : error ? (
          <div style={{ background: 'rgba(239, 68, 68, 0.1)', border: '1px solid rgba(239, 68, 68, 0.3)', borderRadius: '8px', padding: '16px', color: 'var(--status-offline)' }}>
            <div style={{ display: 'flex', alignItems: 'center', gap: '8px', fontWeight: '600', marginBottom: '4px' }}>
              <span className="material-symbols-outlined">error</span>
              Failed to load alerts
            </div>
            <p style={{ fontSize: '13px' }}>{error}</p>
          </div>
        ) : alerts.length === 0 ? (
          <div className="glass-panel" style={{ padding: '64px', textAlign: 'center' }}>
            <div style={{ width: '64px', height: '64px', borderRadius: '50%', background: 'var(--bg-secondary)', border: '1px solid var(--border-color)', display: 'flex', alignItems: 'center', justifyContent: 'center', margin: '0 auto 16px', color: 'var(--text-muted)' }}>
              <span className="material-symbols-outlined" style={{ fontSize: '32px' }}>notifications_off</span>
            </div>
            <h3 style={{ fontSize: '18px', fontWeight: '600', marginBottom: '8px', color: 'var(--text-primary)' }}>No active alerts</h3>
            <p style={{ color: 'var(--text-secondary)', fontSize: '14px', marginBottom: '24px' }}>Set up your first alert to stay informed about your server status.</p>
            <Link to="/alerts/create" className="btn-secondary">
              Create Alert
            </Link>
          </div>
        ) : (
          <div style={{ display: 'flex', flexDirection: 'column', gap: '32px' }}>
            {/* Active Incidents Summary */}
            {(() => {
              const firing = alerts.flatMap(a =>
                a.enabled ? a.agents.filter(ag => ag.status === 'firing' || ag.status === 'failed').map(ag => ({ ...ag, alert: a })) : []
              );
              if (firing.length === 0) return null;
              return (
                <div className="glass-panel" style={{ padding: '24px', border: '1px solid var(--status-offline)', background: 'rgba(239, 68, 68, 0.05)' }}>
                  <h2 style={{ fontSize: '14px', fontWeight: '700', color: 'var(--status-offline)', textTransform: 'uppercase', letterSpacing: '0.05em', marginBottom: '16px', display: 'flex', alignItems: 'center', gap: '8px' }}>
                    <span className="material-symbols-outlined" style={{ fontSize: '18px' }}>warning</span> Active Incidents ({firing.length})
                  </h2>
                  <div style={{ display: 'flex', flexDirection: 'column', gap: '12px' }}>
                    {firing.map((f, i) => (
                      <div key={i} className="mobile-stack" style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', padding: '12px 16px', background: 'var(--bg-primary)', borderRadius: '8px', border: '1px solid var(--border-color)', gap: '12px' }}>
                        <div style={{ display: 'flex', alignItems: 'center', gap: '12px' }}>
                          <span className="material-symbols-outlined" style={{ color: 'var(--status-offline)', fontSize: '20px' }}>
                            {f.status === 'failed' ? 'error' : 'notifications_active'}
                          </span>
                          <div>
                            <div style={{ fontSize: '14px', fontWeight: '600' }}>
                              {f.alert.nickname || f.agent_id} {f.alert.nickname && <span style={{ fontWeight: '400', fontSize: '12px', color: 'var(--text-secondary)' }}>({f.agent_id})</span>}
                            </div>
                            <div style={{ fontSize: '12px', color: 'var(--text-secondary)' }}>
                              {f.alert.trigger.type === 'agent_down' ? (
                                <span style={{ fontWeight: '700', color: 'var(--status-offline)' }}>NODE IS OFFLINE</span>
                              ) : (
                                <>
                                  {getAlertConditionString(f.alert.trigger)}
                                </>
                              )}
                              {f.status === 'failed' && <span style={{ color: 'var(--status-offline)', marginLeft: '8px' }}>• Notification Failed</span>}
                            </div>
                          </div>
                        </div>
                        <div style={{ display: 'flex', gap: '8px', width: '100%', justifyContent: 'flex-end' }} className="mobile-flex-col">
                          {onSelectNode && (
                            <button
                              onClick={() => onSelectNode(f.agent_id)}
                              className="btn-secondary mobile-full"
                              style={{ padding: '4px 12px', fontSize: '12px', gap: '4px', justifyContent: 'center' }}
                            >
                              <span className="material-symbols-outlined" style={{ fontSize: '14px' }}>visibility</span>
                              Go to Node
                            </button>
                          )}
                          <Link to={`/alerts/edit?id=${f.alert.alert_id}`} className="btn-secondary mobile-full" style={{ padding: '4px 12px', fontSize: '12px', gap: '4px', justifyContent: 'center' }}>
                            <span className="material-symbols-outlined" style={{ fontSize: '14px' }}>settings</span>
                            Manage
                          </Link>
                        </div>
                      </div>
                    ))}
                  </div>
                </div>
              );
            })()}

            <div style={{ display: 'flex', flexDirection: 'column', gap: '12px' }}>
              <h2 style={{ fontSize: '14px', fontWeight: '700', color: 'var(--text-secondary)', textTransform: 'uppercase', letterSpacing: '0.05em', marginBottom: '4px' }}>Configured Alerts</h2>
              {alerts.map((alert) => (
                <div key={alert.alert_id} className="card mobile-stack mobile-p-sm" style={{ padding: '20px', display: 'flex', alignItems: 'center', gap: '20px' }}>
                  <div className="mobile-gap-sm" style={{ display: 'flex', alignItems: 'center', gap: '20px', width: '100%' }}>
                    <div
                      onClick={() => toggleEnabled(alert)}
                      style={{
                        cursor: 'pointer',
                        width: '40px',
                        height: '24px',
                        borderRadius: '12px',
                        background: alert.enabled ? 'var(--status-online)' : 'var(--bg-secondary)',
                        position: 'relative',
                        transition: 'var(--transition-fast)',
                        flexShrink: 0,
                        border: '1px solid var(--border-color)'
                      }}
                    >
                      <div style={{
                        position: 'absolute',
                        top: '2px',
                        left: alert.enabled ? '18px' : '2px',
                        width: '18px',
                        height: '18px',
                        borderRadius: '50%',
                        background: '#fff',
                        transition: 'var(--transition-fast)',
                        boxShadow: '0 2px 4px rgba(0,0,0,0.2)'
                      }} />
                    </div>

                    <div style={{ flex: 1 }}>
                      <div style={{ display: 'flex', alignItems: 'center', gap: '8px', marginBottom: '4px', flexWrap: 'wrap' }}>
                        <span className="material-symbols-outlined" style={{ fontSize: '18px', color: 'var(--text-secondary)' }}>
                          {TRIGGER_ICONS[alert.trigger.type] || 'notifications'}
                        </span>
                        <h3 style={{ fontSize: '15px', fontWeight: '600' }}>
                          {alert.nickname || getAlertConditionString(alert.trigger)}
                        </h3>
                        {alert.trigger.duration && (
                          <span className="badge" style={{ background: 'var(--bg-secondary)', color: 'var(--text-secondary)', fontSize: '10px' }}>
                            for {alert.trigger.duration}
                          </span>
                        )}
                      </div>
                      <p style={{ fontSize: '12px', color: 'var(--text-muted)' }}>
                        {alert.nickname && (
                          <span style={{ marginRight: '8px', color: 'var(--text-secondary)' }}>
                            {getAlertConditionString(alert.trigger)} •
                          </span>
                        )}
                        Targeting {alert.agents.length} nodes • Notify via {alert.action.type}
                      </p>
                    </div>
                  </div>

                  <div className="flex items-center gap-2 mobile-full" style={{ justifyContent: 'flex-end' }}>
                    <Link
                      to={`/alerts/edit?id=${alert.alert_id}`}
                      className="mobile-full"
                      style={{ padding: '8px', borderRadius: '8px', background: 'var(--bg-secondary)', color: 'var(--text-secondary)', transition: 'var(--transition-fast)', display: 'flex', alignItems: 'center', justifyContent: 'center' }}
                      onMouseOver={(e) => { e.currentTarget.style.color = 'var(--text-primary)'; e.currentTarget.style.background = 'rgba(255,255,255,0.1)'; }}
                      onMouseOut={(e) => { e.currentTarget.style.color = 'var(--text-secondary)'; e.currentTarget.style.background = 'var(--bg-secondary)'; }}
                    >
                      <span className="material-symbols-outlined" style={{ fontSize: '20px' }}>edit</span>
                    </Link>
                    <button
                      onClick={() => handleDelete(alert.alert_id)}
                      disabled={deletingId === alert.alert_id}
                      className="mobile-full"
                      style={{ padding: '8px', borderRadius: '8px', background: 'var(--bg-secondary)', color: 'var(--status-offline)', transition: 'var(--transition-fast)', display: 'flex', alignItems: 'center', justifyContent: 'center' }}
                      onMouseOver={(e) => { if (deletingId !== alert.alert_id) e.currentTarget.style.background = 'rgba(239, 68, 68, 0.15)'; }}
                      onMouseOut={(e) => { if (deletingId !== alert.alert_id) e.currentTarget.style.background = 'var(--bg-secondary)'; }}
                    >
                      <span className="material-symbols-outlined" style={{ fontSize: '20px' }}>
                        {deletingId === alert.alert_id ? 'hourglass_empty' : 'delete'}
                      </span>
                    </button>
                  </div>
                </div>
              ))}
            </div>
          </div>
        )}

        {/* Recent Alert Events Section */}
        {true && (
          <div style={{ marginTop: '48px' }}>
            <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: '16px' }}>
              <h2 className="font-display mobile-text-lg" style={{ fontSize: '20px', fontWeight: '600', margin: 0 }}>Recent Alert Events</h2>
              {isHistoryLoading && (
                <span className="material-symbols-outlined" style={{ fontSize: '18px', animation: 'spin 1s linear infinite', color: 'var(--accent-primary)' }}>sync</span>
              )}
            </div>

            {/* History Toolbar Row */}
            <div style={{
              display: 'flex',
              alignItems: 'center',
              gap: '12px',
              flexWrap: 'wrap',
              background: 'var(--bg-secondary)',
              padding: '8px',
              borderRadius: '16px',
              border: '1px solid var(--border-color)',
              boxShadow: '0 4px 12px rgba(0,0,0,0.1)',
              marginBottom: '16px'
            }}>
              {/* Search Box */}
              <div style={{ position: 'relative', flex: '1', minWidth: '240px' }}>
                <span className="material-symbols-outlined" style={{ position: 'absolute', left: '16px', top: '50%', transform: 'translateY(-50%)', fontSize: '20px', color: 'var(--text-muted)', pointerEvents: 'none' }}>search</span>
                <input
                  type="text"
                  placeholder="Search events by node, condition or metric..."
                  value={searchInputValue}
                  onChange={(e) => handleSearchChange(e.target.value)}
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

              {/* Status Segmented Controls */}
              <div style={{ display: 'flex', gap: '2px', background: 'var(--bg-primary)', padding: '3px', borderRadius: '10px', border: '1px solid var(--border-color)' }}>
                {(["all", "firing", "resolved"] as const).map((status) => (
                  <button
                    key={status}
                    onClick={() => handleStatusChange(status)}
                    style={{
                      padding: '6px 12px',
                      borderRadius: '8px',
                      fontSize: '12px',
                      fontWeight: '700',
                      textTransform: 'uppercase',
                      letterSpacing: '0.05em',
                      background: historyStatus === status ? 'var(--accent-primary)' : 'transparent',
                      color: historyStatus === status ? '#fff' : 'var(--text-muted)',
                      transition: 'var(--transition-fast)',
                      cursor: 'pointer',
                      border: 'none'
                    }}
                  >
                    {status === "all" ? "All" : status === "firing" ? "Firing" : "Resolved"}
                  </button>
                ))}
              </div>
            </div>

            {history.length === 0 ? (
              <div style={{ textAlign: 'center', padding: '48px 24px', border: '1px dashed var(--border-color)', borderRadius: '12px', background: 'var(--bg-secondary)', display: 'flex', flexDirection: 'column', alignItems: 'center', justifyContent: 'center' }}>
                <span className="material-symbols-outlined" style={{ fontSize: '48px', color: 'var(--text-muted)', marginBottom: '12px', opacity: 0.5 }}>manage_search</span>
                <h3 style={{ fontSize: '16px', fontWeight: '600', color: 'var(--text-primary)', marginBottom: '4px' }}>No Events Found</h3>
                <p style={{ color: 'var(--text-muted)', fontSize: '13px' }}>Try adjusting your search query or status filter.</p>
              </div>
            ) : (
              <>
                <div className="card" style={{ overflowX: 'auto', padding: 0 }}>
                  <div style={{ minWidth: '700px' }}>
                    <table style={{ width: '100%', borderCollapse: 'collapse', textAlign: 'left' }}>
                      <thead style={{ position: 'sticky', top: 0, background: 'var(--bg-secondary)', zIndex: 1 }}>
                        <tr>
                          <th style={{ padding: '16px', fontSize: '12px', fontWeight: '600', color: 'var(--text-secondary)', borderBottom: '1px solid var(--border-color)' }}>Incident Timeline</th>
                          <th style={{ padding: '16px', fontSize: '12px', fontWeight: '600', color: 'var(--text-secondary)', borderBottom: '1px solid var(--border-color)' }}>Node</th>
                          <th style={{ padding: '16px', fontSize: '12px', fontWeight: '600', color: 'var(--text-secondary)', borderBottom: '1px solid var(--border-color)' }}>Violation & Condition</th>
                          <th style={{ padding: '16px', fontSize: '12px', fontWeight: '600', color: 'var(--text-secondary)', borderBottom: '1px solid var(--border-color)' }}>Status & Duration</th>
                        </tr>
                      </thead>
                      <tbody>
                        {history.map(h => {
                          const tTrigger = new Date(h.triggered_at);
                          const tResolve = h.resolved_at ? new Date(h.resolved_at) : null;
                          const isResolved = !!h.resolved_at;

                          let durationStr = "";
                          if (isResolved && tResolve) {
                            const diffSec = Math.floor((tResolve.getTime() - tTrigger.getTime()) / 1000);
                            const hrs = Math.floor(diffSec / 3600);
                            const mins = Math.floor((diffSec % 3600) / 60);
                            const secs = diffSec % 60;
                            if (hrs > 0) {
                              durationStr = `${hrs}h ${mins}m ${secs}s`;
                            } else if (mins > 0) {
                              durationStr = `${mins}m ${secs}s`;
                            } else {
                              durationStr = `${secs}s`;
                            }
                          }

                          return (
                            <tr
                              key={h.history_id}
                              style={{ borderBottom: '1px solid var(--border-color)', transition: 'background var(--transition-fast)' }}
                              onMouseOver={(e) => e.currentTarget.style.background = 'rgba(255, 255, 255, 0.015)'}
                              onMouseOut={(e) => e.currentTarget.style.background = 'none'}
                            >
                              <td style={{ padding: '16px', fontSize: '13px', color: 'var(--text-primary)', verticalAlign: 'top' }}>
                                <div style={{ display: 'flex', flexDirection: 'column', gap: '4px', fontSize: '12px' }}>
                                  <div style={{ display: 'flex', alignItems: 'center', gap: '8px' }}>
                                    <span style={{ width: '6px', height: '6px', borderRadius: '50%', backgroundColor: 'var(--status-offline)', flexShrink: 0 }} />
                                    <span style={{ color: 'var(--text-secondary)' }}>Triggered:</span>
                                    <span style={{ fontWeight: '500', fontFamily: 'var(--font-mono)' }}>
                                      {tTrigger.toLocaleString([], { month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit', second: '2-digit' })}
                                    </span>
                                  </div>
                                  {tResolve ? (
                                    <div style={{ display: 'flex', alignItems: 'center', gap: '8px' }}>
                                      <span style={{ width: '6px', height: '6px', borderRadius: '50%', backgroundColor: 'var(--status-online)', flexShrink: 0 }} />
                                      <span style={{ color: 'var(--text-secondary)' }}>Resolved:</span>
                                      <span style={{ fontWeight: '500', fontFamily: 'var(--font-mono)' }}>
                                        {tResolve.toLocaleString([], { month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit', second: '2-digit' })}
                                      </span>
                                    </div>
                                  ) : (
                                    <div style={{ display: 'flex', alignItems: 'center', gap: '8px' }}>
                                      <span style={{ width: '6px', height: '6px', borderRadius: '50%', backgroundColor: 'var(--status-offline)', flexShrink: 0, animation: 'pulse 1.5s infinite' }} />
                                      <span style={{ color: 'var(--status-offline)', fontWeight: '600' }}>Firing (Unresolved)</span>
                                    </div>
                                  )}
                                </div>
                              </td>
                              <td style={{ padding: '16px', fontSize: '13px', fontWeight: '600', color: 'var(--text-primary)', verticalAlign: 'top' }}>
                                {h.agent_nickname || h.agent_id.substring(0, 8)}
                                <div style={{ fontSize: '10px', fontWeight: 'normal', color: 'var(--text-muted)', marginTop: '4px', fontFamily: 'var(--font-mono)' }}>{h.agent_id}</div>
                              </td>
                              <td style={{ padding: '16px', fontSize: '13px', color: 'var(--text-secondary)', verticalAlign: 'top' }}>
                                <div style={{ display: 'flex', alignItems: 'center', gap: '6px', fontWeight: '600', color: 'var(--text-primary)', marginBottom: '6px' }}>
                                  <span className="material-symbols-outlined" style={{ fontSize: '18px', color: 'var(--accent-primary)' }}>
                                    {TRIGGER_ICONS[h.trigger.type] || "notifications"}
                                  </span>
                                  {h.alert_nickname || TRIGGER_LABELS[h.trigger.type] || h.trigger.type}
                                </div>
                                {h.alert_nickname && (
                                  <div style={{ fontSize: '12px', color: 'var(--text-muted)', marginBottom: '4px' }}>
                                    Type: <span style={{ color: 'var(--text-secondary)', fontWeight: '500' }}>{TRIGGER_LABELS[h.trigger.type] || h.trigger.type}</span>
                                  </div>
                                )}
                                <div style={{ fontSize: '12px', color: 'var(--text-muted)' }}>
                                  Condition: <span style={{ fontFamily: 'var(--font-mono)', fontSize: '11px', color: 'var(--text-primary)', background: 'var(--bg-secondary)', padding: '2px 6px', borderRadius: '4px' }}>{getAlertConditionString(h.trigger)}</span>
                                </div>
                                {h.trigger.type !== "agent_down" && (
                                  <div style={{ fontSize: '12px', color: 'var(--text-muted)', marginTop: '6px' }}>
                                    Breach Value: <strong style={{ color: 'var(--status-offline)' }}>{h.trigger_value.toFixed(1)}{getSuffix(h.trigger.type)}</strong>
                                  </div>
                                )}
                              </td>
                              <td style={{ padding: '16px', verticalAlign: 'top' }}>
                                <div style={{ display: 'flex', flexDirection: 'column', gap: '6px' }}>
                                  <span className="badge" style={{ background: isResolved ? 'rgba(16, 185, 129, 0.1)' : 'rgba(239, 68, 68, 0.1)', color: isResolved ? 'var(--status-online)' : 'var(--status-offline)', alignSelf: 'flex-start', border: isResolved ? '1px solid rgba(16, 185, 129, 0.3)' : '1px solid rgba(239, 68, 68, 0.3)', fontWeight: '600' }}>
                                    {isResolved ? 'Resolved' : 'Firing'}
                                  </span>
                                  {isResolved && durationStr && (
                                    <span style={{ fontSize: '11px', color: 'var(--text-secondary)', display: 'flex', alignItems: 'center', gap: '4px', fontWeight: '500' }}>
                                      <span className="material-symbols-outlined" style={{ fontSize: '14px', color: 'var(--accent-primary)' }}>schedule</span>
                                      Downtime: {durationStr}
                                    </span>
                                  )}
                                  {h.notified_status === 'failed' && (
                                    <span style={{ fontSize: '10px', color: 'var(--status-offline)', display: 'flex', alignItems: 'center', gap: '2px' }}>
                                      <span className="material-symbols-outlined" style={{ fontSize: '12px' }}>error</span> Delivery Failed
                                    </span>
                                  )}
                                </div>
                              </td>
                            </tr>
                          );
                        })}
                      </tbody>
                    </table>
                  </div>
                </div>

                {/* Pagination Controls */}
                {totalPages > 1 && (
                  <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginTop: '20px', gap: '16px', flexWrap: 'wrap' }}>
                    <span style={{ fontSize: '13px', color: 'var(--text-secondary)' }}>
                      Showing <strong style={{ color: 'var(--text-primary)' }}>{((page - 1) * limit) + 1}</strong> to{" "}
                      <strong style={{ color: 'var(--text-primary)' }}>{Math.min(page * limit, total)}</strong> of{" "}
                      <strong style={{ color: 'var(--text-primary)' }}>{total}</strong> events
                    </span>

                    <div style={{ display: 'flex', gap: '6px', alignItems: 'center' }}>
                      <button
                        onClick={() => { if (page > 1) setPage(page - 1); }}
                        disabled={page <= 1 || isHistoryLoading}
                        className="btn-secondary"
                        style={{ padding: '6px 12px', fontSize: '12px', display: 'flex', alignItems: 'center', opacity: page <= 1 ? 0.5 : 1, cursor: page <= 1 ? 'not-allowed' : 'pointer', gap: '4px' }}
                      >
                        <span className="material-symbols-outlined" style={{ fontSize: '16px' }}>chevron_left</span>
                        Prev
                      </button>

                      {Array.from({ length: totalPages }, (_, idx) => idx + 1).map(p => {
                        if (totalPages > 5 && Math.abs(p - page) > 1 && p !== 1 && p !== totalPages) {
                          if (Math.abs(p - page) === 2) {
                            return <span key={p} style={{ padding: '0 4px', color: 'var(--text-muted)' }}>...</span>;
                          }
                          return null;
                        }
                        return (
                          <button
                            key={p}
                            onClick={() => setPage(p)}
                            disabled={isHistoryLoading}
                            style={{
                              minWidth: '32px',
                              height: '32px',
                              borderRadius: '6px',
                              fontSize: '12px',
                              fontWeight: '600',
                              border: 'none',
                              cursor: 'pointer',
                              background: page === p ? 'var(--accent-primary)' : 'var(--bg-secondary)',
                              color: page === p ? 'var(--bg-primary)' : 'var(--text-primary)',
                              transition: 'var(--transition-fast)'
                            }}
                          >
                            {p}
                          </button>
                        );
                      })}

                      <button
                        onClick={() => { if (page < totalPages) setPage(page + 1); }}
                        disabled={page >= totalPages || isHistoryLoading}
                        className="btn-secondary"
                        style={{ padding: '6px 12px', fontSize: '12px', display: 'flex', alignItems: 'center', opacity: page >= totalPages ? 0.5 : 1, cursor: page >= totalPages ? 'not-allowed' : 'pointer', gap: '4px' }}
                      >
                        Next
                        <span className="material-symbols-outlined" style={{ fontSize: '16px' }}>chevron_right</span>
                      </button>
                    </div>
                  </div>
                )}
              </>
            )}
          </div>
        )}
      </div>

      <DeleteConfirmModal
        isOpen={deleteConfirmAlertId !== null}
        title="Delete Alert?"
        message="Are you sure you want to delete this alert? This action cannot be undone and will permanently remove this notification rule."
        confirmText="Delete Alert"
        onClose={() => setDeleteConfirmAlertId(null)}
        onConfirm={() => {
          const id = deleteConfirmAlertId;
          if (id) {
            setDeleteConfirmAlertId(null);
            handleDeleteExecute(id);
          }
        }}
      />
    </div>
  );
}