import React, { useEffect, useState, useRef } from "react";
import { createPortal } from "react-dom";
import { fetchAPI } from "../lib/api";
import { MetricResponse, PublicAgent, MetricKey } from "../types";
import { TelemetryChart } from "./TelemetryChart";

// ── Helpers ────────────────────────────────────────────────────────

export function fmtBytes(b?: number | null) {
  if (!b) return "–";
  const u = ["B", "KB", "MB", "GB", "TB"];
  let v = b, i = 0;
  while (v >= 1024 && i < u.length - 1) { v /= 1024; i++; }
  return `${v.toFixed(1)} ${u[i]}`;
}

export function fmtUptime(s?: number | null) {
  if (!s) return "–";
  const d = Math.floor(s / 86400), h = Math.floor((s % 86400) / 3600), m = Math.floor((s % 3600) / 60);
  return d ? `${d}d ${h}h` : h ? `${h}h ${m}m` : `${m}m`;
}

export function fmtBps(v?: number | null) {
  if (v == null) return "0 B/s";
  const u = ["B/s", "KB/s", "MB/s", "GB/s"];
  let i = 0;
  while (v >= 1024 && i < u.length - 1) { v /= 1024; i++; }
  return `${v.toFixed(1)} ${u[i]}`;
}

export const TIME_RANGES = [
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

// ── Metric Chart Configuration ─────────────────────────────────────

interface SeriesCfg {
  metric: MetricKey;
  label: string;
  color: string;
  fill?: boolean;
}

export const CHART_GROUPS: { label: string; series: SeriesCfg[]; fmt: (v: number) => string }[] = [
  {
    label: "CPU",
    fmt: v => `${v.toFixed(1)}%`,
    series: [
      { metric: "agent_cpu_usage", label: "Usr", color: "var(--accent-primary)", fill: true },
      { metric: "agent_cpu_iowait", label: "IO", color: "var(--text-secondary)" },
      { metric: "agent_cpu_steal", label: "Stl", color: "var(--status-offline)" },
    ]
  },
  {
    label: "Memory",
    fmt: fmtBytes,
    series: [
      { metric: "agent_ram_used", label: "RAM", color: "#14b8a6" },
      { metric: "agent_swap_used", label: "Swap", color: "var(--text-secondary)" },
    ]
  },
  {
    label: "Network I/O",
    fmt: fmtBps,
    series: [
      { metric: "agent_rx_bytes", label: "RX", color: "#1e40af" },
      { metric: "agent_tx_bytes", label: "TX", color: "#7e22ce" },
    ]
  },
  {
    label: "Disk Usage",
    fmt: fmtBytes,
    series: [
      { metric: "agent_disk_used", label: "Disk", color: "var(--accent-secondary)", fill: true },
    ]
  },
  {
    label: "Disk I/O",
    fmt: fmtBps,
    series: [
      { metric: "agent_disk_read_bytes", label: "Read", color: "#f59e0b" },
      { metric: "agent_disk_write_bytes", label: "Write", color: "#ef4444" },
    ]
  }
];

function MetricPanel({
  dashboardID,
  publicId,
  group,
  allowedMetrics,
  hours,
  livePoint,
  maxValue,
}: {
  dashboardID: string;
  publicId: string;
  group: typeof CHART_GROUPS[0];
  allowedMetrics: string[];
  hours: number;
  livePoint?: any;
  maxValue?: number;
}) {
  type ChartPoint = { time: number } & Record<string, number | null>;
  const [data, setData] = useState<ChartPoint[]>([]);
  const [loading, setLoading] = useState(true);
  const lastProcessedTs = useRef<string | null>(null);

  const activeSeries = group.series.filter(s => allowedMetrics.includes(s.metric));

  useEffect(() => {
    if (activeSeries.length === 0) return;
    setLoading(true);

    Promise.all(
      activeSeries.map(s =>
        fetchAPI<MetricResponse>(
          `/api/public/metrics?dashboard_id=${dashboardID}&agent_id=${publicId}&metric=${s.metric}&hours=${hours}`
        ).catch(() => ({ metric: s.metric, series: [] } as MetricResponse))
      )
    ).then((results) => {
      const map = new Map<number, ChartPoint>();

      results.forEach((res, i) => {
        let pts = res.series && res.series[0] ? res.series[0].data : [];
        const s = activeSeries[i];

        if (s.metric.includes("bytes")) {
          // Values are "bytes in this interval" (delta) — convert to B/s
          pts = pts.map(([ts, v], j) => {
            const prevTs = j > 0 ? pts[j - 1][0] : null;
            if (prevTs === null) {
              // First point: no previous timestamp, estimate from next point
              const nextTs = pts.length > 1 ? pts[1][0] : ts + 60000;
              const dt = (nextTs - ts) / 1000;
              return [ts, dt > 0 ? Math.max(0, v / dt) : 0] as [number, number];
            }
            const dt = (ts - prevTs) / 1000;
            const rate = dt > 0 ? Math.max(0, v / dt) : 0;
            return [ts, rate] as [number, number];
          });
        }

        pts.forEach(([ts, v]) => {
          const existing: ChartPoint = map.get(ts) ?? { time: ts };
          existing[s.label] = v;
          map.set(ts, existing);
        });
      });

      setData(Array.from(map.values()).sort((a, b) => a.time - b.time));
      setLoading(false);
    });
  }, [dashboardID, publicId, hours, activeSeries.length]);


  useEffect(() => {
    if (loading) {
      lastProcessedTs.current = null;
      return;
    }
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

      activeSeries.forEach(s => {
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
  }, [livePoint, loading, hours, activeSeries]);

  const lastPoint = data[data.length - 1] || {};

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

  return (
    <div className="card flex-col animate-fade-in mobile-p-sm" style={{ padding: '20px', position: 'relative', overflow: 'hidden' }}>
      <div className="flex justify-between items-center" style={{ marginBottom: '12px', position: 'relative', zIndex: 10 }}>
        <h2 className="text-sm font-bold text-primary uppercase tracking-wider" style={{ letterSpacing: '0.05em' }}>{group.label}</h2>
        <div className="flex gap-4 text-[11px] font-mono flex-wrap justify-end">
          {activeSeries.map((s) => {
            const val = lastPoint[s.label];
            const displayVal = val != null ? group.fmt(val) : "–";
            return (
              <div key={s.label} className="flex items-center gap-2">
                <span style={{ width: '8px', height: '8px', borderRadius: '50%', backgroundColor: s.color }} />
                <span style={{ color: 'var(--text-secondary)', fontWeight: '600' }}>{s.label} <span style={{ color: 'var(--text-primary)' }}>{displayVal}</span></span>
              </div>
            );
          })}
        </div>
      </div>

      {loading ? (
        <div style={{ height: '200px', display: 'flex', alignItems: 'center', justifyContent: 'center', fontSize: '12px', color: 'var(--text-muted)' }}>
          <span className="material-symbols-outlined" style={{ animation: 'spin 1s linear infinite' }}>sync</span>
        </div>
      ) : data.length === 0 ? (
        <div style={{ height: '200px', display: 'flex', alignItems: 'center', justifyContent: 'center', fontSize: '12px', color: 'var(--text-muted)', border: '1px dashed var(--border-color)', borderRadius: '8px' }}>
          No data available
        </div>
      ) : (
        <div className="mobile-h-tall" style={{ height: '200px', width: '100%', marginTop: '8px', marginLeft: '-16px' }}>
          <TelemetryChart
            data={data}
            series={activeSeries as any}
            maxValue={maxValue}
            fmt={group.fmt}
          />
        </div>
      )}
    </div>
  );
}

function HwCard({ label, value, unit, icon, statusColor = "var(--accent-primary)", progress }: { label: string; value: string; unit: string; icon: string; statusColor?: string; progress?: number }) {
  return (
    <div className="card flex-col gap-2 animate-fade-in" style={{ padding: '20px', borderRadius: '16px', background: 'var(--bg-secondary)', border: '1px solid var(--border-color)', boxShadow: 'var(--card-shadow)' }}>
      <div className="flex justify-between items-center text-muted">
        <span style={{ fontSize: '10px', textTransform: 'uppercase', letterSpacing: '0.15em', fontWeight: '900', color: 'var(--text-muted)', opacity: 0.6 }}>{label}</span>
        <span className="material-symbols-outlined" style={{ fontSize: '18px', color: statusColor, opacity: 0.8 }}>{icon}</span>
      </div>
      <div className="flex items-baseline gap-2 mt-2">
        <span className="font-mono" style={{ fontSize: '24px', fontWeight: '700', color: 'var(--text-primary)', lineHeight: '1' }}>{value}</span>
        <span className="font-mono" style={{ fontSize: '11px', color: 'var(--text-muted)', fontWeight: '700' }}>{unit}</span>
      </div>
      <div style={{ width: '100%', height: '4px', background: 'var(--bar-bg)', borderRadius: '2px', marginTop: '14px', overflow: 'hidden' }}>
        <div style={{
          height: '100%',
          width: progress !== undefined ? `${Math.min(100, Math.max(0, progress))}%` : '100%',
          backgroundColor: statusColor,
          borderRadius: '2px',
          boxShadow: `0 0 10px ${statusColor}30`,
          transition: 'width 1.2s cubic-bezier(0.4, 0, 0.2, 1)'
        }} />
      </div>
    </div>
  );
}

interface PublicAgentDetailProps {
  active: PublicAgent;
  setSelectedId: (id: string | null) => void;
  hours: number;
  setHours: (val: number) => void;
  liveMetrics: Record<string, any>;
  allowedMetrics: string[];
  maxDays: number;
  dashboardID: string;
  TIME_RANGES: { label: string; value: number }[];
}

export function PublicAgentDetail({
  active,
  setSelectedId,
  hours,
  setHours,
  liveMetrics,
  allowedMetrics,
  maxDays,
  dashboardID,
  TIME_RANGES
}: PublicAgentDetailProps) {
  const [timePickerOpen, setTimePickerOpen] = useState(false);
  const [coords, setCoords] = useState({ top: 0, left: 0, openUp: false });
  const dropdownRef = useRef<HTMLDivElement>(null);

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

  return (
    <div className="animate-fade-in mobile-px-4" style={{ padding: '32px 8px', maxWidth: '1200px', margin: '0 auto', display: 'flex', flexDirection: 'column', gap: '24px', width: '100%' }}>

      {/* ── HEADER NAVIGATION ────────────────────────────────── */}
      <button
        onClick={() => setSelectedId(null)}
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
            <h2 className="font-display mobile-text-lg" style={{ fontSize: '32px', fontWeight: '800', color: 'var(--text-primary)', lineHeight: '1.2' }}>
              {active.display_name}
            </h2>
            <div className="flex items-center gap-2">
              {active.is_online !== undefined && (
                <span className={`status-pill ${active.is_online ? 'online' : 'offline'}`}>
                  <span className="status-dot-pulse" />
                  {active.is_online ? "online" : "offline"}
                </span>
              )}
            </div>
          </div>

          <div className="flex items-center gap-2" style={{ position: 'relative' }} ref={dropdownRef}>
            <button
              onClick={(e) => {
                if (!timePickerOpen) {
                  const rect = e.currentTarget.getBoundingClientRect();
                  const menuHeight = 280;
                  const menuWidth = 280;
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
                background: 'var(--bg-secondary)',
                border: '1px solid var(--border-color)',
                color: 'var(--text-primary)',
                cursor: 'pointer'
              }}
            >
              <span className="material-symbols-outlined" style={{ fontSize: '18px' }}>history</span>
              <span>Last {TIME_RANGES.find(r => r.value === hours)?.label || `${hours}h`}</span>
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
                  width: '280px',
                  borderRadius: '16px',
                  padding: '20px',
                  zIndex: 9999,
                  boxShadow: 'var(--card-shadow)',
                  border: '1px solid var(--border-color)',
                  background: 'var(--bg-secondary)',
                  backdropFilter: 'blur(20px)',
                  display: 'flex',
                  flexDirection: 'column',
                  gap: '12px',
                }}
                onClick={e => e.stopPropagation()}
              >
                <div>
                  <h4 style={{ fontSize: '10px', fontWeight: '800', color: 'var(--text-muted)', textTransform: 'uppercase', letterSpacing: '0.08em', marginBottom: '10px' }}>
                    Quick Ranges
                  </h4>
                  <div style={{ display: 'grid', gridTemplateColumns: 'repeat(3, 1fr)', gap: '8px' }}>
                    {TIME_RANGES.filter(r => r.value <= maxDays * 24).map((r: any) => (
                      <button
                        key={r.value}
                        onClick={() => {
                          setHours(r.value);
                          setTimePickerOpen(false);
                        }}
                        style={{
                          padding: '6px',
                          fontSize: '11px',
                          fontWeight: '600',
                          borderRadius: '8px',
                          textAlign: 'center',
                          background: hours === r.value ? 'rgba(59, 130, 246, 0.15)' : 'var(--bar-bg)',
                          border: hours === r.value ? '1px solid var(--accent-primary)' : '1px solid var(--border-color)',
                          color: hours === r.value ? 'var(--accent-primary)' : 'var(--text-secondary)',
                          cursor: 'pointer',
                        }}
                        className="hover:scale-105"
                      >
                        {r.label}
                      </button>
                    ))}
                  </div>
                </div>
              </div>,
              document.body
            )}
          </div>
        </div>

        {/* Specifications Tiles */}
        <div className="spec-tile-grid">

          <div className="spec-tile">
            <div className="spec-tile-icon" style={{ background: 'var(--spec-indigo-bg)', borderColor: 'var(--spec-indigo-border)', color: 'var(--spec-indigo-color)' }}>
              <span className="material-symbols-outlined" style={{ fontSize: '18px' }}>fingerprint</span>
            </div>
            <div className="spec-tile-content">
              <span className="spec-tile-label">Public ID</span>
              <span className="spec-tile-value mono">{active.public_id}</span>
            </div>
          </div>

          {active.uptime !== undefined && active.uptime !== null && (
            <div className="spec-tile">
              <div className="spec-tile-icon" style={{ background: 'var(--spec-green-bg)', borderColor: 'var(--spec-green-border)', color: 'var(--spec-green-color)' }}>
                <span className="material-symbols-outlined" style={{ fontSize: '18px' }}>schedule</span>
              </div>
              <div className="spec-tile-content">
                <span className="spec-tile-label">Uptime</span>
                <span className="spec-tile-value">{fmtUptime(active.uptime)}</span>
              </div>
            </div>
          )}

          {active.linux_version && active.linux_version !== "Pending connection..." && (
            <div className="spec-tile">
              <div className="spec-tile-icon" style={{ background: 'var(--spec-blue-bg)', borderColor: 'var(--spec-blue-border)', color: 'var(--spec-blue-color)' }}>
                <span className="material-symbols-outlined" style={{ fontSize: '18px' }}>terminal</span>
              </div>
              <div className="spec-tile-content">
                <span className="spec-tile-label">OS Kernel</span>
                <span className="spec-tile-value mono" title={active.linux_version}>{active.linux_version}</span>
              </div>
            </div>
          )}

          {active.cpu_model && active.cpu_model !== "Waiting for data..." && (
            <div className="spec-tile">
              <div className="spec-tile-icon" style={{ background: 'var(--spec-amber-bg)', borderColor: 'var(--spec-amber-border)', color: 'var(--spec-amber-color)' }}>
                <span className="material-symbols-outlined" style={{ fontSize: '18px' }}>developer_board</span>
              </div>
              <div className="spec-tile-content">
                <span className="spec-tile-label">CPU Architecture</span>
                <span className="spec-tile-value mono" title={active.cpu_model}>{active.cpu_model}</span>
              </div>
            </div>
          )}
        </div>

        {/* Odometers Row */}
        {(((active.net?.total_rx_bytes !== undefined) || (active.net?.total_tx_bytes !== undefined)) || (active.disks && active.disks.length > 0)) && (
          <div className="odometer-grid">
            {((active.net?.total_rx_bytes !== undefined) || (active.net?.total_tx_bytes !== undefined)) && (
              <div className="odometer-tile">
                <div className="odometer-label-group">
                  <span className="material-symbols-outlined" style={{ fontSize: '16px', color: 'var(--accent-primary)' }}>download_for_offline</span>
                  <span>Network Traffic</span>
                </div>
                <div className="odometer-values">
                  {active.net?.total_rx_bytes !== undefined && <span>↓ {fmtBytes(active.net.total_rx_bytes)}</span>}
                  {active.net?.total_rx_bytes !== undefined && active.net?.total_tx_bytes !== undefined && <span style={{ opacity: 0.3 }}>/</span>}
                  {active.net?.total_tx_bytes !== undefined && <span>↑ {fmtBytes(active.net.total_tx_bytes)}</span>}
                </div>
              </div>
            )}

            {active.disks?.map((d) => (
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
      {(() => {
        const snap = liveMetrics[active.public_id];
        return (
          <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(220px, 1fr))', gap: '16px', width: '100%', marginTop: '8px' }}>
            {active.cpu_cores !== undefined && (
              <HwCard
                label="CPU Cores"
                value={active.cpu_cores.toString()}
                unit="vCPU"
                icon="memory"
                statusColor="var(--accent-primary)"
                progress={snap?.CPUUsagePercent}
              />
            )}
            {active.ram_size !== undefined && active.ram_size > 0 && (
              <HwCard
                label="Total RAM"
                value={fmtBytes(active.ram_size).split(' ')[0]}
                unit={fmtBytes(active.ram_size).split(' ')[1] || "GB"}
                icon="memory_alt"
                statusColor="var(--status-online)"
                progress={snap && active.ram_size ? (snap.RAMUsedBytes / active.ram_size) * 100 : 0}
              />
            )}
            {active.disk_size !== undefined && active.disk_size > 0 && (
              <HwCard
                label="Root Disk"
                value={fmtBytes(active.disk_size).split(' ')[0]}
                unit={fmtBytes(active.disk_size).split(' ')[1] || "GB"}
                icon="hard_drive"
                statusColor="var(--status-offline)"
                progress={snap && active.disk_size ? (snap.DiskUsedBytes / active.disk_size) * 100 : 0}
              />
            )}
            {active.swap_size !== undefined && (
              <HwCard
                label="Swap Size"
                value={fmtBytes(active.swap_size).split(' ')[0]}
                unit={fmtBytes(active.swap_size).split(' ')[1] || "GB"}
                icon="storage"
                statusColor="var(--text-muted)"
                progress={snap && active.swap_size ? (snap.RAMSwapUsedBytes / active.swap_size) * 100 : 0}
              />
            )}
          </div>
        );
      })()}

      {allowedMetrics.length === 0 ? (
        <div style={{ background: 'var(--bg-secondary)', border: '1px dashed var(--border-color)', borderRadius: '12px', padding: '48px', textAlign: 'center' }}>
          <span className="material-symbols-outlined" style={{ fontSize: '48px', color: 'var(--text-muted)', opacity: 0.5, marginBottom: '16px' }}>monitoring</span>
          <p style={{ color: 'var(--text-secondary)', fontWeight: '500' }}>No metrics configured for public viewing.</p>
        </div>
      ) : (
        <div className="grid-charts mobile-grid-1" style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fill, minmax(400px, 1fr))', gap: '16px', marginTop: '8px' }}>
          {CHART_GROUPS.map((group) => {
            const isAnyAllowed = group.series.some(s => allowedMetrics.includes(s.metric));
            if (!isAnyAllowed) return null;

            let maxValue: number | undefined = undefined;
            if (group.label === "Memory") maxValue = Math.max(active.ram_size || 0, active.swap_size || 0);
            if (group.label === "Network I/O") maxValue = 1024;
            if (group.label === "Disk Usage") maxValue = active.disk_size || 0;

            return (
              <MetricPanel
                key={group.label}
                dashboardID={dashboardID}
                publicId={active.public_id}
                group={group}
                allowedMetrics={allowedMetrics}
                hours={hours}
                livePoint={liveMetrics[active.public_id]}
                maxValue={maxValue}
              />
            );
          })}
        </div>
      )}
    </div>
  );
}
