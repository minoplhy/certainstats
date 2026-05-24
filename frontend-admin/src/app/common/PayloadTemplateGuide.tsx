import React from "react";

interface PayloadTemplateGuideProps {
  standalone?: boolean;
}

export default function PayloadTemplateGuide({ standalone = false }: PayloadTemplateGuideProps) {
  const content = (
    <>
      <p style={{ fontWeight: 'bold', marginBottom: '8px', display: 'flex', alignItems: 'center', gap: '6px', color: 'var(--text-primary)', margin: 0 }}>
        <span className="material-symbols-outlined" style={{ fontSize: '16px', color: 'var(--accent-primary)' }}>info</span>
        Custom Payload Template Guide
      </p>
      <p style={{ color: 'var(--text-secondary)', marginBottom: '12px', marginTop: '6px', fontSize: '13px', lineHeight: '1.5' }}>
        Customize the JSON body of your webhook. If left empty, a standard notification payload is dispatched. Use the placeholders below to inject dynamic telemetry state:
      </p>
      
      <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: '8px 16px', fontFamily: 'var(--font-mono)', fontSize: '12px', marginBottom: '16px' }} className="mobile-grid-1">
        <div>
          <span style={{ color: 'var(--accent-primary)', fontWeight: 'bold' }}>{"{{AGENT_ID}}"}</span>
          <span style={{ color: 'var(--text-secondary)', opacity: 0.8, marginLeft: '8px' }}>— Node ID</span>
        </div>
        <div>
          <span style={{ color: 'var(--accent-primary)', fontWeight: 'bold' }}>{"{{NICKNAME}}"}</span>
          <span style={{ color: 'var(--text-secondary)', opacity: 0.8, marginLeft: '8px' }}>— Readable Nickname</span>
        </div>
        <div>
          <span style={{ color: 'var(--accent-primary)', fontWeight: 'bold' }}>{"{{STATUS}}"}</span>
          <span style={{ color: 'var(--text-secondary)', opacity: 0.8, marginLeft: '8px' }}>— Alert state (FIRING/RESOLVED)</span>
        </div>
        <div>
          <span style={{ color: 'var(--accent-primary)', fontWeight: 'bold' }}>{"{{VALUE}}"}</span>
          <span style={{ color: 'var(--text-secondary)', opacity: 0.8, marginLeft: '8px' }}>— Current Value (e.g. 85.50%)</span>
        </div>
        <div>
          <span style={{ color: 'var(--accent-primary)', fontWeight: 'bold' }}>{"{{TRIGGER_LABEL}}"}</span>
          <span style={{ color: 'var(--text-secondary)', opacity: 0.8, marginLeft: '8px' }}>— Metric Label (e.g. CPU IO Wait)</span>
        </div>
        <div>
          <span style={{ color: 'var(--accent-primary)', fontWeight: 'bold' }}>{"{{THRESHOLD}}"}</span>
          <span style={{ color: 'var(--text-secondary)', opacity: 0.8, marginLeft: '8px' }}>— Condition Boundary</span>
        </div>
        <div>
          <span style={{ color: 'var(--accent-primary)', fontWeight: 'bold' }}>{"{{TIME_TRIGGER}}"}</span>
          <span style={{ color: 'var(--text-secondary)', opacity: 0.8, marginLeft: '8px' }}>— Breach/Offline Timestamp</span>
        </div>
        <div>
          <span style={{ color: 'var(--accent-primary)', fontWeight: 'bold' }}>{"{{TIME_RESOLVED}}"}</span>
          <span style={{ color: 'var(--text-secondary)', opacity: 0.8, marginLeft: '8px' }}>— Recovery/Online Timestamp</span>
        </div>
      </div>
      
      <div style={{
        paddingTop: '12px',
        borderTop: '1px solid var(--border-color)',
        fontSize: '12px'
      }}>
        <p style={{ fontWeight: 'bold', color: 'var(--text-primary)', marginBottom: '4px', marginTop: 0 }}>Downtime & Discord Integration Note:</p>
        <p style={{ color: 'var(--text-secondary)', opacity: 0.8, fontSize: '12px', margin: 0, lineHeight: '1.5' }}>
          For Discord Webhooks, <code style={{ color: 'var(--accent-primary)' }}>{"{{TIME_TRIGGER}}"}</code> and <code style={{ color: 'var(--accent-primary)' }}>{"{{TIME_RESOLVED}}"}</code> render client-side relative timestamps dynamically. You can also use <code style={{ color: 'var(--accent-primary)' }}>{"{{DOWN_DURATION}}"}</code> (e.g., <code style={{ color: 'var(--text-primary)' }}>5m 12s</code>) on resolved notifications for offline events.
        </p>
      </div>
    </>
  );

  if (standalone) {
    return (
      <div className="card" style={{ padding: '24px' }}>
        {content}
      </div>
    );
  }

  return (
    <div style={{
      marginTop: '12px',
      padding: '16px',
      background: 'var(--bg-secondary)',
      borderRadius: '8px',
      borderLeft: '4px solid var(--accent-primary)',
      fontSize: '13px',
      lineHeight: '1.6'
    }}>
      {content}
    </div>
  );
}
