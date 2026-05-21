import React, { useState } from "react";
import { Agent, MetricKey } from "../../types";
import { getPublicURL } from "../../lib/env";

export const FEATURE_OPTIONS = [
  { id: "is_online", label: "Online Status" },
  { id: "uptime", label: "Uptime" },
  { id: "linux_version", label: "Linux Version" },
  { id: "cpu_model", label: "CPU Model" },
  { id: "cpu_cores", label: "CPU Cores" },
  { id: "ram_size", label: "Total RAM" },
  { id: "swap_size", label: "Total Swap" },
  { id: "disk_size", label: "Total Disk" },
];

export const METRIC_OPTIONS: { id: MetricKey; label: string }[] = [
  { id: "agent_cpu_usage", label: "CPU Usage" },
  { id: "agent_cpu_iowait", label: "CPU IO Wait" },
  { id: "agent_cpu_steal", label: "CPU Steal" },
  { id: "agent_ram_used", label: "RAM Usage" },
  { id: "agent_swap_used", label: "Swap Usage" },
  { id: "agent_disk_used", label: "Disk Usage" },
  { id: "agent_disk_read_bytes", label: "Disk Read" },
  { id: "agent_disk_write_bytes", label: "Disk Write" },
  { id: "agent_rx_bytes", label: "Network RX" },
  { id: "agent_tx_bytes", label: "Network TX" },
];

interface DashboardFormFieldsProps {
  title: string;
  setTitle: (v: string) => void;
  slug: string;
  setSlug: (v: string) => void;
  maxDays: number;
  setMaxDays: (v: number) => void;
  allowedFeatures: string[];
  setAllowedFeatures: React.Dispatch<React.SetStateAction<string[]>>;
  allowedFields: MetricKey[];
  setAllowedFields: React.Dispatch<React.SetStateAction<MetricKey[]>>;
  selectedAgents: Record<string, string>;
  setSelectedAgents: React.Dispatch<React.SetStateAction<Record<string, string>>>;
  availableAgents: Agent[];
  loadingAgents: boolean;
}

