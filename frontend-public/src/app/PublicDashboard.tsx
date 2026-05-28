import { useEffect, useState, useRef } from "react";
import { useSearchParams, useParams, useNavigate, Link } from "react-router-dom";
import {
  AreaChart, Area, XAxis, YAxis, Tooltip,
  ResponsiveContainer, CartesianGrid,
} from "recharts";
import { fetchAPI } from "../lib/api";
import { getPublicPath } from "../lib/env";
import { MetricResponse } from "../types";
import { UsageBar } from "../lib/UsageBar";

import { MetricKey } from "../types";

import { PublicAgentDetail, fmtUptime, fmtBps, fmtBytes, TIME_RANGES } from "./PublicAgentDetail";
import { PublicAgent, DashboardResponse } from "../types";
import { TotalTelemetryPanel } from "../lib/TotalTelemetryPanel";

const PULSE_CSS = `
  @keyframes pulse-update {
    0% { background-color: rgba(255, 255, 255, 0.05); }
    50% { background-color: rgba(255, 255, 255, 0.15); }
    100% { background-color: rgba(255, 255, 255, 0.05); }
  }
  .pulse-flash {
    animation: pulse-update 0.4s ease-out;
  }
  @media (max-width: 768px) {
    .mobile-hide { display: none !important; }
    .mobile-full { width: 100% !important; min-width: 100% !important; }
    .mobile-stack { flex-direction: column !important; align-items: flex-start !important; gap: 16px !important; }
    .mobile-grid-1 { grid-template-columns: 1fr !important; }
    .mobile-text-lg { font-size: 24px !important; }
    .mobile-p-sm { padding: 20px !important; }
    .mobile-gap-sm { gap: 16px !important; }
    .mobile-h-tall { height: 300px !important; }
    .mobile-m-0 { margin: 0 !important; }
    .mobile-px-4 { padding-left: 16px !important; padding-right: 16px !important; }
  }

`;

// Components moved to PublicAgentDetail.tsx

// ── Dashboard Layout ───────────────────────────────────────────────

