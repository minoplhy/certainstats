import { useState, useCallback, useRef } from "react";
import { useNavigate, useLocation, useParams } from "react-router-dom";

import { fetchAPI } from "../../lib/api";
import { Agent, ProvisionResponse, AgentSnapshot } from "../../types";
import { AgentDetail } from "./AgentDetail";
import { AgentView } from "./AgentView";
import { DashboardsPanel } from "../dashboards/DashboardsPanel";
import AlertsPanel from "../alerts/AlertsPanel";
import ReinstallModal from "../common/ReinstallModal";
import DeleteConfirmModal from "../common/DeleteConfirmModal";
import { fmtBytes, fmtBps, fmtUptime, isUnauthorized } from "../../lib/utils";
import { useApp } from "../../context/AppContext";

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

  const {
    agents,
    setAgents,
    liveMetrics,
    isSidebarExpanded,
    setIsSidebarExpanded,
    showToast,
    filter,
    setFilter,
    filteredAgents,
  } = useApp();

  const path = location.pathname;
  let section = "agent";
  if (path.includes("/dashboards")) section = "dashboards";
  else if (path.includes("/alerts")) section = "alerts";
  const selectedId = id || null;

  const [viewMode, setViewMode] = useState<"grid" | "list">(() => {
    const saved = localStorage.getItem("certainstats_agent_view_mode");
    return (saved === "list" || saved === "grid") ? saved as "grid" | "list" : "grid";
  });
  const [gridDensity, setGridDensity] = useState<"detailed" | "simplified">(() => {
    const saved = localStorage.getItem("certainstats_grid_density");
    return (saved === "simplified" || saved === "detailed") ? saved as "detailed" | "simplified" : "detailed";
  });

  const [activeHours, setActiveHours] = useState<HourKey>(() => {
    const saved = localStorage.getItem("certainstats_active_hours");
    if (saved) {
      const v = parseInt(saved, 10);
      if (TIME_RANGES.some(r => r.value === v)) return v as HourKey;
    }
    return 6;
  });

  // Local Modals
  const [provisionResult, setProvisionResult] = useState<ProvisionResponse | null>(null);
  const [revokeTarget, setRevokeTarget] = useState<Agent | null>(null);
  const [revoking, setRevoking] = useState(false);
  const [renameTarget, setRenameTarget] = useState<Agent | null>(null);
  const [renaming, setRenaming] = useState(false);

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
      setAgents(prev => prev.filter(a => a.agent_id !== revokeTarget.agent_id));
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

  const selectedAgent = agents.find(a => a.agent_id === selectedId);

  return (
    <div style={{ padding: '32px' }}>
      {section === "agent" ? (
        !selectedAgent ? (
          <AgentView
            filteredAgents={filteredAgents}
            liveMetrics={liveMetrics}
            viewMode={viewMode}
            setViewMode={setViewMode}
            gridDensity={gridDensity}
            setGridDensity={setGridDensity}
            setShowTypeSelect={() => {}} // Done at layout level sidebar
            provisioning={false}
            filter={filter}
            setFilter={setFilter}
            fmtBytes={fmtBytes}
            fmtBps={fmtBps}
            onInstall={handleInstall}
            onRename={setRenameTarget}
            onRevoke={setRevokeTarget}
          />
        ) : (
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
        )
      ) : section === "dashboards" ? (
        <DashboardsPanel />
      ) : (
        <AlertsPanel onSelectNode={(id) => navigate(`/agent/${id}`)} />
      )}

      {/* REINSTALL INSTRUCTION MODAL */}
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
    </div>
  );
}