import React, { useEffect, useState, useRef } from "react";
import { createPortal } from "react-dom";
import { useNavigate } from "react-router-dom";
import { fetchAPI } from "../../lib/api";
import { Agent, MetricResponse, AgentSnapshot } from "../../types";
import { TelemetryChart } from "./TelemetryChart";

// ── Types ──────────────────────────────────────────────────────────────────

type TabKey = "cpu" | "ram" | "disk" | "disk_io" | "net";
type HourKey = 1 | 6 | 12 | 24 | 48 | 168 | 720 | 2160 | 4320 | 8760 | 17520 | 43800 | 87600 | 876000;

interface SeriesCfg {
  metric: string;
  label: string;
  color: string;
  fill?: boolean;
}

// ── Props ──────────────────────────────────────────────────────────────────

interface AgentDetailProps {
  agent: Agent;
  selectedId: string | null;
  isSidebarExpanded: boolean;
  setIsSidebarExpanded: (val: boolean) => void;
  setAgents: React.Dispatch<React.SetStateAction<Agent[]>>;
  activeHours: HourKey;
  setActiveHours: (val: HourKey) => void;
  liveMetrics: Record<string, AgentSnapshot>;
  setRevokeTarget: (a: Agent) => void;
  onInstall: (id: string) => void;
  onRename: (a: Agent) => void;

  fmtUptime: (s: number) => string;
  fmtBytes: (b: number) => string;
  fmtBps: (v: number) => string;
  TIME_RANGES: { label: string; value: HourKey }[];
  TABS: Record<TabKey, { label: string; series: SeriesCfg[]; fmt: (v: number) => string }>;
}

// ── Local Components ───────────────────────────────────────────────────────

function NicknameEditor({ agent, onSaved }: { agent: Agent; onSaved: (nick: string) => void }) {
  const [editing, setEditing] = useState(false);
  const [value, setValue] = useState(agent.nickname || agent.agent_id);
  const [saving, setSaving] = useState(false);
  const inputRef = useRef<HTMLInputElement>(null);

  const startEdit = () => {
    setValue(agent.nickname || agent.agent_id);
    setEditing(true);
  };

  const cancel = () => {
    setEditing(false);
    setValue(agent.nickname || agent.agent_id);
  };

  const save = async () => {
    if (value === agent.nickname) return cancel();
    setSaving(true);
    try {
      await fetchAPI("/api/agent", {
        method: "PUT",
        body: JSON.stringify({ agent_id: agent.agent_id, nickname: value })
      });
      onSaved(value);
      setEditing(false);
    } catch (err) {
      alert("Failed to save nickname");
    } finally {
      setSaving(false);
    }
  };

  if (editing) {
    return (
      <div className="flex items-center gap-2">
        <input
          ref={inputRef}
          value={value}
          onChange={(e) => setValue(e.target.value)}
          onKeyDown={(e) => { if (e.key === "Enter") save(); if (e.key === "Escape") cancel(); }}
          maxLength={64}
          className="input-field font-display text-lg font-semibold"
          style={{ width: '220px', padding: '6px 10px' }}
          autoFocus
        />
        <button onClick={save} disabled={saving} className="btn-primary" style={{ padding: '6px 12px' }}>
          {saving ? "…" : "Save"}
        </button>
        <button onClick={cancel} className="btn-secondary" style={{ padding: '6px 10px' }}>✕</button>
      </div>
    );
  }

  return (
    <div className="flex items-center gap-2 group cursor-pointer" onClick={startEdit} title="Edit node name" style={{ cursor: 'pointer' }}>
      <h1 className="font-display font-bold text-primary mobile-text-lg" style={{ fontSize: '32px', letterSpacing: '-0.02em' }}>
        {agent.nickname || agent.agent_id}
      </h1>
      <span className="material-symbols-outlined text-muted" style={{ fontSize: '18px', opacity: 0, transition: 'opacity 0.2s', marginLeft: '4px' }} onMouseOver={(e) => e.currentTarget.style.opacity = '1'} onMouseOut={(e) => e.currentTarget.style.opacity = '0'} ref={(el) => { if (el) el.style.opacity = '0.5'; }}>
        edit
      </span>
    </div>
  );
}

