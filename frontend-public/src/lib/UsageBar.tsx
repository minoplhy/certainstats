import React, { useState } from "react";
import { createPortal } from "react-dom";

interface UsageBarSegment {
  label: string;
  value: number; // percentage (0-100)
  color: string;
  displayValue?: string; // Optional absolute value for legend
  totalDisplay?: string; // Optional total value
}

interface UsageBarProps {
  label: string;
  segments: UsageBarSegment[];
  compact?: boolean;
  unit?: string;
}

export const UsageBar: React.FC<UsageBarProps> = ({ label, segments, compact, unit }) => {
  // Ensure we don't exceed 100% and handle empty space
  const totalValue = segments.reduce((acc, s) => acc + (s.value || 0), 0);

  const [tooltipState, setTooltipState] = useState<{
    visible: boolean;
    x: number;
    y: number;
  }>({ visible: false, x: 0, y: 0 });

  if (compact) {
    return (
      <>
        <div
          onMouseEnter={(e) => setTooltipState({ visible: true, x: e.clientX, y: e.clientY })}
          onMouseMove={(e) => setTooltipState({ visible: true, x: e.clientX, y: e.clientY })}
          onMouseLeave={() => setTooltipState(prev => ({ ...prev, visible: false }))}
          style={{
            width: '100%',
            height: '4px',
            background: 'var(--bg-primary)',
            borderRadius: '2px',
            overflow: 'hidden',
            display: 'flex',
            marginBottom: '3px',
            cursor: 'pointer'
          }}
        >
          {segments.map((s, i) => (
            <div
              key={i}
              style={{
                height: '100%',
                width: `${s.value || 0}%`,
                backgroundColor: s.color,
                transition: 'width 0.5s ease'
              }}
            />
          ))}
        </div>

        {tooltipState.visible && createPortal(
          <div
            style={{
              position: 'fixed',
              left: `${tooltipState.x < 220 ? tooltipState.x + 15 : tooltipState.x - 215}px`,
              top: `${tooltipState.y - 10}px`,
              zIndex: 999999,
              backgroundColor: 'var(--bg-panel)',
              backdropFilter: 'blur(12px)',
              WebkitBackdropFilter: 'blur(12px)',
              border: '1px solid var(--border-color)',
              borderRadius: '10px',
              padding: '10px 14px',
              boxShadow: 'var(--card-shadow)',
              color: 'var(--text-primary)',
              fontSize: '13px',
              fontWeight: '500',
              fontFamily: 'var(--font-sans)',
              pointerEvents: 'none',
              display: 'flex',
              flexDirection: 'column',
              gap: '6px',
              minWidth: '180px',
              transform: 'translateY(-50%)',
              transition: 'background-color var(--transition-fast), border-color var(--transition-fast), color var(--transition-fast)',
            }}
          >
            {/* Tooltip Header / Label */}
            <div style={{ fontSize: '10px', fontWeight: '800', color: 'var(--text-secondary)', textTransform: 'uppercase', letterSpacing: '0.05em', borderBottom: '1px solid var(--border-color)', paddingBottom: '4px', marginBottom: '2px' }}>
              {label}
            </div>

            {/* Segments list */}
            {segments.map((s, i) => {
              const segLabel = s.label === 'Used' || s.label === 'used' ? label : s.label;
              const formattedVal = s.displayValue
                ? `${s.displayValue}${s.totalDisplay ? '/' + s.totalDisplay : ''}`
                : `${(s.value || 0).toFixed(2)}%`;

              return (
                <div key={i} style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', gap: '16px' }}>
                  <div style={{ display: 'flex', alignItems: 'center', gap: '8px' }}>
                    <div style={{ width: '6px', height: '6px', borderRadius: '50%', backgroundColor: s.color, flexShrink: 0 }} />
                    <span style={{ color: 'var(--text-secondary)', fontSize: '12px' }}>{segLabel}</span>
                  </div>
                  <span style={{ fontFamily: 'var(--font-mono)', fontWeight: 'bold', color: 'var(--text-primary)', fontSize: '12px' }}>
                    {formattedVal}
                  </span>
                </div>
              );
            })}
          </div>,
          document.body
        )}
      </>
    );
  }

  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: '8px', width: '100%' }}>
      {/* Header */}
      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'flex-end', flexWrap: 'wrap', gap: '4px' }}>
        <span style={{ fontSize: '10px', fontWeight: 'bold', color: 'var(--text-muted)', textTransform: 'uppercase', letterSpacing: '0.05em', whiteSpace: 'nowrap' }}>{label}</span>
        <span style={{ fontSize: '11px', fontFamily: 'var(--font-mono)', fontWeight: '500', color: 'var(--text-primary)', whiteSpace: 'nowrap' }}>
          {segments[0]?.displayValue
            ? (unit !== undefined ? `${segments[0].displayValue}${unit ? " " + unit : ""}` : `${segments[0].displayValue} used`)
            : `${totalValue.toFixed(1)}%`
          }
        </span>
      </div>

      {/* The Bar */}
      <div style={{ width: '100%', height: '8px', background: 'rgba(255, 255, 255, 0.05)', borderRadius: '4px', overflow: 'hidden', display: 'flex', border: '1px solid rgba(255, 255, 255, 0.05)' }}>
        {segments.map((s, i) => (
          <div
            key={i}
            style={{
              height: '100%',
              width: `${s.value || 0}%`,
              backgroundColor: s.color,
              transition: 'width 0.5s ease'
            }}
            title={`${s.label}: ${s.displayValue || (s.value || 0).toFixed(1) + '%'}`}
          />
        ))}
      </div>

      {/* Legend - Only show if not compact and we have segments */}
      {segments.length > 0 && (
        <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: '4px 12px', marginTop: '2px' }}>
          {segments.map((s, i) => (
            <div key={i} style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between' }}>
              <div style={{ display: 'flex', alignItems: 'center', gap: '6px', minWidth: 0, flex: 1 }}>
                <div style={{ width: '6px', height: '6px', borderRadius: '50%', backgroundColor: s.color, flexShrink: 0 }} />
                <span style={{ fontSize: '10px', color: 'var(--text-muted)', whiteSpace: 'nowrap', overflow: 'hidden', textOverflow: 'ellipsis' }}>{s.label}</span>
              </div>
              <span style={{ fontSize: '10px', fontFamily: 'var(--font-mono)', color: 'var(--text-secondary)', marginLeft: '6px' }}>
                {s.displayValue || `${(s.value || 0).toFixed(1)}%`}
              </span>
            </div>
          ))}
        </div>
      )}
    </div>
  );
};
