import { Outlet, useLocation, useParams } from "react-router-dom";
import { AppProvider, useApp } from "../../context/AppContext";
import { AgentSidebar } from "../agent/AgentSidebar";
import PanelNav from "./PanelNav";
import { fmtBytes, fmtBps } from "../../lib/utils";
import { useState } from "react";
import AddNodeModal from "./AddNodeModal";
import ReinstallModal from "./ReinstallModal";
import { fetchAPI } from "../../lib/api";
import { isUnauthorized } from "../../lib/utils";
import { useNavigate } from "react-router-dom";
import { ProvisionResponse } from "../../types";

function Shell() {
  const location = useLocation();
  const navigate = useNavigate();
  const { id } = useParams<{ id?: string }>();
  const selectedId = id || null;

  const {
    agents,
    liveMetrics,
    isSidebarExpanded,
    setIsSidebarExpanded,
    loadAgents,
    showToast,
    toast,
  } = useApp();

  const [provisioning, setProvisioning] = useState(false);
  const [showTypeSelect, setShowTypeSelect] = useState(false);
  const [provisionResult, setProvisionResult] = useState<ProvisionResponse | null>(null);
  const [filter, setFilter] = useState("");

  const filteredAgents = agents.filter(a =>
    (a.nickname || a.agent_id).toLowerCase().includes(filter.toLowerCase())
  );

  // Determine if sidebar is shown
  // We want the sidebar visible on almost every main view (Dashboard, Alerts, Settings, Management, Node details).
  // We will hide it on edit/creation routes to focus on forms.
  const hiddenSidebarRoutes = ["/dashboards/create", "/dashboards/edit", "/alerts/create", "/alerts/edit"];
  const showSidebar = !hiddenSidebarRoutes.some(route => location.pathname.startsWith(route));

  // Determine current section for Navbar
  const path = location.pathname;
  let section = "agent";
  if (path.includes("/dashboards")) section = "dashboards";
  else if (path.includes("/alerts")) section = "alerts";
  else if (path.includes("/management")) section = "management";
  else if (path.includes("/settings")) section = "settings";

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

  return (
    <div className="dashboard-container" style={{ display: 'flex', height: '100vh', background: 'var(--bg-primary)', overflow: 'hidden' }}>
      {/* Persistent Sidebar */}
      {showSidebar && (
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

      {/* Main Panel Content (includes Nav and Page Outlet) */}
      <div style={{ flex: 1, display: 'flex', flexDirection: 'column', position: 'relative', overflow: 'hidden' }}>
        {/* Navigation Bar */}
        <PanelNav section={section} />

        {/* Floating Sidebar Toggle (Desktop & Mobile) */}
        {showSidebar && !isSidebarExpanded ? (
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
              transition: 'all 0.3s ease',
              cursor: 'pointer'
            }}
          >
            <span className="material-symbols-outlined" style={{ fontSize: '28px' }}>double_arrow</span>
          </button>
        ) : showSidebar && (
          <button
            onClick={() => setIsSidebarExpanded(false)}
            className="sidebar-toggle-btn expanded"
            style={{
              position: 'absolute',
              top: '50%',
              left: '0',
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
              transition: 'all 0.3s ease',
              cursor: 'pointer'
            }}
          >
            <span className="material-symbols-outlined" style={{ fontSize: '28px' }}>keyboard_double_arrow_left</span>
          </button>
        )}

        {/* PAGE INJECTOR */}
        <main className="main-panel animate-fade-in" style={{ flex: 1, background: 'var(--bg-primary)', overflowY: 'auto' }}>
          <Outlet />
        </main>
      </div>

      {/* Global Modals */}
      <AddNodeModal
        isOpen={showTypeSelect}
        onClose={() => setShowTypeSelect(false)}
        onConfirm={handleProvision}
        confirming={provisioning}
      />

      <ReinstallModal
        data={provisionResult}
        onClose={() => setProvisionResult(null)}
        showToast={showToast}
      />

      {/* Global Toast */}
      {toast && (
        <div className="animate-fade-in" style={{ position: 'fixed', bottom: '32px', right: '32px', zIndex: 10000 }}>
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

export default function AuthenticatedLayout() {
  return (
    <AppProvider>
      <Shell />
    </AppProvider>
  );
}
