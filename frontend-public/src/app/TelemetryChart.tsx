import React from "react";
import {
  AreaChart, Area, LineChart, Line, XAxis, YAxis, Tooltip,
  ResponsiveContainer, CartesianGrid, ReferenceArea
} from "recharts";

export interface SeriesCfg {
  metric: string;
  label: string;
  color: string;
  fill?: boolean;
}

export type ChartPoint = { time: number } & Record<string, number | null>;

interface TelemetryChartProps {
  data: ChartPoint[];
  series: SeriesCfg[];
  maxValue?: number;
  fmt: (v: number) => string;

  // Brush selection for zoom (optional)
  refAreaLeft?: number | null;
  refAreaRight?: number | null;
  setRefAreaLeft?: (v: number | null) => void;
  setRefAreaRight?: (v: number | null) => void;
  onZoom?: (start: number, end: number) => void;

  // Custom height (optional)
  height?: string | number;
}

export function TelemetryChart({
  data,
  series,
  maxValue,
  fmt,
  refAreaLeft,
  refAreaRight,
  setRefAreaLeft,
  setRefAreaRight,
  onZoom,
  height
}: TelemetryChartProps) {
  // ── Dynamic Peak & yMax Calculations ─────────────────────────────────────
  const computedMaxVal = React.useMemo(() => {
    let max = 0;
    data.forEach(pt => {
      series.forEach(s => {
        const val = pt[s.label];
        if (typeof val === 'number' && val > max) {
          max = val;
        }
      });
    });
    return max;
  }, [data, series]);

  const yMax = React.useMemo(() => {
    if (maxValue !== undefined) {
      return Math.max(maxValue, computedMaxVal);
    }
    return computedMaxVal || 100;
  }, [maxValue, computedMaxVal]);

  // ── Reactive Downtime Gap Detection & Insertion ──────────────────────────
  const downtimes = React.useMemo(() => {
    if (data.length < 2) return [];
    const gaps: number[] = [];
    for (let i = 1; i < data.length; i++) {
      const d = data[i].time - data[i - 1].time;
      if (d > 0) gaps.push(d);
    }
    if (gaps.length === 0) return [];
    gaps.sort((a, b) => a - b);
    let normalInterval = gaps[Math.floor(gaps.length * 0.15)] || gaps[0];
    normalInterval = Math.max(15000, normalInterval);

    const threshold = normalInterval * 2.5;
    const intervals: { start: number; end: number }[] = [];

    for (let i = 1; i < data.length; i++) {
      const prev = data[i - 1].time;
      const curr = data[i].time;
      const diff = curr - prev;

      if (diff > threshold) {
        intervals.push({
          start: prev + normalInterval,
          end: curr - normalInterval,
        });
      }
    }
    return intervals;
  }, [data]);

  const chartData = React.useMemo(() => {
    if (data.length < 2) return data;
    const gaps: number[] = [];
    for (let i = 1; i < data.length; i++) {
      const d = data[i].time - data[i - 1].time;
      if (d > 0) gaps.push(d);
    }
    if (gaps.length === 0) return data;
    gaps.sort((a, b) => a - b);
    let normalInterval = gaps[Math.floor(gaps.length * 0.15)] || gaps[0];
    normalInterval = Math.max(15000, normalInterval);

    const threshold = normalInterval * 2.5;
    const result: ChartPoint[] = [];

    for (let i = 0; i < data.length; i++) {
      if (i > 0) {
        const prev = data[i - 1].time;
        const curr = data[i].time;
        const diff = curr - prev;

        if (diff > threshold) {
          const nullPoint1: any = { time: prev + normalInterval };
          const nullPoint2: any = { time: curr - normalInterval };
          series.forEach((s) => {
            nullPoint1[s.label] = null;
            nullPoint2[s.label] = null;
          });
          result.push(nullPoint1);
          result.push(nullPoint2);
        }
      }
      result.push(data[i]);
    }
    return result;
  }, [data, series]);

  // ── Formatter ────────────────────────────────────────────────────────────
  const timeRange = React.useMemo(() => {
    if (data.length < 2) return 0;
    let min = Infinity;
    let max = -Infinity;
    for (let i = 0; i < data.length; i++) {
      const t = data[i].time;
      if (typeof t === 'number' && !isNaN(t)) {
        if (t < min) min = t;
        if (t > max) max = t;
      }
    }
    return min === Infinity || max === -Infinity ? 0 : max - min;
  }, [data]);

  const tickFmt = React.useCallback((ms: number) => {
    const date = new Date(ms);
    // Less than 24 hours (86,400,000 ms)
    if (timeRange < 86400000) {
      return date.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit', hour12: false });
    }
    
    const day = date.getDate();
    const month = date.toLocaleDateString([], { month: 'short' });
    
    // Less than 1 year (approx 31,536,000,000 ms)
    if (timeRange < 31536000000) {
      return `${day} ${month}`;
    }
    
    // More than 1 year
    const year = date.getFullYear();
    return `${day} ${month} ${year}`;
  }, [timeRange]);

  const hasAreaFill = series.some((s) => s.fill);

  return (
    <ResponsiveContainer width="100%" height={(height || "100%") as any}>
      {hasAreaFill ? (
        <AreaChart
          data={chartData}
          margin={{ top: 10, right: 30, bottom: 0, left: 10 }}
          onMouseDown={(e) => {
            if (onZoom && setRefAreaLeft && e && e.activeLabel) {
              setRefAreaLeft(Number(e.activeLabel));
            }
          }}
          onMouseMove={(e) => {
            if (onZoom && setRefAreaLeft && refAreaLeft && setRefAreaRight && e && e.activeLabel) {
              setRefAreaRight(Number(e.activeLabel));
            }
          }}
          onMouseUp={() => {
            if (onZoom && refAreaLeft && refAreaRight && refAreaLeft !== refAreaRight) {
              let [left, right] = [refAreaLeft, refAreaRight];
              if (left > right) [left, right] = [right, left];
              onZoom(left, right);
            }
            if (setRefAreaLeft) setRefAreaLeft(null);
            if (setRefAreaRight) setRefAreaRight(null);
          }}
          onTouchStart={(e) => {
            if (onZoom && setRefAreaLeft && e && e.activeLabel) {
              setRefAreaLeft(Number(e.activeLabel));
            }
          }}
          onTouchMove={(e) => {
            if (onZoom && setRefAreaLeft && refAreaLeft && setRefAreaRight && e && e.activeLabel) {
              setRefAreaRight(Number(e.activeLabel));
            }
          }}
          onTouchEnd={() => {
            if (onZoom && refAreaLeft && refAreaRight && refAreaLeft !== refAreaRight) {
              let [left, right] = [refAreaLeft, refAreaRight];
              if (left > right) [left, right] = [right, left];
              onZoom(left, right);
            }
            if (setRefAreaLeft) setRefAreaLeft(null);
            if (setRefAreaRight) setRefAreaRight(null);
          }}
        >
          <defs>
            {series.map((s) => (
              s.fill && (
                <linearGradient key={s.label} id={`color-${s.label}`} x1="0" y1="0" x2="0" y2="1">
                  <stop offset="5%" stopColor={s.color} stopOpacity={0.3} />
                  <stop offset="95%" stopColor={s.color} stopOpacity={0} />
                </linearGradient>
              )
            ))}
          </defs>
          <CartesianGrid strokeDasharray="3 3" stroke="var(--border-color)" vertical={false} />
          <XAxis dataKey="time" type="number" domain={["dataMin", "dataMax"]} scale="time" tickFormatter={tickFmt} tick={{ fontSize: 10, fill: "var(--text-muted)", fontFamily: "var(--font-mono)" }} tickLine={false} axisLine={false} minTickGap={40} />
          <YAxis domain={[0, yMax]} tickFormatter={fmt} tick={{ fontSize: 10, fill: "var(--text-muted)", fontFamily: "var(--font-mono)" }} tickLine={false} axisLine={false} width={65} />
          <Tooltip labelFormatter={(ms) => new Date(ms as number).toLocaleString([], { month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit' })} formatter={(v: any, name: any) => [fmt(Number(v)), name]} cursor={{ stroke: 'var(--text-muted)', strokeWidth: 1, strokeDasharray: '4 4' }} />
          {downtimes.map((dw, idx) => (
            <ReferenceArea
              key={`dw-${idx}`}
              x1={dw.start}
              x2={dw.end}
              y1={0}
              y2={yMax}
              fill="rgba(239, 68, 68, 0.22)"
              stroke="rgba(239, 68, 68, 0.5)"
              strokeDasharray="4 4"
              strokeWidth={1.5}
              ifOverflow="discard"
            />
          ))}
          {series.map((s) => (
            <Area key={s.label} type="monotone" dataKey={s.label} stroke={s.color} strokeWidth={2} fill={s.fill ? `url(#color-${s.label})` : "transparent"} isAnimationActive={false} />
          ))}
          {onZoom && refAreaLeft && refAreaRight && (
            <ReferenceArea
              x1={refAreaLeft}
              x2={refAreaRight}
              strokeOpacity={0.3}
              fill="var(--accent-primary)"
              fillOpacity={0.15}
            />
          )}
        </AreaChart>
      ) : (
        <LineChart
          data={chartData}
          margin={{ top: 10, right: 30, bottom: 0, left: 10 }}
          onMouseDown={(e) => {
            if (onZoom && setRefAreaLeft && e && e.activeLabel) {
              setRefAreaLeft(Number(e.activeLabel));
            }
          }}
          onMouseMove={(e) => {
            if (onZoom && setRefAreaLeft && refAreaLeft && setRefAreaRight && e && e.activeLabel) {
              setRefAreaRight(Number(e.activeLabel));
            }
          }}
          onMouseUp={() => {
            if (onZoom && refAreaLeft && refAreaRight && refAreaLeft !== refAreaRight) {
              let [left, right] = [refAreaLeft, refAreaRight];
              if (left > right) [left, right] = [right, left];
              onZoom(left, right);
            }
            if (setRefAreaLeft) setRefAreaLeft(null);
            if (setRefAreaRight) setRefAreaRight(null);
          }}
          onTouchStart={(e) => {
            if (onZoom && setRefAreaLeft && e && e.activeLabel) {
              setRefAreaLeft(Number(e.activeLabel));
            }
          }}
          onTouchMove={(e) => {
            if (onZoom && setRefAreaLeft && refAreaLeft && setRefAreaRight && e && e.activeLabel) {
              setRefAreaRight(Number(e.activeLabel));
            }
          }}
          onTouchEnd={() => {
            if (onZoom && refAreaLeft && refAreaRight && refAreaLeft !== refAreaRight) {
              let [left, right] = [refAreaLeft, refAreaRight];
              if (left > right) [left, right] = [right, left];
              onZoom(left, right);
            }
            if (setRefAreaLeft) setRefAreaLeft(null);
            if (setRefAreaRight) setRefAreaRight(null);
          }}
        >
          <CartesianGrid strokeDasharray="3 3" stroke="var(--border-color)" vertical={false} />
          <XAxis dataKey="time" type="number" domain={["dataMin", "dataMax"]} scale="time" tickFormatter={tickFmt} tick={{ fontSize: 10, fill: "var(--text-muted)", fontFamily: "var(--font-mono)" }} tickLine={false} axisLine={false} minTickGap={40} />
          <YAxis domain={[0, yMax]} tickFormatter={fmt} tick={{ fontSize: 10, fill: "var(--text-muted)", fontFamily: "var(--font-mono)" }} tickLine={false} axisLine={false} width={65} />
          <Tooltip labelFormatter={(ms) => new Date(ms as number).toLocaleString([], { month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit' })} formatter={(v: any, name: any) => [fmt(Number(v)), name]} cursor={{ stroke: 'var(--text-muted)', strokeWidth: 1, strokeDasharray: '4 4' }} />
          {downtimes.map((dw, idx) => (
            <ReferenceArea
              key={`dw-${idx}`}
              x1={dw.start}
              x2={dw.end}
              y1={0}
              y2={yMax}
              fill="rgba(239, 68, 68, 0.22)"
              stroke="rgba(239, 68, 68, 0.5)"
              strokeDasharray="4 4"
              strokeWidth={1.5}
              ifOverflow="discard"
            />
          ))}
          {series.map((s) => (
            <Line key={s.label} type="monotone" dataKey={s.label} stroke={s.color} strokeWidth={2} dot={false} activeDot={{ r: 4, fill: s.color, stroke: 'var(--bg-secondary)', strokeWidth: 2 }} isAnimationActive={false} />
          ))}
          {onZoom && refAreaLeft && refAreaRight && (
            <ReferenceArea
              x1={refAreaLeft}
              x2={refAreaRight}
              strokeOpacity={0.3}
              fill="var(--accent-primary)"
              fillOpacity={0.15}
            />
          )}
        </LineChart>
      )}
    </ResponsiveContainer>
  );
}
