import { useEffect, useState, useCallback, useRef } from "react";
import { useNavigate, useLocation, useParams } from "react-router-dom";

import { fetchAPI, getWSURL } from "../../lib/api";
import { Agent, ProvisionResponse, AgentSnapshot } from "../../types";
import { AgentSidebar } from "./AgentSidebar";
import { AgentDetail } from "./AgentDetail";
import { AgentView } from "./AgentView";
import { DashboardsPanel } from "../dashboards/DashboardsPanel";
import AlertsPanel from "../alerts/AlertsPanel";
import PanelNav from "../common/PanelNav";
import ReinstallModal from "../common/ReinstallModal";
import DeleteConfirmModal from "../common/DeleteConfirmModal";
import AddNodeModal from "../common/AddNodeModal";
import { fmtBytes, fmtBps, fmtUptime, isUnauthorized } from "../../lib/utils";

// ── Metric chart config ────────────────────────────────────────────────────

type TabKey = "cpu" | "ram" | "disk" | "disk_io" | "net";
type HourKey = 1 | 6 | 12 | 24 | 48 | 168 | 720 | 2160 | 4320 | 8760 | 17520 | 43800 | 87600 | 876000;

const TIME_RANGES: { label: string; value: HourKey }[] = [
  { label: "1h", value: 1 },
  { label: "6h", value: 6 },
  { label: "12h", value: 12 },
  { label: "24h", value: 24 },
  { label: "2d", value: 48 },
  { label: "7d", value: 168 },
  { label: "30d", value: 720 },
  { label: "90d", value: 2160 },
  { label: "180d", value: 4320 },
  { label: "1y", value: 8760 },
  { label: "2y", value: 17520 }
];

interface SeriesCfg {
  metric: string;
  label: string;
  color: string;
  fill?: boolean;
}

const TABS: Record<TabKey, { label: string; series: SeriesCfg[]; fmt: (v: number) => string }> = {
  cpu: {
    label: "CPU",
    fmt: (v) => `${v.toFixed(1)}%`,
    series: [
      { metric: "agent_cpu_usage", label: "Usr", color: "var(--accent-primary)", fill: true },
      { metric: "agent_cpu_iowait", label: "IO", color: "var(--text-secondary)" },
      { metric: "agent_cpu_steal", label: "Stl", color: "var(--status-offline)" },
    ],
  },
  ram: {
    label: "Memory",
    fmt: fmtBytes,
    series: [
      { metric: "agent_ram_used", label: "RAM", color: "#14b8a6", fill: false },
      { metric: "agent_swap_used", label: "Swap", color: "var(--text-secondary)" },
    ],
  },
  net: {
    label: "Network I/O",
    fmt: fmtBps,
    series: [
      { metric: "agent_rx_bytes", label: "RX", color: "#1e40af" },
      { metric: "agent_tx_bytes", label: "TX", color: "#7e22ce" },
    ],
  },
  disk: {
    label: "Disk Usage",
    fmt: fmtBytes,
    series: [
      { metric: "agent_disk_used", label: "Used", color: "var(--accent-secondary)", fill: true },
    ],
  },
  disk_io: {
    label: "Disk I/O",
    fmt: fmtBps,
    series: [
      { metric: "agent_disk_read_bytes", label: "Read", color: "#f59e0b" },
      { metric: "agent_disk_write_bytes", label: "Write", color: "#ef4444" },
    ],
  },
};

// ── Main component ─────────────────────────────────────────────────────────

