import { useState } from "react";
import { createPortal } from "react-dom";

export interface ReinstallMessage {
  name: string;
  content: string;
  message_type: 'copy' | 'command' | 'note' | 'big_copy' | 'warning';
  description?: string;
}

export interface ReinstallData {
  message?: string;
  messages: ReinstallMessage[];
}

interface ReinstallModalProps {
  data: ReinstallData | null;
  onClose: () => void;
  showToast?: (msg: string) => void;
}

export default function ReinstallModal({ data, onClose, showToast }: ReinstallModalProps) {
  // We keep a map of copied states for commands/copies to show the checkmark locally
  const [copiedIndex, setCopiedIndex] = useState<Record<number, boolean>>({});

  if (!data) return null;

  const handleCopy = (text: string, index: number, label: string) => {
    navigator.clipboard.writeText(text);
    setCopiedIndex(prev => ({ ...prev, [index]: true }));
    if (showToast) {
      showToast(`${label} copied to clipboard`);
    }
    setTimeout(() => {
      setCopiedIndex(prev => ({ ...prev, [index]: false }));
    }, 2000);
  };

  return createPortal(
    <div 
      style={{ 
        position: 'fixed', 
        inset: 0, 
        display: 'flex', 
        alignItems: 'center', 
        justifyContent: 'center', 
        zIndex: 99999, 
        padding: '16px', 
        overflowY: 'auto' 
      }}
    >
      <div 
        onClick={onClose}
        className="modal-backdrop"
      />
      <div 
        className="card" 
        style={{ 
          width: '100%', 
          maxWidth: '600px', 
          padding: '32px', 
          border: '1px solid var(--border-color)', 
          background: 'var(--bg-primary)', 
          animation: 'fadeIn 0.4s ease', 
          marginTop: 'auto', 
          marginBottom: 'auto',
          zIndex: 1
        }} 
        onClick={e => e.stopPropagation()}
      >
        <div className="flex items-center gap-3" style={{ marginBottom: '24px' }}>
          <span className="material-symbols-outlined" style={{ fontSize: '32px', color: 'var(--status-online)' }}>check_circle</span>
          <h2 className="font-display" style={{ fontSize: '24px', fontWeight: '700', color: 'var(--text-primary)' }}>
            {data.message || "Agent Reinstall"}
          </h2>
        </div>
        
        <div style={{ display: 'flex', flexDirection: 'column', gap: '24px' }}>
          {data.messages.map((m, idx) => (
            <div key={idx}>
              <h4 style={{ fontSize: '11px', fontWeight: '900', color: 'var(--text-muted)', textTransform: 'uppercase', letterSpacing: '0.1em', marginBottom: '10px' }}>
                {m.name}
              </h4>
              
              {m.message_type === 'command' && (
                <div className="font-mono" style={{ background: '#000', padding: '24px 20px', borderRadius: '12px', border: '1px solid var(--border-color)', position: 'relative', overflowX: 'auto' }}>
                  <pre style={{ fontSize: '12px', color: '#10b981', margin: 0, whiteSpace: 'pre-wrap', wordBreak: 'break-all', lineHeight: '1.6' }}>
                    {m.content}
                  </pre>
                  <button
                    type="button"
                    onClick={() => handleCopy(m.content, idx, "Command")}
                    style={{ 
                      position: 'absolute', 
                      top: '12px', 
                      right: '12px', 
                      padding: '6px', 
                      borderRadius: '6px', 
                      background: 'rgba(255,255,255,0.1)', 
                      color: '#fff', 
                      border: 'none', 
                      cursor: 'pointer',
                      display: 'flex'
                    }}
                  >
                    <span className="material-symbols-outlined" style={{ fontSize: '18px' }}>
                      {copiedIndex[idx] ? "check" : "content_copy"}
                    </span>
                  </button>
                </div>
              )}

              {m.message_type === 'copy' && (
                <div className="flex items-center gap-2" style={{ background: 'rgba(255,255,255,0.03)', padding: '12px', borderRadius: '8px', border: '1px solid var(--border-color)' }}>
                  <code style={{ fontSize: '12px', color: 'var(--accent-primary)', flex: 1, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
                    {m.content}
                  </code>
                  <button
                    type="button"
                    onClick={() => handleCopy(m.content, idx, "Key")}
                    style={{ background: 'none', border: 'none', color: 'var(--text-muted)', display: 'flex', cursor: 'pointer' }}
                  >
                    <span className="material-symbols-outlined" style={{ fontSize: '18px' }}>
                      {copiedIndex[idx] ? "check" : "content_copy"}
                    </span>
                  </button>
                </div>
              )}

              {m.message_type === 'big_copy' && (
                <div 
                  className="mobile-stack" 
                  style={{ 
                    display: 'flex', 
                    alignItems: 'center', 
                    justifyContent: 'space-between', 
                    gap: '24px', 
                    background: 'var(--bg-secondary)', 
                    padding: '24px 28px', 
                    borderRadius: '16px', 
                    border: '1px dashed var(--border-color)',
                    transition: 'all 0.2s ease'
                  }}
                >
                  <div className="flex items-center gap-4">
                    <span className="material-symbols-outlined" style={{ color: 'var(--accent-primary)', fontSize: '32px' }}>data_object</span>
                    <div className="flex flex-col">
                      <span style={{ fontSize: '14px', fontWeight: '700', color: 'var(--text-primary)' }}>
                        {m.name || "Large Configuration File"}
                      </span>
                      <span style={{ fontSize: '12px', color: 'var(--text-secondary)', marginTop: '2px' }}>
                        {m.description || "Content hidden for performance. Click copy to use."}
                      </span>
                    </div>
                  </div>
                  <button
                    type="button"
                    onClick={() => handleCopy(m.content, idx, "Content")}
                    className="btn-primary mobile-full"
                    style={{ 
                      padding: '10px 20px', 
                      borderRadius: '10px', 
                      fontSize: '13px', 
                      background: copiedIndex[idx] ? 'var(--status-online)' : 'var(--accent-primary)',
                      color: copiedIndex[idx] ? '#000' : '#fff',
                      border: 'none',
                      cursor: 'pointer',
                      display: 'flex',
                      alignItems: 'center',
                      justifyContent: 'center',
                      gap: '8px',
                      boxShadow: copiedIndex[idx] ? 'none' : '0 4px 12px var(--accent-glow)'
                    }}
                  >
                    <span className="material-symbols-outlined" style={{ fontSize: '20px' }}>
                      {copiedIndex[idx] ? "check" : "content_copy"}
                    </span>
                    {copiedIndex[idx] ? "Copied!" : "Copy Content"}
                  </button>
                </div>
              )}

              {m.message_type === 'note' && (
                <div
                  style={{ 
                    background: 'rgba(99, 102, 241, 0.05)', 
                    padding: '16px', 
                    borderRadius: '8px', 
                    border: '1px solid var(--border-color)', 
                    fontSize: '13px', 
                    color: 'var(--text-secondary)', 
                    lineHeight: '1.6', 
                    whiteSpace: 'pre-wrap' 
                  }}
                  dangerouslySetInnerHTML={{ __html: m.content }}
                />
              )}

              {m.message_type === 'warning' && (
                <div
                  style={{ 
                    background: 'rgba(239, 68, 68, 0.05)', 
                    padding: '16px', 
                    borderRadius: '8px', 
                    border: '1px solid rgba(239, 68, 68, 0.2)', 
                    fontSize: '13px', 
                    color: 'var(--status-offline)', 
                    fontWeight: '500', 
                    lineHeight: '1.6', 
                    display: 'flex', 
                    gap: '12px' 
                  }}
                >
                  <span className="material-symbols-outlined" style={{ fontSize: '20px', color: 'var(--status-offline)' }}>warning</span>
                  <div dangerouslySetInnerHTML={{ __html: m.content }} />
                </div>
              )}
            </div>
          ))}
        </div>

        <div style={{ display: 'flex', justifyContent: 'flex-end', marginTop: '32px' }}>
          <button 
            type="button" 
            onClick={onClose} 
            className="btn-primary" 
            style={{ padding: '10px 24px', background: 'var(--accent-primary)', color: '#fff', border: 'none' }}
          >
            Done
          </button>
        </div>
      </div>
    </div>,
    document.body
  );
}