function InlineNotes({ agent, onSaved }: { agent: Agent; onSaved: (note: string) => void }) {
  const [editing, setEditing] = useState(false);
  const [value, setValue] = useState(agent.note || "");
  const [saving, setSaving] = useState(false);

  useEffect(() => {
    setValue(agent.note || "");
  }, [agent.note]);

  const handleSave = async () => {
    setSaving(true);
    try {
      await fetchAPI("/api/agent", {
        method: "PUT",
        body: JSON.stringify({ agent_id: agent.agent_id, note: value })
      });
      onSaved(value);
      setEditing(false);
    } catch (err) {
      alert("Failed to save note");
    } finally {
      setSaving(false);
    }
  };

  if (editing) {
    return (
      <div style={{ display: 'flex', alignItems: 'center', gap: '8px', marginLeft: '4px' }}>
        <div style={{ width: '1px', height: '14px', background: 'rgba(255,255,255,0.15)', marginRight: '4px' }} />
        <input
          value={value}
          onChange={(e) => setValue(e.target.value)}
          onKeyDown={(e) => { if (e.key === "Enter") handleSave(); if (e.key === "Escape") setEditing(false); }}
          placeholder="Notes..."
          className="input-field"
          style={{ padding: '4px 8px', fontSize: '12px', height: '24px', width: '160px' }}
          autoFocus
        />
        <button 
          onClick={handleSave} 
          disabled={saving} 
          className="btn-primary" 
          style={{ padding: '2px 8px', fontSize: '10px', height: '24px', display: 'flex', alignItems: 'center', cursor: 'pointer' }}
        >
          {saving ? "…" : "Save"}
        </button>
        <button 
          onClick={() => setEditing(false)} 
          className="btn-secondary" 
          style={{ padding: '2px 6px', fontSize: '10px', height: '24px', display: 'flex', alignItems: 'center', cursor: 'pointer' }}
        >
          ✕
        </button>
      </div>
    );
  }

  return (
    <div style={{ display: 'flex', alignItems: 'center', gap: '6px', fontSize: '13px', marginLeft: '4px' }}>
      <div style={{ width: '1px', height: '14px', background: 'rgba(255,255,255,0.15)', marginRight: '4px' }} />
      <span 
        onClick={() => setEditing(true)}
        style={{ 
          color: 'var(--text-primary)', 
          borderBottom: '1px dashed rgba(255,255,255,0.4)', 
          cursor: 'pointer',
          maxWidth: '240px',
          overflow: 'hidden',
          textOverflow: 'ellipsis',
          whiteSpace: 'nowrap',
          transition: 'color 0.2s',
        }}
        onMouseOver={(e) => e.currentTarget.style.color = 'var(--accent-primary)'}
        onMouseOut={(e) => e.currentTarget.style.color = 'var(--text-primary)'}
        title={agent.note ? `${agent.note} (Click to edit)` : "No notes yet. Click to add private notes."}
      >
        {agent.note || "No notes"}
      </span>
      <span style={{ color: 'var(--text-muted)', fontSize: '12px' }}>notes</span>
    </div>
  );
}