export default function AdminPanel() {
  const navigate = useNavigate();
  const location = useLocation();
  const { id } = useParams<{ id?: string }>();

  const path = location.pathname;
  let section = "agent";
  if (path.includes("/dashboards")) section = "dashboards";
  else if (path.includes("/alerts")) section = "alerts";
  else if (path.includes("/management")) section = "management";
  else if (path.includes("/settings")) section = "settings";
  const selectedId = id || null;

  const [isSidebarExpanded, setIsSidebarExpanded] = useState(false);
  const [viewMode, setViewMode] = useState<"grid" | "list">(() => {
    const saved = localStorage.getItem("certainstats_agent_view_mode");
    return (saved === "list" || saved === "grid") ? saved as "grid" | "list" : "grid";
  });
  const [gridDensity, setGridDensity] = useState<"detailed" | "simplified">(() => {
    const saved = localStorage.getItem("certainstats_grid_density");
    return (saved === "simplified" || saved === "detailed") ? saved as "detailed" | "simplified" : "detailed";
  });

  useEffect(() => {
    localStorage.setItem("certainstats_agent_view_mode", viewMode);
  }, [viewMode]);

  useEffect(() => {
    localStorage.setItem("certainstats_grid_density", gridDensity);
  }, [gridDensity]);



  const [agents, setAgents] = useState<Agent[]>([]);
  const agentsRef = useRef<Agent[]>([]);
  useEffect(() => { agentsRef.current = agents; }, [agents]);
  const [filter, setFilter] = useState("");
  const [activeHours, setActiveHours] = useState<HourKey>(() => {
    const saved = localStorage.getItem("certainstats_active_hours");
    if (saved) {
      const v = parseInt(saved, 10);
      if (TIME_RANGES.some(r => r.value === v)) return v as HourKey;
    }
    return 6;
  });

  useEffect(() => {
    localStorage.setItem("certainstats_active_hours", activeHours.toString());
  }, [activeHours]);

  // Modals & toasts
  const [provisionResult, setProvisionResult] = useState<ProvisionResponse | null>(null);
  const [provisioning, setProvisioning] = useState(false);
  const [copied, setCopied] = useState(false);
  const [revokeTarget, setRevokeTarget] = useState<Agent | null>(null);
  const [revoking, setRevoking] = useState(false);
  const [toast, setToast] = useState<{ msg: string; ok: boolean } | null>(null);
  const toastTimer = useRef<ReturnType<typeof setTimeout> | null>(null);

  const [showTypeSelect, setShowTypeSelect] = useState(false);
  const [renameTarget, setRenameTarget] = useState<Agent | null>(null);
  const [renaming, setRenaming] = useState(false);

  const showToast = (msg: string, ok = true) => {
    setToast({ msg, ok });
    if (toastTimer.current) clearTimeout(toastTimer.current);
    toastTimer.current = setTimeout(() => setToast(null), 3200);
  };

  const loadAgents = useCallback(async () => {
    if (section !== "agent") return;
    try {
      const data = await fetchAPI<Agent[]>("/api/agents");
      setAgents(data ?? []);
    } catch (err: unknown) {
      if (isUnauthorized(err)) {
        navigate("/login", { replace: true });
      }
    }
  }, [navigate, section]);

  useEffect(() => {
    loadAgents();
    const timer = setInterval(loadAgents, 30000);
    return () => clearInterval(timer);
  }, [loadAgents]);

  const [liveMetrics, setLiveMetrics] = useState<Record<string, AgentSnapshot>>({});
  useEffect(() => {
    const wsUrl = getWSURL("/api/ws");
    let ws: WebSocket;
    let reconnectTimer: ReturnType<typeof setTimeout>;

    function connect() {
      ws = new WebSocket(wsUrl);
      ws.onmessage = (e) => {
        try {
          const pkg = JSON.parse(e.data);
          if (pkg.type === "agent_update") {
            const snaps = pkg.data || {};
            const enriched: Record<string, any> = {};

            for (const id in snaps) {
              const snap = snaps[id];
              const agent = agentsRef.current.find(a => a.agent_id === id);

              const item = { ...snap };

              if (agent) {
                if (agent.ram_size > 0) item.RAMUsagePercent = (snap.RAMUsedBytes / agent.ram_size) * 100;
                if (agent.swap_size > 0) item.RAMSwapUsagePercent = (snap.RAMSwapUsedBytes / agent.swap_size) * 100;
                if (agent.disk_size > 0) item.DiskUsagePercent = (snap.DiskUsedBytes / agent.disk_size) * 100;
              }
              enriched[id] = item;
            }

            setLiveMetrics(prev => ({ ...prev, ...enriched }));
          }
        } catch (err) { }
      };
      ws.onclose = () => {
        reconnectTimer = setTimeout(connect, 3000);
      };
    }

    connect();
    return () => {
      if (ws) ws.close();
      clearTimeout(reconnectTimer);
    };
  }, []);

  const handleProvision = async (agentType: string) => {
    setProvisioning(true);
    try {
      const res = await fetchAPI<ProvisionResponse>("/api/agent", {
        method: "POST",
        body: JSON.stringify({ agent_type: agentType })
      });
      setProvisionResult(res);
      setShowTypeSelect(false);
      loadAgents();
    } catch (err: unknown) {
      if (isUnauthorized(err)) {
        navigate("/login", { replace: true });
      } else {
        showToast("Failed to provision agent", false);
      }
    } finally {
      setProvisioning(false);
    }
  };

  const handleInstall = async (agentId: string) => {
    try {
      const res = await fetchAPI<ProvisionResponse>(`/api/agent/install/${agentId}`);
      setProvisionResult(res);
    } catch (err: unknown) {
      showToast("Failed to fetch installation instructions", false);
    }
  };

  const handleRename = async (newName: string) => {
    if (!renameTarget) return;
    setRenaming(true);
    try {
      await fetchAPI("/api/agent", {
        method: "PUT",
        body: JSON.stringify({ agent_id: renameTarget.agent_id, nickname: newName })
      });
      setAgents(prev => prev.map(a => a.agent_id === renameTarget.agent_id ? { ...a, nickname: newName } : a));
      setRenameTarget(null);
      showToast("Node renamed");
    } catch (err: unknown) {
      showToast("Rename failed", false);
    } finally {
      setRenaming(false);
    }
  };

  const handleRevoke = async () => {
    if (!revokeTarget) return;
    setRevoking(true);
    try {
      await fetchAPI(`/api/agent?agent_id=${revokeTarget.agent_id}`, { method: "DELETE" });
      showToast("Agent revoked");
      if (selectedId === revokeTarget.agent_id) navigate("/");
      setRevokeTarget(null);
      loadAgents();
    } catch (err: unknown) {
      if (isUnauthorized(err)) {
        navigate("/login", { replace: true });
      } else {
        showToast(err instanceof Error ? err.message : "Revoke failed", false);
      }
    } finally {
      setRevoking(false);
    }
  };

  const filteredAgents = agents.filter(a =>
    (a.nickname || a.agent_id).toLowerCase().includes(filter.toLowerCase())
  );

  const selectedAgent = agents.find(a => a.agent_id === selectedId);

  return (
    <div className="dashboard-container" style={{ display: 'flex', height: '100vh', background: 'var(--bg-primary)', overflow: 'hidden' }}>
      {/* Sidebar */}
      {section === "agent" && (
        <AgentSidebar
          selectedId={selectedId}
          provisioning={provisioning}
          setShowTypeSelect={setShowTypeSelect}
          filter={filter}
          setFilter={setFilter}
          filteredAgents={filteredAgents}
          liveMetrics={liveMetrics}
          fmtBytes={fmtBytes}
          fmtBps={fmtBps}
          isExpanded={isSidebarExpanded}
          setIsExpanded={setIsSidebarExpanded}
        />
      )}

      <div style={{ flex: 1, display: 'flex', flexDirection: 'column', position: 'relative', overflow: 'hidden' }}>
        {/* Navigation Bar */}
        <PanelNav
          section={section}
        />

        {/* Floating Sidebar Toggle (Desktop & Mobile) */}
        {section === "agent" && !isSidebarExpanded ? (
          <button
            onClick={() => setIsSidebarExpanded(true)}
            className="sidebar-toggle-btn collapsed"
            style={{
              position: 'absolute',
              left: '0',
              top: '50%',
              transform: 'translateY(-50%)',
              zIndex: 400,
              width: '32px',
              height: '80px',
              background: 'var(--bg-secondary)',
              border: '1px solid var(--border-color)',
              borderLeft: 'none',
              borderRadius: '0 16px 16px 0',
              display: 'flex',
              alignItems: 'center',
              justifyContent: 'center',
              color: 'var(--accent-primary)',
              boxShadow: '4px 0 16px rgba(0,0,0,0.3)',
              transition: 'all 0.3s ease'
            }}
            onMouseOver={(e) => e.currentTarget.style.color = 'var(--text-primary)'}
            onMouseOut={(e) => e.currentTarget.style.color = 'var(--accent-primary)'}
          >
            <span className="material-symbols-outlined" style={{ fontSize: '28px' }}>double_arrow</span>
          </button>
        ) : section === "agent" && (
          <button
            onClick={() => setIsSidebarExpanded(false)}
            className="sidebar-toggle-btn expanded"
            style={{
              position: 'absolute',
              top: '50%',
              transform: 'translateY(-50%)',
              zIndex: 400,
              width: '32px',
              height: '80px',
              background: 'var(--bg-secondary)',
              border: '1px solid var(--border-color)',
              borderLeft: 'none',
              borderRadius: '0 16px 16px 0',
              display: 'flex',
              alignItems: 'center',
              justifyContent: 'center',
              color: 'var(--text-muted)',
              boxShadow: '4px 0 16px rgba(0,0,0,0.3)',
              transition: 'all 0.3s cubic-bezier(0.4, 0, 0.2, 1)'
            }}
            onMouseOver={(e) => e.currentTarget.style.color = 'var(--text-primary)'}
            onMouseOut={(e) => e.currentTarget.style.color = 'var(--text-muted)'}
          >
            <span className="material-symbols-outlined" style={{ fontSize: '28px' }}>keyboard_double_arrow_left</span>
          </button>
        )}

        {/* MAIN CONTENT */}
        <main className="main-panel" style={{ flex: 1, background: 'var(--bg-primary)', overflowY: 'auto' }}>
          {section === "agent" ? (
            !selectedAgent ? (
              <div style={{ padding: '32px' }}>
                <AgentView
                  filteredAgents={filteredAgents}
                  liveMetrics={liveMetrics}
                  viewMode={viewMode}
                  setViewMode={setViewMode}
                  gridDensity={gridDensity}
                  setGridDensity={setGridDensity}
                  setShowTypeSelect={setShowTypeSelect}
                  provisioning={provisioning}
                  filter={filter}
                  setFilter={setFilter}
                  fmtBytes={fmtBytes}
                  fmtBps={fmtBps}
                  onInstall={handleInstall}
                  onRename={setRenameTarget}
                  onRevoke={setRevokeTarget}
                />
              </div>
            ) : (
              <div style={{ padding: '32px' }}>
                <AgentDetail
                  agent={selectedAgent}
                  selectedId={selectedId}
                  isSidebarExpanded={isSidebarExpanded}
                  setIsSidebarExpanded={setIsSidebarExpanded}
                  setAgents={setAgents}
                  activeHours={activeHours}
                  setActiveHours={setActiveHours}
                  liveMetrics={liveMetrics}
                  setRevokeTarget={setRevokeTarget}

                  fmtUptime={fmtUptime}
                  fmtBytes={fmtBytes}
                  fmtBps={fmtBps}
                  TIME_RANGES={TIME_RANGES}
                  TABS={TABS}
                  onInstall={handleInstall}
                  onRename={setRenameTarget}
                />
              </div>
            )
          ) : section === "dashboards" ? (
            <div style={{ padding: '32px' }}>
              <DashboardsPanel />
            </div>
          ) : (
            <div style={{ padding: '32px' }}>
              <AlertsPanel onSelectNode={(id) => navigate(`/agent/${id}`)} />
            </div>
          )}
        </main>
      </div>

      {/* PROVISION MODAL */}
      <AddNodeModal
        isOpen={showTypeSelect}
        onClose={() => setShowTypeSelect(false)}
        onConfirm={handleProvision}
        confirming={provisioning}
      />

      {/* PROVISION RESULT MODAL */}
      <ReinstallModal
        data={provisionResult}
        onClose={() => setProvisionResult(null)}
        showToast={showToast}
      />

      {/* RENAME MODAL */}
      {renameTarget && (
        <div
          style={{ position: 'fixed', inset: 0, display: 'flex', alignItems: 'center', justifyContent: 'center', zIndex: 9999, padding: '16px' }}
        >
          <div
            onClick={() => setRenameTarget(null)}
            className="modal-backdrop"
          />
          <div className="card" style={{ width: '100%', maxWidth: '400px', padding: '32px', border: '1px solid var(--border-color)', background: 'var(--bg-primary)', animation: 'fadeIn 0.3s ease', zIndex: 1 }} onClick={e => e.stopPropagation()}>
            <h2 className="font-display text-xl font-bold mb-2" style={{ color: 'var(--text-primary)' }}>Rename Node</h2>
            <p style={{ color: 'var(--text-secondary)', marginBottom: '24px', fontSize: '14px' }}>Enter a new nickname for this node.</p>

            <input
              type="text"
              autoFocus
              defaultValue={renameTarget.nickname || renameTarget.agent_id}
              className="input-field"
              style={{ width: '100%', marginBottom: '24px' }}
              onKeyDown={(e) => {
                if (e.key === 'Enter') handleRename(e.currentTarget.value);
                if (e.key === 'Escape') setRenameTarget(null);
              }}
              id="rename-input"
            />

            <div style={{ display: 'flex', justifyContent: 'flex-end', gap: '12px' }}>
              <button type="button" onClick={() => setRenameTarget(null)} className="btn-secondary" style={{ padding: '10px 20px' }}>Cancel</button>
              <button
                type="button"
                onClick={() => handleRename((document.getElementById('rename-input') as HTMLInputElement).value)}
                disabled={renaming}
                className="btn-primary"
                style={{ padding: '10px 24px', background: 'var(--accent-primary)', color: '#fff', border: 'none' }}
              >
                {renaming ? "Renaming…" : "Save Name"}
              </button>
            </div>
          </div>
        </div>
      )}

      {/* REVOKE MODAL */}
      <DeleteConfirmModal
        isOpen={revokeTarget !== null}
        title="Revoke Agent?"
        message={
          <>
            This will permanently disconnect <strong style={{ color: 'var(--text-primary)' }}>{revokeTarget?.nickname || revokeTarget?.agent_id}</strong> and delete all associated telemetry history and data.
          </>
        }
        confirmText="Revoke Access"
        onClose={() => setRevokeTarget(null)}
        onConfirm={handleRevoke}
        deleting={revoking}
      />

      {/* TOAST */}
      {toast && (
        <div className="animate-fade-in" style={{ position: 'fixed', bottom: '32px', right: '32px', zIndex: 1000 }}>
          <div className="glass-panel" style={{ padding: '12px 24px', borderRadius: '12px', display: 'flex', alignItems: 'center', gap: '12px', boxShadow: '0 8px 32px rgba(0,0,0,0.4)', border: `1px solid ${toast.ok ? 'rgba(16,185,129,0.2)' : 'rgba(239,68,68,0.2)'}` }}>
            <span className="material-symbols-outlined" style={{ color: toast.ok ? 'var(--status-online)' : 'var(--status-offline)' }}>
              {toast.ok ? "check_circle" : "error"}
            </span>
            <span style={{ fontSize: '14px', fontWeight: '600' }}>{toast.msg}</span>
          </div>
        </div>
      )}
    </div>
  );
}