function DashboardContent() {
  const [searchParams] = useSearchParams();
  const { slug: pathSlug, agentId: pathAgentId } = useParams();
  const navigate = useNavigate();
  const slug = pathSlug || searchParams.get("slug");
  const base = getPublicPath().replace(/\/$/, "");

  const [title, setTitle] = useState("");
  const [dashboard_id, setDashboardId] = useState<string | null>(null);
  const [agents, setAgents] = useState<PublicAgent[]>([]);
  const agentsRef = useRef<PublicAgent[]>([]);

  useEffect(() => {
    agentsRef.current = agents;
  }, [agents]);

  const selectedId = pathAgentId || null;
  const [allowedMetrics, setAllowedMetrics] = useState<string[]>([]);
  const [maxDays, setMaxDays] = useState<number>(30);

  const [searchQuery, setSearchQuery] = useState("");
  const [viewMode, setViewMode] = useState<"grid" | "list">(() => {
    const saved = localStorage.getItem("certainstats_public_view_mode");
    return (saved === "list" || saved === "grid") ? saved as "grid" | "list" : "grid";
  });
  const [gridDensity, setGridDensity] = useState<"detailed" | "simplified">(() => {
    const saved = localStorage.getItem("certainstats_public_grid_density");
    return (saved === "simplified" || saved === "detailed") ? saved as "detailed" | "simplified" : "detailed";
  });

  useEffect(() => {
    localStorage.setItem("certainstats_public_view_mode", viewMode);
  }, [viewMode]);

  useEffect(() => {
    localStorage.setItem("certainstats_public_grid_density", gridDensity);
  }, [gridDensity]);
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);

  // Theme Management
  const [theme, setTheme] = useState(() => {
    const saved = localStorage.getItem("certainstats_public_theme");
    if (saved) return saved;
    return window.matchMedia("(prefers-color-scheme: dark)").matches ? "dark" : "light";
  });

  useEffect(() => {
    document.documentElement.setAttribute("data-theme", theme);
    localStorage.setItem("certainstats_public_theme", theme);
  }, [theme]);

  const toggleTheme = () => setTheme(prev => prev === "dark" ? "light" : "dark");

  // Scroll to top on view change
  useEffect(() => {
    window.scrollTo({ top: 0, behavior: 'instant' });
  }, [selectedId]);

  useEffect(() => {
    if (!slug) { setError("No dashboard slug provided."); setLoading(false); return; }

    fetchAPI<DashboardResponse>(`/api/public/dashboard/${slug}`)
      .then((data) => {
        setTitle(data.title);
        setDashboardId(data.dashboard_id);
        setMaxDays(data.max_days || 30);
        const fetchedAgents = data.agents || [];
        setAgents(fetchedAgents);
        setAllowedMetrics(data.allowed_metrics || []);
      })
      .catch((e) => setError(e.message || "Dashboard not found"))
      .finally(() => setLoading(false));
  }, [slug]);

  // Live Metrics state
  const [liveMetrics, setLiveMetrics] = useState<Record<string, any>>({});

  useEffect(() => {
    if (!dashboard_id) return;

    const protocol = window.location.protocol === "https:" ? "wss:" : "ws:";
    // In dev mode, we need to point to the backend (8080)
    const host = import.meta.env.MODE === "development" ? "localhost:8080" : window.location.host;
    const publicPath = getPublicPath().replace(/\/$/, "");
    const wsUrl = `${protocol}//${host}${publicPath}/api/public/ws/${dashboard_id}`;

    let socket: WebSocket | null = new WebSocket(wsUrl);

    const prevSnaps = { current: {} as Record<string, any> };

    socket.onmessage = (event) => {
      try {
        const msg = JSON.parse(event.data);
        if (msg.type === "agent_update") {
          const snaps = msg.data || {};
          const enrichedSnaps: Record<string, any> = {};

          for (const id in snaps) {
            const snap = snaps[id];
            enrichedSnaps[id] = { ...snap };
            enrichedSnaps[id].RXBps = snap.RXBps || 0;
            enrichedSnaps[id].TXBps = snap.TXBps || 0;

            // Calculate disk usage percentage on-the-fly
            if (snap.DiskTotalBytes > 0) {
              enrichedSnaps[id].DiskUsagePercent = (snap.DiskUsedBytes / snap.DiskTotalBytes) * 100;
            } else {
              enrichedSnaps[id].DiskUsagePercent = 0;
            }

            // Calculate RAM and Swap usage percentages on-the-fly
            const agent = agentsRef.current.find(a => a.public_id === id);
            if (agent) {
              const rSize = agent.ram_size || 0;
              const sSize = agent.swap_size || 0;
              if (rSize > 0) {
                enrichedSnaps[id].RAMUsagePercent = (snap.RAMUsedBytes / rSize) * 100;
              } else {
                enrichedSnaps[id].RAMUsagePercent = 0;
              }
              if (sSize > 0) {
                enrichedSnaps[id].RAMSwapUsagePercent = (snap.RAMSwapUsedBytes / sSize) * 100;
              } else {
                enrichedSnaps[id].RAMSwapUsagePercent = 0;
              }
            }
          }

          prevSnaps.current = enrichedSnaps;
          setLiveMetrics(enrichedSnaps);

          setAgents((prev: PublicAgent[]) => prev.map((a: PublicAgent) => {
            const snap = snaps[a.public_id];
            return {
              ...a,
              is_online: !!snap,
              uptime: snap?.Uptime !== undefined ? snap.Uptime : a.uptime,
              linux_version: snap?.LinuxVersion || a.linux_version,
              cpu_model: snap?.CpuModel || a.cpu_model,
            };
          }));
        }
      } catch (err) {
        console.error("WS Parse Error:", err);
      }
    };

    socket.onclose = () => {
      console.log("WS Closed. Reconnecting...");
      setTimeout(() => {
        // Reconnection logic could go here
      }, 5000);
    };

    return () => {
      socket?.close();
    };
  }, [dashboard_id]);

  const [hours, setHours] = useState<number>(() => {
    const saved = localStorage.getItem("certainstats_active_hours");
    if (saved) {
      const v = parseInt(saved, 10);
      if (TIME_RANGES.some((r: any) => r.value === v)) return v;
    }
    return 6;
  });

  useEffect(() => {
    localStorage.setItem("certainstats_active_hours", hours.toString());
  }, [hours]);

  // Navigation and back-button history flows are natively driven by standard routing Link components

  const active = agents.find((a) => a.public_id === selectedId) ?? null;
  const isAllOnline = agents.length > 0 && agents.every((a) => a.is_online);

  const filteredAgents = agents.filter(a =>
    a.display_name.toLowerCase().includes(searchQuery.toLowerCase()) ||
    (a.cpu_model && a.cpu_model.toLowerCase().includes(searchQuery.toLowerCase())) ||
    (a.linux_version && a.linux_version.toLowerCase().includes(searchQuery.toLowerCase()))
  );
  if (loading) {
    return (
      <div style={{ height: '100vh', width: '100%', display: 'flex', alignItems: 'center', justifyContent: 'center', background: 'var(--bg-primary)' }}>
        <div style={{ display: 'flex', flexDirection: 'column', alignItems: 'center', gap: '16px', color: 'var(--text-muted)' }}>
          <span className="material-symbols-outlined" style={{ fontSize: '32px', animation: 'spin 1s linear infinite' }}>data_usage</span>
          <p style={{ fontSize: '14px', fontWeight: '500', letterSpacing: '0.02em' }}>Loading Telemetry...</p>
        </div>
      </div>
    );
  }

  if (error) {
    return (
      <div style={{ height: '100vh', width: '100%', display: 'flex', flexDirection: 'column', alignItems: 'center', justifyContent: 'center', background: 'var(--bg-primary)', padding: '24px', textAlign: 'center' }}>
        <div style={{ width: '64px', height: '64px', borderRadius: '50%', background: 'rgba(239, 68, 68, 0.1)', border: '1px solid rgba(239, 68, 68, 0.3)', display: 'flex', alignItems: 'center', justifyContent: 'center', marginBottom: '24px' }}>
          <span className="material-symbols-outlined" style={{ fontSize: '32px', color: 'var(--status-offline)' }}>warning</span>
        </div>
        <h1 className="font-display" style={{ fontSize: '24px', fontWeight: '600', marginBottom: '8px' }}>Dashboard Not Found</h1>
        <p style={{ fontSize: '14px', color: 'var(--text-secondary)', maxWidth: '400px' }}>{error}</p>
      </div>
    );
  }

  return (
    <div style={{ minHeight: '100dvh', display: 'flex', flexDirection: 'column', background: 'var(--bg-primary)' }}>

      {/* ── Navbar ── */}
      <style>{PULSE_CSS}</style>
      <header className="mobile-px-4" style={{
        flexShrink: 0,
        minHeight: '80px',
        background: 'var(--glass-bg)',
        backdropFilter: 'blur(16px)',
        borderBottom: '1px solid var(--border-color)',
        display: 'flex',
        alignItems: 'center',
        justifyContent: 'center',
        zIndex: 50,
        position: 'sticky',
        top: 0
      }}>
        <div style={{ width: '100%', maxWidth: '1200px', display: 'flex', alignItems: 'center', justifyContent: 'space-between', padding: '0 32px' }}>
          <Link
            to={`/${slug}`}
            className="header-nav-link"
            style={{
              display: 'flex',
              alignItems: 'center',
              gap: '20px',
              cursor: 'pointer',
              padding: '8px',
              margin: '-8px',
              borderRadius: '16px',
              transition: 'all 0.2s ease',
              userSelect: 'none',
              textDecoration: 'none'
            }}
          >
            <div style={{
              width: '40px',
              height: '40px',
              background: 'linear-gradient(135deg, #6366f1, #8b5cf6)',
              borderRadius: '10px',
              display: 'flex',
              alignItems: 'center',
              justifyContent: 'center',
              boxShadow: '0 4px 14px rgba(99, 102, 241, 0.45)',
              flexShrink: 0
            }}>
              <span className="material-symbols-outlined" style={{ fontSize: '22px', color: '#fff' }}>hub</span>
            </div>
            <div style={{ display: 'flex', flexDirection: 'column', gap: '2px', minWidth: 0 }}>
              <h1 className="font-display" style={{ fontSize: 'clamp(16px, 4vw, 22px)', fontWeight: '800', color: 'var(--text-primary)', letterSpacing: '-0.02em', maxWidth: '300px', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
                {title}
              </h1>
              <div style={{ display: 'flex', alignItems: 'center', gap: '8px' }}>
                <div style={{
                  width: '8px',
                  height: '8px',
                  borderRadius: '50%',
                  background: isAllOnline ? 'var(--status-online)' : 'var(--status-offline)',
                  boxShadow: isAllOnline ? '0 0 8px var(--status-online)' : 'none'
                }} />
                <p style={{ fontSize: '10px', fontWeight: '800', color: 'var(--text-muted)', textTransform: 'uppercase', letterSpacing: '0.08em' }}>
                  {agents.length === 0 ? "No Nodes Configured" : isAllOnline ? "Operational" : "Degraded Performance"}
                </p>
              </div>
            </div>
          </Link>

          <div style={{ display: 'flex', alignItems: 'center', gap: '16px' }}>
            <button
              onClick={toggleTheme}
              style={{ width: '40px', height: '40px', borderRadius: '50%', display: 'flex', alignItems: 'center', justifyContent: 'center', background: 'var(--bg-secondary)', border: '1px solid var(--border-color)', color: 'var(--text-secondary)' }}
            >
              <span className="material-symbols-outlined" style={{ fontSize: '20px' }}>
                {theme === 'dark' ? 'light_mode' : 'dark_mode'}
              </span>
            </button>
          </div>
        </div>
      </header>

      {/* ── Main Layout (Overview / Detail) ── */}
      <div style={{ flex: 1, background: 'var(--bg-primary)' }}>
        {!active ? (
          <div className="animate-fade-in mobile-px-4" style={{ padding: '40px 32px', maxWidth: '1200px', margin: '0 auto' }}>
            {/* Redesigned Header Row */}
            <div style={{ display: 'flex', flexDirection: 'column', gap: '24px', marginBottom: '32px' }}>
              <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', flexWrap: 'wrap', gap: '20px' }}>
                <div className="flex items-center gap-4">
                  <h2 className="font-display mobile-text-lg" style={{ fontSize: '36px', fontWeight: '900', letterSpacing: '-0.04em', color: 'var(--text-primary)' }}>Agent Hub</h2>
                  <div style={{ background: 'var(--accent-glow)', color: 'var(--accent-primary)', padding: '4px 12px', borderRadius: '20px', fontSize: '14px', fontWeight: '800', marginTop: '4px' }}>
                    {filteredAgents.length} Nodes
                  </div>
                </div>
              </div>

              {/* Unified Toolbar */}
              <div className="mobile-stack" style={{
                display: 'flex',
                alignItems: 'center',
                gap: '12px',
                background: 'var(--bg-secondary)',
                padding: '8px',
                borderRadius: '16px',
                border: '1px solid var(--border-color)',
                boxShadow: 'var(--card-shadow)'
              }}>
                <div className="mobile-full" style={{ position: 'relative', flex: '1' }}>
                  <span className="material-symbols-outlined" style={{ position: 'absolute', left: '16px', top: '50%', transform: 'translateY(-50%)', fontSize: '20px', color: 'var(--text-muted)', pointerEvents: 'none' }}>search</span>
                  <input
                    type="text"
                    placeholder="Find node by name..."
                    value={searchQuery}
                    onChange={(e) => setSearchQuery(e.target.value)}
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
                      title="Grid View"
                      style={{
                        display: 'flex', padding: '6px 10px', borderRadius: '8px',
                        background: viewMode === "grid" ? 'var(--bg-secondary)' : 'transparent',
                        color: viewMode === "grid" ? 'var(--accent-primary)' : 'var(--text-muted)',
                        transition: 'var(--transition-fast)',
                        boxShadow: viewMode === "grid" ? '0 2px 8px rgba(0,0,0,0.1)' : 'none'
                      }}
                    >
                      <span className="material-symbols-outlined" style={{ fontSize: '18px' }}>grid_view</span>
                    </button>
                    <button
                      onClick={() => setViewMode("list")}
                      title="List View"
                      style={{
                        display: 'flex', padding: '6px 10px', borderRadius: '8px',
                        background: viewMode === "list" ? 'var(--bg-secondary)' : 'transparent',
                        color: viewMode === "list" ? 'var(--accent-primary)' : 'var(--text-muted)',
                        transition: 'var(--transition-fast)',
                        boxShadow: viewMode === "list" ? '0 2px 8px rgba(0,0,0,0.1)' : 'none'
                      }}
                    >
                      <span className="material-symbols-outlined" style={{ fontSize: '18px' }}>view_list</span>
                    </button>
                  </div>
                </div>
              </div>
            </div>

            <TotalTelemetryPanel
              filteredAgents={filteredAgents}
              liveMetrics={liveMetrics}
              allowedMetrics={allowedMetrics}
              fmtBytes={fmtBytes}
              fmtBps={fmtBps}
              isPublic={true}
            />

            {filteredAgents.length === 0 ? (
              <div style={{ textAlign: 'center', color: 'var(--text-muted)', marginTop: '48px' }}>
                <span className="material-symbols-outlined" style={{ fontSize: '48px', opacity: 0.3, marginBottom: '16px' }}>search_off</span>
                <p>No nodes match your search.</p>
              </div>
            ) : viewMode === "grid" ? (
              <div className="grid-cards" style={{
                display: 'grid',
                gridTemplateColumns: gridDensity === 'detailed'
                  ? 'repeat(auto-fill, minmax(min(100%, 360px), 1fr))'
                  : 'repeat(auto-fill, minmax(min(100%, 320px), 1fr))',
                gap: '24px'
              }}>
                {filteredAgents.map(a => {
                  const snap = liveMetrics[a.public_id];
                  return (
                    <Link
                      key={`${a.public_id}-${snap?.Timestamp || 'initial'}`}
                      to={`/${slug}/${a.public_id}`}
                      className={`card animate-fade-in ${snap ? 'pulse-flash' : ''}`}
                      style={{
                        display: 'flex',
                        flexDirection: 'column',
                        textAlign: 'left',
                        padding: '28px',
                        cursor: 'pointer',
                        borderRadius: '24px',
                        background: 'var(--bg-secondary)',
                        border: '1px solid var(--border-color)',
                        boxShadow: 'var(--card-shadow)',
                        transition: 'all 0.3s cubic-bezier(0.4, 0, 0.2, 1)',
                        position: 'relative',
                        overflow: 'hidden',
                        textDecoration: 'none'
                      }}
                      onMouseOver={(e) => {
                        e.currentTarget.style.transform = 'translateY(-4px)';
                        e.currentTarget.style.borderColor = 'var(--accent-primary)';
                        e.currentTarget.style.boxShadow = '0 12px 30px rgba(0,0,0,0.2)';
                      }}
                      onMouseOut={(e) => {
                        e.currentTarget.style.transform = 'translateY(0)';
                        e.currentTarget.style.borderColor = 'var(--border-color)';
                        e.currentTarget.style.boxShadow = 'var(--card-shadow)';
                      }}
                    >
                      <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', marginBottom: '20px', gap: '12px' }}>
                        <div style={{ display: 'flex', alignItems: 'center', gap: '12px', minWidth: 0 }}>
                          <div style={{ position: 'relative', display: 'flex', flexShrink: 0 }}>
                            <div className={a.is_online ? "status-dot-online" : "status-dot-offline"} style={{ width: '10px', height: '10px' }} />
                            {a.is_online && <div style={{ position: 'absolute', top: 0, left: 0, right: 0, bottom: 0, borderRadius: '50%', background: 'var(--status-online)', filter: 'blur(4px)', opacity: 0.6 }} />}
                          </div>
                          <h3 className="font-display" style={{ fontSize: '18px', fontWeight: '800', color: 'var(--text-primary)', whiteSpace: 'nowrap', overflow: 'hidden', textOverflow: 'ellipsis', letterSpacing: '-0.01em' }}>
                            {a.display_name}
                          </h3>
                        </div>
                        <span style={{ fontSize: '10px', color: 'var(--text-muted)', fontFamily: 'var(--font-mono)', fontWeight: '600' }}>{a.public_id.substring(0, 8)}</span>
                      </div>

                      {/* Visual Load Bars */}
                      <div style={{ display: 'flex', flexDirection: 'column', gap: gridDensity === "detailed" ? '12px' : '6px', marginBottom: '24px' }}>
                        {gridDensity === "detailed" && (
                          <div style={{ display: 'flex', flexDirection: 'column', gap: '6px', marginBottom: '8px', fontSize: '12px', color: 'var(--text-primary)', opacity: 0.8, letterSpacing: '0.01em' }}>
                            {Boolean(a.cpu_model) && (
                              <div style={{ whiteSpace: 'nowrap', overflow: 'hidden', textOverflow: 'ellipsis', fontWeight: '700' }}>{a.cpu_model}</div>
                            )}
                            {Boolean(a.linux_version) && (
                              <div style={{ fontFamily: 'var(--font-mono)', fontSize: '10px', opacity: 0.9, whiteSpace: 'nowrap', overflow: 'hidden', textOverflow: 'ellipsis' }}>{a.linux_version}</div>
                            )}
                          </div>
                        )}
                        {snap && a.is_online ? (
                          <>
                            {allowedMetrics.some(m => m.startsWith('agent_cpu')) && (
                              <UsageBar
                                label="CPU Status"
                                compact={gridDensity === "simplified"}
                                segments={[
                                  { label: 'Used', value: snap.CPUUsagePercent, color: '#3b82f6', hide: !allowedMetrics.includes('agent_cpu_usage') },
                                  { label: 'IO Wait', value: snap.CPUIOWaitPercent, color: '#fb923c', hide: !allowedMetrics.includes('agent_cpu_iowait') },
                                  { label: 'Steal', value: snap.CPUStealPercent, color: '#ef4444', hide: !allowedMetrics.includes('agent_cpu_steal') }
                                ].filter(s => !s.hide)}
                              />
                            )}
                            {allowedMetrics.some(m => m.startsWith('agent_ram')) && (
                              <UsageBar
                                label="Total RAM"
                                compact={gridDensity === "simplified"}
                                segments={[
                                  { label: 'Used', value: snap?.RAMUsagePercent || 0, color: '#14b8a6', displayValue: fmtBytes(snap?.RAMUsedBytes), totalDisplay: a.ram_size ? fmtBytes(a.ram_size) : undefined, hide: !allowedMetrics.includes('agent_ram_used') },
                                  { label: 'Swap', value: snap?.RAMSwapUsagePercent || 0, color: '#4b5563', displayValue: fmtBytes(snap?.RAMSwapUsedBytes), totalDisplay: a.swap_size ? fmtBytes(a.swap_size) : undefined, hide: !allowedMetrics.includes('agent_swap_used') }
                                ].filter(s => !s.hide)}
                              />
                            )}
                            {allowedMetrics.includes('agent_disk_used') && (
                              <UsageBar
                                label="Root Disk"
                                compact={gridDensity === "simplified"}
                                segments={[
                                  { label: 'Used', value: (snap.DiskUsedBytes / (a.disk_size || snap.DiskUsedBytes || 1)) * 100, color: '#a855f7', displayValue: fmtBytes(snap.DiskUsedBytes), totalDisplay: a.disk_size ? fmtBytes(a.disk_size) : undefined }
                                ]}
                              />
                            )}
                            {(allowedMetrics.includes('agent_rx_bytes') || allowedMetrics.includes('agent_tx_bytes')) && (
                              <UsageBar
                                label="Network IO"
                                compact={gridDensity === "simplified"}
                                unit=""
                                segments={[
                                  { label: 'RX', value: (snap?.RXBps + snap?.TXBps > 0) ? (snap.RXBps / (snap.RXBps + snap.TXBps)) * 100 : 0, color: '#1e40af', displayValue: fmtBps(snap?.RXBps || 0), hide: !allowedMetrics.includes('agent_rx_bytes') },
                                  { label: 'TX', value: (snap?.RXBps + snap?.TXBps > 0) ? (snap.TXBps / (snap.RXBps + snap.TXBps)) * 100 : 0, color: '#7e22ce', displayValue: fmtBps(snap?.TXBps || 0), hide: !allowedMetrics.includes('agent_tx_bytes') }
                                ].filter(s => !s.hide)}
                              />
                            )}
                            {(allowedMetrics.includes('agent_disk_read_bytes') || allowedMetrics.includes('agent_disk_write_bytes')) && (
                              <UsageBar
                                label="Disk IO"
                                compact={gridDensity === "simplified"}
                                unit=""
                                segments={[
                                  { label: 'Read', value: (snap?.DiskReadBps + snap?.DiskWriteBps > 0) ? (snap.DiskReadBps / (snap.DiskReadBps + snap.DiskWriteBps)) * 100 : 0, color: '#f59e0b', displayValue: fmtBps(snap?.DiskReadBps || 0), hide: !allowedMetrics.includes('agent_disk_read_bytes') },
                                  { label: 'Write', value: (snap?.DiskReadBps + snap?.DiskWriteBps > 0) ? (snap.DiskWriteBps / (snap.DiskReadBps + snap.DiskWriteBps)) * 100 : 0, color: '#ef4444', displayValue: fmtBps(snap?.DiskWriteBps || 0), hide: !allowedMetrics.includes('agent_disk_write_bytes') }
                                ].filter(s => !s.hide)}
                              />
                            )}
                          </>
                        ) : (
                          <div style={{ display: 'flex', flexDirection: 'column', gap: '10px', opacity: 0.1 }}>
                            <div style={{ height: '6px', background: 'var(--text-muted)', borderRadius: '3px' }} />
                            <div style={{ height: '6px', background: 'var(--text-muted)', borderRadius: '3px' }} />
                            <div style={{ height: '6px', background: 'var(--text-muted)', borderRadius: '3px' }} />
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
                        {a.linux_version ? (
                          <div style={{ display: 'flex', alignItems: 'center', gap: '6px' }}>
                            <span className="material-symbols-outlined" style={{ fontSize: '16px' }}>terminal</span>
                            {a.linux_version.split(' ')[0] || "Linux"}
                          </div>
                        ) : <div />}
                      </div>
                    </Link>
                  );
                })}
              </div>
            ) : (
              <div className="card animate-fade-in" style={{ padding: '0', overflowX: 'auto', border: '1px solid var(--border-color)', borderRadius: '24px', background: 'var(--bg-secondary)', boxShadow: 'var(--card-shadow)' }}>
                <table style={{ width: '100%', minWidth: '1200px', borderCollapse: 'collapse', textAlign: 'left' }}>
                  <thead>
                    <tr style={{ background: 'var(--bg-primary)', borderBottom: '1px solid var(--border-color)' }}>
                      <th style={{ padding: '20px 28px', fontSize: '11px', textTransform: 'uppercase', letterSpacing: '0.1em', color: 'var(--text-muted)', fontWeight: '800' }}>Status</th>
                      <th style={{ padding: '20px 28px', fontSize: '11px', textTransform: 'uppercase', letterSpacing: '0.1em', color: 'var(--text-muted)', fontWeight: '800' }}>Node</th>
                      <th style={{ padding: '20px 28px', fontSize: '11px', textTransform: 'uppercase', letterSpacing: '0.1em', color: 'var(--text-muted)', fontWeight: '800' }}>Current Load</th>
                      {agents.some(a => a.cpu_model) && <th style={{ padding: '20px 28px', fontSize: '11px', textTransform: 'uppercase', letterSpacing: '0.1em', color: 'var(--text-muted)', fontWeight: '800' }}>CPU</th>}
                      {agents.some(a => a.linux_version) && <th style={{ padding: '20px 28px', fontSize: '11px', textTransform: 'uppercase', letterSpacing: '0.1em', color: 'var(--text-muted)', fontWeight: '800' }}>OS</th>}
                      {agents.some(a => a.ram_size) && <th style={{ padding: '20px 28px', fontSize: '11px', textTransform: 'uppercase', letterSpacing: '0.1em', color: 'var(--text-muted)', fontWeight: '800' }}>RAM</th>}
                      {agents.some(a => a.disk_size) && <th style={{ padding: '20px 28px', fontSize: '11px', textTransform: 'uppercase', letterSpacing: '0.1em', color: 'var(--text-muted)', fontWeight: '800' }}>Disk</th>}
                      {agents.some(a => a.uptime) && <th style={{ padding: '20px 28px', fontSize: '11px', textTransform: 'uppercase', letterSpacing: '0.1em', color: 'var(--text-muted)', fontWeight: '800' }}>Uptime</th>}
                    </tr>
                  </thead>
                  <tbody>
                    {filteredAgents.map(a => {
                      const snap = liveMetrics[a.public_id];
                      return (
                        <tr
                          key={`${a.public_id}-${snap?.Timestamp || 'initial'}`}
                          onClick={(e) => {
                            if (e.ctrlKey || e.metaKey) {
                              window.open(`${window.location.origin}${base}/${slug}/${a.public_id}`, '_blank');
                            } else {
                              navigate(`/${slug}/${a.public_id}`);
                            }
                          }}
                          onAuxClick={(e) => {
                            if (e.button === 1) { // Middle click
                              window.open(`${window.location.origin}${base}/${slug}/${a.public_id}`, '_blank');
                            }
                          }}
                          className={snap ? "pulse-flash" : ""}
                          style={{ borderBottom: '1px solid var(--border-color)', cursor: 'pointer', transition: 'background 0.2s ease' }}
                          onMouseOver={(e) => e.currentTarget.style.background = 'var(--bg-primary)'}
                          onMouseOut={(e) => e.currentTarget.style.background = 'transparent'}
                        >
                          <td style={{ padding: '20px 28px', verticalAlign: 'middle' }}>
                            <div style={{ position: 'relative', display: 'flex', width: '10px' }}>
                              <div className={a.is_online ? "status-dot-online" : "status-dot-offline"} />
                              {a.is_online && <div style={{ position: 'absolute', top: 0, left: 0, right: 0, bottom: 0, borderRadius: '50%', background: 'var(--status-online)', filter: 'blur(3px)', opacity: 0.4 }} />}
                            </div>
                          </td>
                          <td style={{ padding: '20px 28px', verticalAlign: 'middle' }}>
                            <div style={{ display: 'flex', flexDirection: 'column', gap: '2px' }}>
                              <span style={{ fontSize: '15px', fontWeight: '800', color: 'var(--text-primary)', letterSpacing: '-0.01em' }}>{a.display_name}</span>
                              <span style={{ fontSize: '11px', color: 'var(--text-muted)', fontFamily: 'var(--font-mono)' }}>{a.public_id.substring(0, 12)}</span>
                            </div>
                          </td>
                          <td style={{ padding: '20px 28px', minWidth: '240px', verticalAlign: 'middle' }}>
                            {snap && a.is_online ? (
                              <div style={{ display: 'flex', flexDirection: 'column', gap: '8px' }}>
                                {allowedMetrics.some(m => m.startsWith('agent_cpu')) && (
                                  <UsageBar
                                    label="CPU"
                                    compact
                                    segments={[
                                      { label: 'Used', value: snap.CPUUsagePercent, color: '#3b82f6', hide: !allowedMetrics.includes('agent_cpu_usage') },
                                      { label: 'IO Wait', value: snap.CPUIOWaitPercent, color: '#fb923c', hide: !allowedMetrics.includes('agent_cpu_iowait') },
                                      { label: 'Steal', value: snap.CPUStealPercent, color: '#ef4444', hide: !allowedMetrics.includes('agent_cpu_steal') }
                                    ].filter(s => !s.hide)}
                                  />
                                )}
                                {allowedMetrics.some(m => m.startsWith('agent_ram')) && (
                                  <UsageBar
                                    label="RAM"
                                    compact
                                    segments={[
                                      { label: 'Used', value: snap?.RAMUsagePercent || 0, color: '#14b8a6', displayValue: fmtBytes(snap?.RAMUsedBytes), totalDisplay: a.ram_size ? fmtBytes(a.ram_size) : undefined, hide: !allowedMetrics.includes('agent_ram_used') },
                                      { label: 'Swap', value: snap?.RAMSwapUsagePercent || 0, color: '#4b5563', displayValue: fmtBytes(snap?.RAMSwapUsedBytes), totalDisplay: a.swap_size ? fmtBytes(a.swap_size) : undefined, hide: !allowedMetrics.includes('agent_swap_used') }
                                    ].filter(s => !s.hide)}
                                  />
                                )}
                                {allowedMetrics.includes('agent_disk_used') && (
                                  <UsageBar
                                    label="DISK"
                                    compact
                                    unit=""
                                    segments={[
                                      { label: 'Used', value: snap?.DiskUsagePercent || 0, color: '#a855f7', displayValue: fmtBytes(snap?.DiskUsedBytes), totalDisplay: a.disk_size ? fmtBytes(a.disk_size) : undefined }
                                    ]}
                                  />
                                )}
                                {(allowedMetrics.includes('agent_rx_bytes') || allowedMetrics.includes('agent_tx_bytes')) && (
                                  <UsageBar
                                    label="NETWORK"
                                    compact
                                    unit=""
                                    segments={[
                                      { label: 'RX', value: (snap?.RXBps + snap?.TXBps > 0) ? (snap.RXBps / (snap.RXBps + snap.TXBps)) * 100 : 0, color: '#1e40af', displayValue: fmtBps(snap?.RXBps || 0), hide: !allowedMetrics.includes('agent_rx_bytes') },
                                      { label: 'TX', value: (snap?.RXBps + snap?.TXBps > 0) ? (snap.TXBps / (snap.RXBps + snap.TXBps)) * 100 : 0, color: '#7e22ce', displayValue: fmtBps(snap?.TXBps || 0), hide: !allowedMetrics.includes('agent_tx_bytes') }
                                    ].filter(s => !s.hide)}
                                  />
                                )}
                                {(allowedMetrics.includes('agent_disk_read_bytes') || allowedMetrics.includes('agent_disk_write_bytes')) && (
                                  <UsageBar
                                    label="DISK IO"
                                    compact
                                    unit=""
                                    segments={[
                                      { label: 'Read', value: (snap?.DiskReadBps + snap?.DiskWriteBps > 0) ? (snap.DiskReadBps / (snap.DiskReadBps + snap.DiskWriteBps)) * 100 : 0, color: '#f59e0b', displayValue: fmtBps(snap?.DiskReadBps || 0), hide: !allowedMetrics.includes('agent_disk_read_bytes') },
                                      { label: 'Write', value: (snap?.DiskReadBps + snap?.DiskWriteBps > 0) ? (snap.DiskWriteBps / (snap.DiskReadBps + snap.DiskWriteBps)) * 100 : 0, color: '#ef4444', displayValue: fmtBps(snap?.DiskWriteBps || 0), hide: !allowedMetrics.includes('agent_disk_write_bytes') }
                                    ].filter(s => !s.hide)}
                                  />
                                )}
                              </div>
                            ) : <span style={{ color: 'var(--text-muted)', fontSize: '12px', fontWeight: '600' }}>-</span>}
                          </td>
                          {agents.some(x => x.cpu_model) && (
                            <td style={{ padding: '20px 28px', verticalAlign: 'middle' }}>
                              {a.cpu_model ? (
                                <div style={{ display: 'flex', flexDirection: 'column', gap: '4px' }}>
                                  <span style={{ fontSize: '13px', fontWeight: '800', color: 'var(--text-primary)', whiteSpace: 'nowrap', overflow: 'hidden', textOverflow: 'ellipsis', maxWidth: '220px', letterSpacing: '-0.01em' }}>{a.cpu_model}</span>
                                  <span style={{ fontSize: '11px', fontWeight: '900', color: 'var(--text-primary)', opacity: 0.8, letterSpacing: '0.02em' }}>{a.cpu_cores || "?"} CORES</span>
                                </div>
                              ) : <span style={{ color: 'var(--text-muted)' }}>-</span>}
                            </td>
                          )}
                          {agents.some(x => x.ram_size) && (
                            <td style={{ padding: '20px 28px', verticalAlign: 'middle' }}>
                              {a.ram_size ? (
                                <div style={{ display: 'flex', flexDirection: 'column', gap: '4px' }}>
                                  <span style={{ fontSize: '14px', fontWeight: '800', color: 'var(--text-primary)' }}>{fmtBytes(a.ram_size)}</span>
                                  {Boolean(a.swap_size && a.swap_size > 0) && <span style={{ fontSize: '10px', color: 'var(--text-muted)', fontWeight: '700' }}>+{fmtBytes(a.swap_size)} SWAP</span>}
                                </div>
                              ) : <span style={{ color: 'var(--text-muted)' }}>-</span>}
                            </td>
                          )}
                          {agents.some(x => x.disk_size) && (
                            <td style={{ padding: '20px 28px', verticalAlign: 'middle' }}>
                              {a.disk_size ? (
                                <span style={{ fontSize: '14px', fontWeight: '800', color: 'var(--text-primary)' }}>{fmtBytes(a.disk_size)}</span>
                              ) : <span style={{ color: 'var(--text-muted)' }}>-</span>}
                            </td>
                          )}
                          {agents.some(x => x.uptime) && (
                            <td style={{ padding: '20px 28px', verticalAlign: 'middle' }}>
                              {a.uptime ? (
                                <div style={{ display: 'flex', alignItems: 'center', gap: '8px', fontSize: '13px', color: 'var(--text-primary)', fontWeight: '800' }}>
                                  <span className="material-symbols-outlined" style={{ fontSize: '18px', color: 'var(--text-muted)' }}>schedule</span>
                                  <span>{fmtUptime(a.uptime)}</span>
                                </div>
                              ) : <span style={{ color: 'var(--text-muted)' }}>-</span>}
                            </td>
                          )}
                          {agents.some(x => x.linux_version) && (
                            <td style={{ padding: '20px 28px', verticalAlign: 'middle' }}>
                              {a.linux_version ? (
                                <div style={{ display: 'flex', alignItems: 'center', gap: '8px', fontSize: '13px', color: 'var(--text-secondary)', fontWeight: '600' }}>
                                  <span className="material-symbols-outlined" style={{ fontSize: '18px', color: 'var(--text-muted)' }}>terminal</span>
                                  <span>{a.linux_version.split(' ')[0] || "LINUX"}</span>
                                </div>
                              ) : <span style={{ color: 'var(--text-muted)' }}>-</span>}
                            </td>
                          )}
                        </tr>
                      );
                    })}
                  </tbody>
                </table>
              </div>
            )}
          </div>
        ) : (
          <PublicAgentDetail
            active={active}
            onClose={() => navigate(`/${slug}`)}
            hours={hours}
            setHours={setHours}
            liveMetrics={liveMetrics}
            allowedMetrics={allowedMetrics}
            maxDays={maxDays}
            dashboardID={dashboard_id!}
            TIME_RANGES={TIME_RANGES}
          />
        )}
      </div>
    </div>
  );
}

export default function PublicDashboard() {
  return (
    <DashboardContent />
  );
}