function MetricChart({
  agentId,
  tab,
  hours,
  livePoint,
  maxValue,
  TABS,
  customRange,
  onZoom,
}: {
  agentId: string;
  tab: TabKey;
  hours: HourKey;
  livePoint?: AgentSnapshot;
  maxValue?: number;
  TABS: Record<TabKey, { label: string; series: SeriesCfg[]; fmt: (v: number) => string }>;
  customRange: { start: number; end: number } | null;
  onZoom: (start: number, end: number) => void;
}) {
  type ChartPoint = { time: number } & Record<string, number | null>;
  const [data, setData] = useState<ChartPoint[]>([]);
  const [loading, setLoading] = useState(true);
  const lastProcessedTs = useRef<string | null>(null);
  const cfg = TABS[tab];

  // States for Recharts brush selection
  const [refAreaLeft, setRefAreaLeft] = useState<number | null>(null);
  const [refAreaRight, setRefAreaRight] = useState<number | null>(null);

  useEffect(() => {
    setLoading(true);
    setData([]);

    const queryParams = customRange
      ? `start=${customRange.start}&end=${customRange.end}`
      : `hours=${hours}`;

    Promise.all(
      cfg.series.map((s) =>
        fetchAPI<MetricResponse>(`/api/metrics?agent_id=${agentId}&metric=${s.metric}&${queryParams}`)
          .catch(() => ({ metric: s.metric, series: [] as any[] }))
      )
    ).then((results) => {
      const map = new Map<number, ChartPoint>();

      results.forEach((res, i) => {
        let pts: [number, number][] = res.series && res.series[0] ? res.series[0].data : [];
        const s = cfg.series[i];

        if (tab === "net" || tab === "disk_io") {
          // Values are "bytes in this interval" (delta) — convert to B/s
          pts = pts.map(([ts, v]: [number, number], j: number) => {
            const prevTs = j > 0 ? pts[j - 1][0] : null;
            if (prevTs === null) {
              const nextTs = pts.length > 1 ? pts[1][0] : ts + 60000;
              const dt = (nextTs - ts) / 1000;
              return [ts, dt > 0 ? Math.max(0, v / dt) : 0] as [number, number];
            }
            const dt = (ts - prevTs) / 1000;
            const rate = dt > 0 ? Math.max(0, v / dt) : 0;
            return [ts, rate] as [number, number];
          });
        }

        pts.forEach(([ts, v]: [number, number]) => {
          const existing: ChartPoint = map.get(ts) ?? { time: ts };
          existing[s.label] = v;
          map.set(ts, existing);
        });
      });

      setData(Array.from(map.values()).sort((a, b) => a.time - b.time));
      setLoading(false);
    });
  }, [agentId, tab, hours, TABS, cfg.series, customRange]);


  useEffect(() => {
    if (loading) {
      lastProcessedTs.current = null;
      return;
    }
    if (customRange) return; // Freeze live updates while viewing custom zoomed timeframe
    if (!livePoint) return;
    if (livePoint.Timestamp === lastProcessedTs.current) return;
    lastProcessedTs.current = livePoint.Timestamp;

    const durationMs = hours * 3600 * 1000;
    const stepMs = Math.max(60000, durationMs / 1000);

    const ts = new Date(livePoint.Timestamp).getTime();
    setData(prev => {
      if (prev.length === 0) return prev;
      const last = prev[prev.length - 1];

      // If the incoming timestamp is within stepMs of the last point,
      // update the last point in-place to prevent visual distortion!
      const updateInPlace = (ts - last.time) < stepMs;

      const newPt: ChartPoint = updateInPlace ? { ...last } : { time: ts };
      let hasData = false;

      cfg.series.forEach((s) => {
        let val: number | undefined;
        if (s.metric === "agent_cpu_usage") val = livePoint.CPUUsagePercent;
        if (s.metric === "agent_cpu_iowait") val = livePoint.CPUIOWaitPercent;
        if (s.metric === "agent_cpu_steal") val = livePoint.CPUStealPercent;
        if (s.metric === "agent_ram_used") val = livePoint.RAMUsedBytes;
        if (s.metric === "agent_swap_used") val = livePoint.RAMSwapUsedBytes;
        if (s.metric === "agent_disk_used") val = livePoint.DiskUsedBytes;
        if (s.metric === "agent_rx_bytes") val = livePoint.RXBps;
        if (s.metric === "agent_tx_bytes") val = livePoint.TXBps;
        if (s.metric === "agent_disk_read_bytes") val = livePoint.DiskReadBps;
        if (s.metric === "agent_disk_write_bytes") val = livePoint.DiskWriteBps;

        if (val !== undefined) {
          newPt[s.label] = val;
          hasData = true;
        }
      });

      if (!hasData) return prev;

      if (updateInPlace) {
        return [...prev.slice(0, -1), newPt];
      } else {
        return [...prev, newPt].slice(-1010);
      }
    });
  }, [livePoint, loading, hours, cfg.series, customRange]);

  const tickFmt = (ms: number) => {
    const d = new Date(ms);
    const months = ["Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"];
    if (hours <= 24) {
      return `${String(d.getHours()).padStart(2, "0")}:${String(d.getMinutes()).padStart(2, "0")}`;
    }
    if (hours <= 8760) {
      return `${months[d.getMonth()]} ${d.getDate()}`;
    }
    return `${months[d.getMonth()]} ${d.getDate()}, ${d.getFullYear()}`;
  };

  const lastPoint = data[data.length - 1] || {};

  return (
    <div className="card flex-col gap-3 animate-fade-in" style={{ position: 'relative', overflow: 'hidden' }}>
      <div className="flex justify-between items-center" style={{ marginBottom: '12px' }}>
        <h2 className="text-sm font-bold text-primary uppercase tracking-wider" style={{ letterSpacing: '0.05em' }}>{cfg.label}</h2>
        <div className="flex gap-4 text-[11px] font-mono">
          {cfg.series.map((s: any) => {
            const val = lastPoint[s.label];
            const displayVal = val != null ? cfg.fmt(val) : "-";
            return (
              <div key={s.label} className="flex items-center gap-2">
                <span style={{ width: '8px', height: '8px', borderRadius: '50%', backgroundColor: s.color }} />
                <span style={{ color: 'var(--text-secondary)', fontWeight: '600' }}>{s.label} <span style={{ color: 'var(--text-primary)' }}>{displayVal}</span></span>
              </div>
            );
          })}
        </div>
      </div>

      <div style={{ height: '200px', width: '100%', marginTop: '8px', marginLeft: '-16px', touchAction: 'none' }}>
        {loading ? (
          <div className="h-full flex items-center justify-center text-sm text-muted">
            <span className="material-symbols-outlined" style={{ animation: 'spin 1s linear infinite' }}>sync</span>
          </div>
        ) : data.length === 0 ? (
          <div className="h-full flex items-center justify-center text-sm text-muted" style={{ border: '1px dashed var(--border-color)', borderRadius: '8px', margin: '0 16px' }}>
            No data
          </div>
        ) : (
          <TelemetryChart
            data={data}
            series={cfg.series}
            maxValue={maxValue}
            fmt={cfg.fmt}
            refAreaLeft={refAreaLeft}
            refAreaRight={refAreaRight}
            setRefAreaLeft={setRefAreaLeft}
            setRefAreaRight={setRefAreaRight}
            onZoom={onZoom}
          />
        )}
      </div>
    </div>
  );
}