export default function DashboardFormFields({
  title,
  setTitle,
  slug,
  setSlug,
  maxDays,
  setMaxDays,
  allowedFeatures,
  setAllowedFeatures,
  allowedFields,
  setAllowedFields,
  selectedAgents,
  setSelectedAgents,
  availableAgents,
  loadingAgents,
}: DashboardFormFieldsProps) {
  const [agentSearch, setAgentSearch] = useState("");
  const [currentPage, setCurrentPage] = useState(1);

  const handleTitleChange = (e: React.ChangeEvent<HTMLInputElement>) => {
    const v = e.target.value;
    setTitle(v);
    setSlug(v.toLowerCase().replace(/[^a-z0-9]+/g, "-").replace(/(^-|-$)/g, ""));
  };

  const toggleFeature = (id: string) => {
    setAllowedFeatures(p => p.includes(id) ? p.filter(x => x !== id) : [...p, id]);
  };

  const toggleMetric = (id: MetricKey) => {
    setAllowedFields(p => p.includes(id) ? p.filter(x => x !== id) : [...p, id]);
  };

  const toggleAgent = (agent: Agent) => {
    setSelectedAgents(prev => {
      const next = { ...prev };
      if (next[agent.agent_id] !== undefined) {
        delete next[agent.agent_id];
      } else {
        next[agent.agent_id] = agent.nickname || "Server";
      }
      return next;
    });
  };

  const filteredAgents = availableAgents.filter((a) =>
    (a.nickname || "").toLowerCase().includes(agentSearch.toLowerCase()) ||
    a.agent_id.toLowerCase().includes(agentSearch.toLowerCase())
  );

  const allFilteredSelected = filteredAgents.length > 0 && filteredAgents.every(a => selectedAgents[a.agent_id] !== undefined);

  const handleSelectAllFiltered = () => {
    setSelectedAgents(prev => {
      const next = { ...prev };
      if (allFilteredSelected) {
        filteredAgents.forEach(a => { delete next[a.agent_id]; });
      } else {
        filteredAgents.forEach(a => { 
          if (next[a.agent_id] === undefined) {
            next[a.agent_id] = a.nickname || "Server";
          }
        });
      }
      return next;
    });
  };

  // Pagination for high-performance scale (1000+ items)
  const itemsPerPage = 20;
  const totalPages = Math.ceil(filteredAgents.length / itemsPerPage);
  const displayedAgents = filteredAgents.slice(
    (currentPage - 1) * itemsPerPage,
    currentPage * itemsPerPage
  );

  const selectedCount = Object.keys(selectedAgents).length;

  return (
    <>
      {/* GENERAL */}
      <div className="card">
        <h2 style={{ fontSize: '12px', fontWeight: '700', color: 'var(--text-secondary)', textTransform: 'uppercase', letterSpacing: '0.05em', marginBottom: '16px', display: 'flex', alignItems: 'center', gap: '8px' }}>
          <span className="material-symbols-outlined" style={{ fontSize: '16px' }}>settings</span> General
        </h2>

        <div style={{ marginBottom: '16px' }}>
          <label style={{ display: 'block', fontSize: '11px', fontWeight: '600', color: 'var(--text-secondary)', textTransform: 'uppercase', letterSpacing: '0.05em', marginBottom: '8px' }}>Title</label>
          <input type="text" required value={title} onChange={handleTitleChange} placeholder="e.g., US East Cluster" className="input-field" />
        </div>

        <div style={{ display: 'flex', gap: '16px', flexWrap: 'wrap' }}>
          <div style={{ flex: 1, minWidth: '100%' }}>
            <label style={{ display: 'block', fontSize: '11px', fontWeight: '600', color: 'var(--text-secondary)', textTransform: 'uppercase', letterSpacing: '0.05em', marginBottom: '8px' }}>URL Slug</label>
            <div className="mobile-stack" style={{ display: 'flex', border: '1px solid var(--border-color)', borderRadius: '8px', overflow: 'hidden' }}>
              <span style={{ padding: '10px 14px', background: 'var(--bg-primary)', borderRight: '1px solid var(--border-color)', fontSize: '13px', color: 'var(--text-muted)', fontFamily: 'var(--font-mono)', whiteSpace: 'nowrap', overflow: 'hidden', textOverflow: 'ellipsis' }}>{getPublicURL()}/</span>
              <input type="text" required value={slug} onChange={(e) => setSlug(e.target.value.toLowerCase().replace(/[^a-z0-9-]/g, '-'))} className="input-field" style={{ border: 'none', borderRadius: '0' }} />
            </div>
          </div>
          <div style={{ width: '100%' }}>
            <label style={{ display: 'block', fontSize: '11px', fontWeight: '600', color: 'var(--text-secondary)', textTransform: 'uppercase', letterSpacing: '0.05em', marginBottom: '8px' }}>History (days)</label>
            <select
              value={maxDays}
              onChange={(e) => setMaxDays(Number(e.target.value))}
              className="input-field"
              style={{ width: '100%', cursor: 'pointer' }}
            >
              {![1, 2, 7, 30, 90, 180, 365, 730].includes(maxDays) && (
                <option value={maxDays}>{maxDays} days</option>
              )}
              <option value={1}>1 day (Last 24h)</option>
              <option value={2}>2 days (Last 2d)</option>
              <option value={7}>7 days (Last 7d)</option>
              <option value={30}>30 days (Last 30d)</option>
              <option value={90}>90 days (Last 90d)</option>
              <option value={180}>180 days (Last 180d)</option>
              <option value={365}>365 days (Last 1y)</option>
              <option value={730}>730 days (Last 2y)</option>
            </select>
          </div>
        </div>
      </div>

      {/* ALLOWED SYSTEM INFO */}
      <div className="card">
        <h2 style={{ fontSize: '12px', fontWeight: '700', color: 'var(--text-secondary)', textTransform: 'uppercase', letterSpacing: '0.05em', marginBottom: '16px', display: 'flex', alignItems: 'center', gap: '8px' }}>
          <span className="material-symbols-outlined" style={{ fontSize: '16px' }}>memory</span> Allowed System Info
        </h2>
        <div className="grid-cards" style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fill, minmax(200px, 1fr))', gap: '12px' }}>
          {FEATURE_OPTIONS.map((f) => {
            const on = allowedFeatures.includes(f.id);
            return (
              <button type="button" key={f.id} onClick={() => toggleFeature(f.id)} style={{ display: 'flex', alignItems: 'center', gap: '12px', padding: '12px 16px', border: `1px solid ${on ? 'var(--accent-primary)' : 'var(--border-color)'}`, borderRadius: '8px', background: on ? 'rgba(99, 102, 241, 0.1)' : 'var(--bg-primary)', color: on ? 'var(--text-primary)' : 'var(--text-secondary)', transition: 'var(--transition-fast)' }}>
                <span className="material-symbols-outlined" style={{ fontSize: '20px', color: on ? 'var(--accent-primary)' : 'var(--text-muted)' }}>{on ? "check_box" : "check_box_outline_blank"}</span>
                <span style={{ fontSize: '13px', fontWeight: on ? '600' : '500' }}>{f.label}</span>
              </button>
            );
          })}
        </div>
      </div>

      {/* ALLOWED METRICS */}
      <div className="card">
        <h2 style={{ fontSize: '12px', fontWeight: '700', color: 'var(--text-secondary)', textTransform: 'uppercase', letterSpacing: '0.05em', marginBottom: '16px', display: 'flex', alignItems: 'center', gap: '8px' }}>
          <span className="material-symbols-outlined" style={{ fontSize: '16px' }}>monitoring</span> Allowed Metrics
        </h2>
        <div className="grid-cards" style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fill, minmax(200px, 1fr))', gap: '12px' }}>
          {METRIC_OPTIONS.map((m) => {
            const on = allowedFields.includes(m.id);
            return (
              <button type="button" key={m.id} onClick={() => toggleMetric(m.id)} style={{ display: 'flex', alignItems: 'center', gap: '12px', padding: '12px 16px', border: `1px solid ${on ? 'var(--accent-primary)' : 'var(--border-color)'}`, borderRadius: '8px', background: on ? 'rgba(99, 102, 241, 0.1)' : 'var(--bg-primary)', color: on ? 'var(--text-primary)' : 'var(--text-secondary)', transition: 'var(--transition-fast)' }}>
                <span className="material-symbols-outlined" style={{ fontSize: '20px', color: on ? 'var(--accent-primary)' : 'var(--text-muted)' }}>{on ? "check_box" : "check_box_outline_blank"}</span>
                <span style={{ fontSize: '13px', fontWeight: on ? '600' : '500' }}>{m.label}</span>
              </button>
            );
          })}
        </div>
      </div>

      {/* SELECT AGENTS */}
      <div className="card">
        <div className="mobile-flex-col" style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: '16px', gap: '12px' }}>
          <h2 style={{ fontSize: '12px', fontWeight: '700', color: 'var(--text-secondary)', textTransform: 'uppercase', letterSpacing: '0.05em', display: 'flex', alignItems: 'center', gap: '8px' }}>
            <span className="material-symbols-outlined" style={{ fontSize: '16px' }}>dns</span> Select Agents
            {selectedCount > 0 && (
              <span className="badge badge-online" style={{ textTransform: 'none', padding: '2px 8px', borderRadius: '12px', fontSize: '10px' }}>
                {selectedCount} Selected
              </span>
            )}
          </h2>
          <div className="mobile-w-full" style={{ display: 'flex', gap: '8px', alignItems: 'center', flexWrap: 'wrap' }}>
            <input 
              type="text" 
              placeholder="Search agents..." 
              value={agentSearch} 
              onChange={(e) => { setAgentSearch(e.target.value); setCurrentPage(1); }} 
              className="input-field mobile-w-full" 
              style={{ width: '200px', padding: '8px 12px', fontSize: '12px' }} 
            />
            <button 
              type="button" 
              onClick={handleSelectAllFiltered} 
              className="btn-secondary mobile-w-full" 
              style={{ padding: '8px 16px', fontSize: '12px', justifyContent: 'center', flexShrink: 0 }}
            >
              {allFilteredSelected ? "Deselect" : "Select All"}
            </button>
          </div>
        </div>

        {loadingAgents ? (
          <div style={{ color: 'var(--text-muted)', fontSize: '13px' }}>Loading agents...</div>
        ) : availableAgents.length === 0 ? (
          <div style={{ color: 'var(--text-muted)', fontSize: '13px' }}>No agents found. Make sure agents are reporting in.</div>
        ) : filteredAgents.length === 0 ? (
          <div style={{ color: 'var(--text-muted)', fontSize: '13px' }}>No agents match search.</div>
        ) : (
          <>
            <div style={{ display: 'flex', flexDirection: 'column', gap: '8px', maxHeight: '440px', overflowY: 'auto', paddingRight: '8px' }} className="custom-scrollbar">
              {displayedAgents.map((agent) => {
                const selected = selectedAgents[agent.agent_id] !== undefined;
                return (
                  <div key={agent.agent_id} style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', padding: '16px', border: `1px solid ${selected ? 'var(--accent-primary)' : 'var(--border-color)'}`, borderRadius: '8px', background: selected ? 'rgba(99, 102, 241, 0.05)' : 'var(--bg-primary)', transition: 'var(--transition-fast)', flexWrap: 'wrap', gap: '16px' }}>
                    <label style={{ display: 'flex', alignItems: 'center', gap: '16px', cursor: 'pointer', flex: 1 }}>
                      <span className="material-symbols-outlined" style={{ fontSize: '24px', color: selected ? 'var(--accent-primary)' : 'var(--text-muted)' }}>{selected ? "check_box" : "check_box_outline_blank"}</span>
                      <div style={{ display: 'flex', flexDirection: 'column' }}>
                        <span style={{ fontSize: '14px', fontWeight: '600', color: selected ? 'var(--text-primary)' : 'var(--text-secondary)' }}>{agent.nickname || "Unnamed"}</span>
                        <span style={{ fontSize: '11px', color: 'var(--text-muted)', fontFamily: 'var(--font-mono)', marginTop: '2px' }}>{agent.agent_id}</span>
                      </div>
                      <input type="checkbox" checked={selected} onChange={() => toggleAgent(agent)} style={{ display: 'none' }} />
                    </label>
                    {selected && (
                      <div style={{ display: 'flex', alignItems: 'center', gap: '8px' }}>
                        <span style={{ fontSize: '10px', color: 'var(--text-muted)', textTransform: 'uppercase', letterSpacing: '0.05em', fontWeight: '700' }}>Alias</span>
                        <input type="text" value={selectedAgents[agent.agent_id]} onChange={(e) => setSelectedAgents(p => ({ ...p, [agent.agent_id]: e.target.value }))} placeholder="Public alias" className="input-field" style={{ padding: '6px 12px', fontSize: '12px' }} />
                      </div>
                    )}
                  </div>
                );
              })}
            </div>

            {/* Pagination Controls */}
            {totalPages > 1 && (
              <div className="mobile-flex-col" style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginTop: '20px', paddingTop: '16px', borderTop: '1px solid var(--border-color)', gap: '16px' }}>
                <span style={{ fontSize: '13px', color: 'var(--text-secondary)' }}>
                  Showing <strong>{Math.min((currentPage - 1) * itemsPerPage + 1, filteredAgents.length)}</strong> to <strong>{Math.min(currentPage * itemsPerPage, filteredAgents.length)}</strong> of <strong>{filteredAgents.length}</strong> agents
                </span>
                <div className="mobile-w-full" style={{ display: 'flex', alignItems: 'center', justifyContent: 'center', gap: '6px' }}>
                  <button
                    type="button"
                    disabled={currentPage === 1}
                    onClick={() => setCurrentPage(1)}
                    className="btn-secondary"
                    style={{ padding: '8px', minWidth: '36px', display: 'flex', alignItems: 'center', justifyContent: 'center' }}
                    title="First Page"
                  >
                    <span className="material-symbols-outlined" style={{ fontSize: '18px' }}>first_page</span>
                  </button>
                  <button
                    type="button"
                    disabled={currentPage === 1}
                    onClick={() => setCurrentPage(p => Math.max(p - 1, 1))}
                    className="btn-secondary"
                    style={{ padding: '8px', minWidth: '36px', display: 'flex', alignItems: 'center', justifyContent: 'center' }}
                    title="Previous Page"
                  >
                    <span className="material-symbols-outlined" style={{ fontSize: '18px' }}>chevron_left</span>
                  </button>
                  <span style={{ fontSize: '13px', color: 'var(--text-primary)', margin: '0 8px', fontWeight: '600' }}>
                    Page {currentPage} of {totalPages}
                  </span>
                  <button
                    type="button"
                    disabled={currentPage === totalPages}
                    onClick={() => setCurrentPage(p => Math.min(p + 1, totalPages))}
                    className="btn-secondary"
                    style={{ padding: '8px', minWidth: '36px', display: 'flex', alignItems: 'center', justifyContent: 'center' }}
                    title="Next Page"
                  >
                    <span className="material-symbols-outlined" style={{ fontSize: '18px' }}>chevron_right</span>
                  </button>
                  <button
                    type="button"
                    disabled={currentPage === totalPages}
                    onClick={() => setCurrentPage(totalPages)}
                    className="btn-secondary"
                    style={{ padding: '8px', minWidth: '36px', display: 'flex', alignItems: 'center', justifyContent: 'center' }}
                    title="Last Page"
                  >
                    <span className="material-symbols-outlined" style={{ fontSize: '18px' }}>last_page</span>
                  </button>
                </div>
              </div>
            )}
          </>
        )}
      </div>
    </>
  );
}