function HwCard({ label, value, unit, icon, colorHex, progress }: { label: string; value: string | React.ReactNode; unit: string; icon: string; colorHex: string; progress?: number }) {
  return (
    <div className="card flex-col gap-2 animate-fade-in" style={{ padding: '20px' }}>
      <div className="flex justify-between items-center text-muted">
        <span className="text-[10px] uppercase font-black tracking-[0.15em]" style={{ opacity: 0.6 }}>{label}</span>
        <span className="material-symbols-outlined" style={{ fontSize: '18px', color: colorHex, opacity: 0.8 }}>{icon}</span>
      </div>
      <div className="flex items-baseline gap-2 mt-2">
        <span className="text-2xl font-bold font-mono" style={{ lineHeight: '1', color: 'var(--text-primary)' }}>{value}</span>
        <span className="text-xs font-bold font-mono text-muted">{unit}</span>
      </div>
      <div style={{ width: '100%', height: '4px', background: 'rgba(255,255,255,0.02)', borderRadius: '2px', marginTop: '14px', overflow: 'hidden', border: '1px solid rgba(255,255,255,0.04)' }}>
        <div style={{
          height: '100%',
          width: progress !== undefined ? `${Math.min(100, Math.max(0, progress))}%` : '100%',
          backgroundColor: colorHex,
          borderRadius: '2px',
          boxShadow: `0 0 10px ${colorHex}30`,
          transition: 'width 1.2s cubic-bezier(0.4, 0, 0.2, 1)'
        }} />
      </div>
    </div>
  );
}

// ── Agent Detail Component ───────────────────────────────────────────────

export function AgentDetail({
  agent,
  selectedId,
  isSidebarExpanded,
  setIsSidebarExpanded,
  setAgents,
  activeHours,
  setActiveHours,
  liveMetrics,
  setRevokeTarget,
  fmtUptime,
  fmtBytes,
  fmtBps,
  TIME_RANGES,
  TABS,
  onInstall,
  onRename
}: AgentDetailProps) {
  const navigate = useNavigate();
  const snap = liveMetrics[agent.agent_id];

  const [customRange, setCustomRange] = useState<{ start: number; end: number } | null>(null);

  const [timePickerOpen, setTimePickerOpen] = useState(false);
  const [coords, setCoords] = useState({ top: 0, left: 0, openUp: false });
  const [customStartStr, setCustomStartStr] = useState("");
  const [customEndStr, setCustomEndStr] = useState("");
  const dropdownRef = useRef<HTMLDivElement>(null);

  // Helper to format Date to datetime-local string (YYYY-MM-DDTHH:mm)
  const toDatetimeLocal = (date: Date) => {
    const ten = (i: number) => (i < 10 ? '0' : '') + i;
    const YYYY = date.getFullYear();
    const MM = ten(date.getMonth() + 1);
    const DD = ten(date.getDate());
    const HH = ten(date.getHours());
    const mm = ten(date.getMinutes());
    return `${YYYY}-${MM}-${DD}T${HH}:${mm}`;
  };

  // Sync inputs when customRange or activeHours changes
  useEffect(() => {
    if (customRange) {
      setCustomStartStr(toDatetimeLocal(new Date(customRange.start)));
      setCustomEndStr(toDatetimeLocal(new Date(customRange.end)));
    } else {
      const now = new Date();
      const start = new Date(now.getTime() - activeHours * 60 * 60 * 1000);
      setCustomStartStr(toDatetimeLocal(start));
      setCustomEndStr(toDatetimeLocal(now));
    }
  }, [customRange, activeHours]);

  // Click outside and scroll listener for portal dropdown
  useEffect(() => {
    const handleOutsideClick = (e: MouseEvent) => {
      if (dropdownRef.current && !dropdownRef.current.contains(e.target as Node)) {
        const portalDropdown = document.querySelector('.portal-time-picker-dropdown');
        if (portalDropdown && portalDropdown.contains(e.target as Node)) {
          return;
        }
        setTimePickerOpen(false);
      }
    };
    const handleScroll = () => {
      setTimePickerOpen(false);
    };
    if (timePickerOpen) {
      document.addEventListener("mousedown", handleOutsideClick);
      window.addEventListener("scroll", handleScroll, true);
    }
    return () => {
      document.removeEventListener("mousedown", handleOutsideClick);
      window.removeEventListener("scroll", handleScroll, true);
    };
  }, [timePickerOpen]);

  const handleApplyCustom = () => {
    const startMs = new Date(customStartStr).getTime();
    const endMs = new Date(customEndStr).getTime();
    if (!isNaN(startMs) && !isNaN(endMs) && startMs < endMs) {
      setCustomRange({ start: startMs, end: endMs });
      setTimePickerOpen(false);
    }
  };

  const handleHoursChange = (h: HourKey) => {
    setActiveHours(h);
    setCustomRange(null); // Reset custom zoom when manually selecting standard view
  };

  React.useLayoutEffect(() => {
    const main = document.querySelector('.main-panel');
    if (main) {
      main.scrollTo({ top: 0, behavior: 'instant' });
    }
  }, [agent.agent_id]);

  return (
    <div className="flex-col mobile-gap-sm animate-fade-in" style={{ maxWidth: '1200px', margin: '0 auto', gap: '24px', display: 'flex', padding: '0 8px', width: '100%' }}>

      {/* ── HEADER NAVIGATION ────────────────────────────────── */}
      <button
        onClick={() => navigate("/")}
        className="btn-secondary"
        style={{
          alignSelf: 'flex-start',
          display: 'flex',
          alignItems: 'center',
          gap: '8px',
          fontSize: '11px',
          fontWeight: '800',
          textTransform: 'uppercase',
          letterSpacing: '0.08em',
          padding: '8px 16px',
          borderRadius: '20px',
          background: 'rgba(255,255,255,0.03)',
          border: '1px solid var(--border-color)',
          transition: 'var(--transition-fast)'
        }}
      >
        <span className="material-symbols-outlined" style={{ fontSize: '16px' }}>arrow_back</span>
        Back to Overview
      </button>

      {/* ── HERO BANNER ────────────────────────────────────────── */}
      <div className="detail-hero">

        {/* Title row */}
        <div className="flex items-center justify-between mobile-flex-col gap-6 w-full">
          <div className="flex items-center gap-4 flex-wrap">
            <NicknameEditor agent={agent} onSaved={(nick) => {
              setAgents(prev => prev.map(a => a.agent_id === agent.agent_id ? { ...a, nickname: nick } : a));
            }} />
            <div className="flex items-center gap-2">
              <span className={`status-pill ${agent.is_online ? 'online' : 'offline'}`}>
                <span className="status-dot-pulse" />
                {agent.is_online ? "online" : "offline"}
              </span>
              <span className="type-badge">
                {agent.agent_type || "unknown"}
              </span>
              <InlineNotes agent={agent} onSaved={(note) => {
                setAgents(prev => prev.map(a => a.agent_id === agent.agent_id ? { ...a, note: note } : a));
              }} />
            </div>
          </div>

          <div className="action-btn-group mobile-w-full">
            <button
              onClick={() => onInstall(agent.agent_id)}
              className="btn-secondary"
              style={{ padding: '8px 16px', fontSize: '12px', fontWeight: '700', display: 'flex', alignItems: 'center', gap: '8px', borderRadius: '10px', height: '36px' }}
            >
              <span className="material-symbols-outlined" style={{ fontSize: '18px' }}>terminal</span>
              Reinstall
            </button>

            <div style={{ position: 'relative' }} ref={dropdownRef}>
              <button
                onClick={(e) => {
                  if (!timePickerOpen) {
                    const rect = e.currentTarget.getBoundingClientRect();
                    const menuHeight = 460;
                    const menuWidth = 320;
                    const spaceBelow = window.innerHeight - rect.bottom;
                    const openUp = spaceBelow < menuHeight && rect.top > menuHeight;

                    let left = rect.right - menuWidth;
                    if (left < 10) {
                      left = Math.max(10, rect.left);
                    }
                    if (left + menuWidth > window.innerWidth - 10) {
                      left = window.innerWidth - menuWidth - 10;
                    }

                    setCoords({
                      top: openUp ? rect.top - 8 : rect.bottom + 8,
                      left,
                      openUp
                    });
                  }
                  setTimePickerOpen(!timePickerOpen);
                }}
                className="btn-secondary"
                style={{
                  padding: '8px 16px',
                  fontSize: '12px',
                  fontWeight: '700',
                  height: '36px',
                  borderRadius: '10px',
                  display: 'flex',
                  alignItems: 'center',
                  gap: '8px',
                  background: customRange ? 'rgba(99, 102, 241, 0.1)' : 'var(--bg-secondary)',
                  border: customRange ? '1px solid var(--accent-primary)' : '1px solid var(--border-color)',
                  color: customRange ? 'var(--accent-primary)' : 'var(--text-primary)',
                  cursor: 'pointer'
                }}
              >
                <span className="material-symbols-outlined" style={{ fontSize: '18px' }}>
                  {customRange ? 'calendar_month' : 'history'}
                </span>
                {customRange ? (
                  <span>Custom Range</span>
                ) : (
                  <span>Last {TIME_RANGES.find(r => r.value === activeHours)?.label || `${activeHours}h`}</span>
                )}
                <span className="material-symbols-outlined" style={{ fontSize: '16px', transition: 'transform 0.2s', transform: timePickerOpen ? 'rotate(180deg)' : 'none' }}>
                  expand_more
                </span>
              </button>

              {timePickerOpen && createPortal(
                <div
                  className="glass-panel animate-fade-in portal-time-picker-dropdown"
                  style={{
                    position: 'fixed',
                    top: coords.openUp ? 'auto' : `${coords.top}px`,
                    bottom: coords.openUp ? `${window.innerHeight - coords.top}px` : 'auto',
                    left: `${coords.left}px`,
                    width: '320px',
                    borderRadius: '16px',
                    padding: '20px',
                    zIndex: 9999,
                    boxShadow: 'var(--card-shadow)',
                    border: '1px solid var(--border-color)',
                    background: 'var(--bg-secondary)',
                    backdropFilter: 'blur(20px)',
                    display: 'flex',
                    flexDirection: 'column',
                    gap: '16px',
                  }}
                  onClick={e => e.stopPropagation()}
                >
                  {/* Quick Ranges */}
                  <div>
                    <h4 style={{ fontSize: '10px', fontWeight: '800', color: 'var(--text-muted)', textTransform: 'uppercase', letterSpacing: '0.08em', marginBottom: '10px' }}>
                      Quick Ranges
                    </h4>
                    <div style={{ display: 'grid', gridTemplateColumns: 'repeat(3, 1fr)', gap: '8px' }}>
                      {TIME_RANGES.map((r) => (
                        <button
                          key={r.value}
                          onClick={() => {
                            handleHoursChange(r.value);
                            setTimePickerOpen(false);
                          }}
                          style={{
                            padding: '6px',
                            fontSize: '11px',
                            fontWeight: '600',
                            borderRadius: '8px',
                            textAlign: 'center',
                            background: !customRange && activeHours === r.value ? 'rgba(59, 130, 246, 0.15)' : 'var(--bar-bg)',
                            border: !customRange && activeHours === r.value ? '1px solid var(--accent-primary)' : '1px solid var(--border-color)',
                            color: !customRange && activeHours === r.value ? 'var(--accent-primary)' : 'var(--text-secondary)',
                            cursor: 'pointer',
                          }}
                          className="hover:scale-105"
                        >
                          {r.label}
                        </button>
                      ))}
                    </div>
                  </div>

                  <div style={{ height: '1px', background: 'var(--border-color)' }} />

                  {/* Custom Range Selector */}
                  <div style={{ display: 'flex', flexDirection: 'column', gap: '12px' }}>
                    <h4 style={{ fontSize: '10px', fontWeight: '800', color: 'var(--text-muted)', textTransform: 'uppercase', letterSpacing: '0.08em' }}>
                      Custom Range
                    </h4>
                    <div style={{ display: 'flex', flexDirection: 'column', gap: '8px' }}>
                      <div style={{ display: 'flex', flexDirection: 'column', gap: '4px' }}>
                        <span style={{ fontSize: '10px', color: 'var(--text-secondary)', fontWeight: '600' }}>Start Date/Time</span>
                        <input
                          type="datetime-local"
                          value={customStartStr}
                          onChange={(e) => setCustomStartStr(e.target.value)}
                          className="input-field"
                          style={{ fontSize: '11px', padding: '6px 8px', height: '32px', width: '100%' }}
                        />
                      </div>
                      <div style={{ display: 'flex', flexDirection: 'column', gap: '4px' }}>
                        <span style={{ fontSize: '10px', color: 'var(--text-secondary)', fontWeight: '600' }}>End Date/Time</span>
                        <input
                          type="datetime-local"
                          value={customEndStr}
                          onChange={(e) => setCustomEndStr(e.target.value)}
                          className="input-field"
                          style={{ fontSize: '11px', padding: '6px 8px', height: '32px', width: '100%' }}
                        />
                      </div>
                    </div>
                    <div style={{ display: 'flex', gap: '8px', marginTop: '4px' }}>
                      {customRange && (
                        <button
                          onClick={() => {
                            setCustomRange(null);
                            setTimePickerOpen(false);
                          }}
                          className="btn-secondary"
                          style={{ flex: 1, padding: '6px 12px', fontSize: '11px', fontWeight: '700', borderRadius: '8px', height: '32px', cursor: 'pointer' }}
                        >
                          Clear
                        </button>
                      )}
                      <button
                        onClick={handleApplyCustom}
                        className="btn-primary"
                        style={{ flex: 2, padding: '6px 12px', fontSize: '11px', fontWeight: '700', borderRadius: '8px', height: '32px', cursor: 'pointer' }}
                      >
                        Apply Range
                      </button>
                    </div>
                  </div>
                </div>,
                document.body
              )}
            </div>

            <button
              onClick={() => setRevokeTarget(agent)}
              className="btn-icon-only danger"
              title="Revoke Node"
            >
              <span className="material-symbols-outlined" style={{ fontSize: '20px' }}>delete_forever</span>
            </button>
          </div>
        </div>

        {/* Specifications Tiles */}
        <div className="spec-tile-grid">

          <div className="spec-tile">
            <div className="spec-tile-icon" style={{ background: 'var(--spec-indigo-bg)', borderColor: 'var(--spec-indigo-border)', color: 'var(--spec-indigo-color)' }}>
              <span className="material-symbols-outlined" style={{ fontSize: '18px' }}>fingerprint</span>
            </div>
            <div className="spec-tile-content">
              <span className="spec-tile-label">Node ID</span>
              <span className="spec-tile-value mono">{agent.agent_id}</span>
            </div>
          </div>

          <div className="spec-tile">
            <div className="spec-tile-icon" style={{ background: 'var(--spec-green-bg)', borderColor: 'var(--spec-green-border)', color: 'var(--spec-green-color)' }}>
              <span className="material-symbols-outlined" style={{ fontSize: '18px' }}>schedule</span>
            </div>
            <div className="spec-tile-content">
              <span className="spec-tile-label">Uptime</span>
              <span className="spec-tile-value">{fmtUptime(agent.uptime)}</span>
            </div>
          </div>

          {agent.linux_version && agent.linux_version !== "Pending connection..." && (
            <div className="spec-tile">
              <div className="spec-tile-icon" style={{ background: 'var(--spec-blue-bg)', borderColor: 'var(--spec-blue-border)', color: 'var(--spec-blue-color)' }}>
                <span className="material-symbols-outlined" style={{ fontSize: '18px' }}>terminal</span>
              </div>
              <div className="spec-tile-content">
                <span className="spec-tile-label">OS Kernel</span>
                <span className="spec-tile-value mono" title={agent.linux_version}>{agent.linux_version}</span>
              </div>
            </div>
          )}

          {agent.cpu_model && agent.cpu_model !== "Waiting for data..." && (
            <div className="spec-tile">
              <div className="spec-tile-icon" style={{ background: 'var(--spec-amber-bg)', borderColor: 'var(--spec-amber-border)', color: 'var(--spec-amber-color)' }}>
                <span className="material-symbols-outlined" style={{ fontSize: '18px' }}>developer_board</span>
              </div>
              <div className="spec-tile-content">
                <span className="spec-tile-label">CPU Architecture</span>
                <span className="spec-tile-value mono" title={agent.cpu_model}>{agent.cpu_model}</span>
              </div>
            </div>
          )}
        </div>

        {/* Odometers Row */}
        {(((agent.net?.total_rx_bytes !== undefined) || (agent.net?.total_tx_bytes !== undefined)) || (agent.disks && agent.disks.length > 0)) && (
          <div className="odometer-grid">
            {((agent.net?.total_rx_bytes !== undefined) || (agent.net?.total_tx_bytes !== undefined)) && (
              <div className="odometer-tile">
                <div className="odometer-label-group">
                  <span className="material-symbols-outlined" style={{ fontSize: '16px', color: 'var(--accent-primary)' }}>download_for_offline</span>
                  <span>Network Traffic</span>
                </div>
                <div className="odometer-values">
                  {agent.net?.total_rx_bytes !== undefined && <span>↓ {fmtBytes(agent.net.total_rx_bytes)}</span>}
                  {agent.net?.total_rx_bytes !== undefined && agent.net?.total_tx_bytes !== undefined && <span style={{ opacity: 0.3 }}>/</span>}
                  {agent.net?.total_tx_bytes !== undefined && <span>↑ {fmtBytes(agent.net.total_tx_bytes)}</span>}
                </div>
              </div>
            )}

            {agent.disks?.map((d) => (
              <div key={d.path} className="odometer-tile">
                <div className="odometer-label-group">
                  <span className="material-symbols-outlined" style={{ fontSize: '16px', color: 'var(--status-offline)' }}>save</span>
                  <span>Disk Read/Write ({d.path})</span>
                </div>
                <div className="odometer-values">
                  {d.read_bytes !== undefined && <span>R: {fmtBytes(d.read_bytes)}</span>}
                  {d.read_bytes !== undefined && d.write_bytes !== undefined && <span style={{ opacity: 0.3 }}>/</span>}
                  {d.write_bytes !== undefined && <span>W: {fmtBytes(d.write_bytes)}</span>}
                </div>
              </div>
            ))}
          </div>
        )}
      </div>

      {/* Hardware Grid */}
      <div className="grid-cards mobile-grid-1" style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(220px, 1fr))', gap: '16px', marginTop: '8px' }}>
        <HwCard
          label="CPU Cores"
          value={agent.cpu_cores || "-"}
          unit="vCPU"
          icon="memory"
          colorHex="var(--accent-primary)"
          progress={snap?.CPUUsagePercent}
        />
        <HwCard
          label="Total RAM"
          value={fmtBytes(agent.ram_size).split(' ')[0]}
          unit={fmtBytes(agent.ram_size).split(' ')[1] || "GB"}
          icon="memory_alt"
          colorHex="var(--status-online)"
          progress={snap && agent.ram_size > 0 ? (snap.RAMUsedBytes / agent.ram_size) * 100 : 0}
        />
        <HwCard
          label="Root Disk"
          value={fmtBytes(agent.disk_size).split(' ')[0]}
          unit={fmtBytes(agent.disk_size).split(' ')[1] || "GB"}
          icon="hard_drive"
          colorHex="var(--status-offline)"
          progress={snap && agent.disk_size > 0 ? (snap.DiskUsedBytes / agent.disk_size) * 100 : 0}
        />
        <HwCard
          label="Swap Size"
          value={fmtBytes(agent.swap_size).split(' ')[0]}
          unit={fmtBytes(agent.swap_size).split(' ')[1] || "GB"}
          icon="storage"
          colorHex="var(--text-muted)"
          progress={snap && agent.swap_size > 0 ? (snap.RAMSwapUsedBytes / agent.swap_size) * 100 : 0}
        />
      </div>

      {/* Charts Grid */}
      <div className="grid-charts mobile-grid-1" style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(400px, 1fr))', gap: '16px', marginTop: '8px' }}>
        <MetricChart agentId={agent.agent_id} tab="cpu" hours={activeHours} livePoint={liveMetrics[agent.agent_id]} TABS={TABS} customRange={customRange} onZoom={(start, end) => setCustomRange({ start, end })} />
        <MetricChart agentId={agent.agent_id} tab="ram" hours={activeHours} livePoint={liveMetrics[agent.agent_id]} maxValue={Math.max(agent.ram_size, agent.swap_size)} TABS={TABS} customRange={customRange} onZoom={(start, end) => setCustomRange({ start, end })} />
        <MetricChart agentId={agent.agent_id} tab="net" hours={activeHours} livePoint={liveMetrics[agent.agent_id]} maxValue={1024} TABS={TABS} customRange={customRange} onZoom={(start, end) => setCustomRange({ start, end })} />
        <MetricChart agentId={agent.agent_id} tab="disk" hours={activeHours} livePoint={liveMetrics[agent.agent_id]} maxValue={agent.disk_size} TABS={TABS} customRange={customRange} onZoom={(start, end) => setCustomRange({ start, end })} />
        <MetricChart agentId={agent.agent_id} tab="disk_io" hours={activeHours} livePoint={liveMetrics[agent.agent_id]} TABS={TABS} customRange={customRange} onZoom={(start, end) => setCustomRange({ start, end })} />
      </div>
    </div>
  );
}
