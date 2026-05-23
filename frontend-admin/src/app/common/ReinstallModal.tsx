import { useState } from "react";
import { createPortal } from "react-dom";

export interface ReinstallMessage {
  name: string;
  content: string;
  message_type: 'copy' | 'command' | 'note' | 'big_copy' | 'warning' | 'tabs' | 'tab';
  description?: string;
  children?: ReinstallMessage[];
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

interface InstructionItemProps {
  message: ReinstallMessage;
  copiedIndexMap: Record<string, boolean>;
  onCopy: (text: string, key: string, label: string) => void;
  parentKey: string;
}

export function InstructionItem({ message, copiedIndexMap, onCopy, parentKey }: InstructionItemProps) {
  const [activeTab, setActiveTab] = useState(0);

  // A. Handle Tab Containers ("tabs")
  if (message.message_type === 'tabs' && message.children && message.children.length > 0) {
    return (
      <div style={{ display: 'flex', flexDirection: 'column', gap: '16px', marginTop: '8px' }}>
        {/* Tab Header Selector */}
        <div 
          style={{ 
            display: 'flex', 
            borderBottom: '1px solid var(--border-color)', 
            gap: '8px', 
            overflowX: 'auto',
            paddingBottom: '2px',
            scrollbarWidth: 'none'
          }}
        >
          {message.children.map((tab, idx) => (
            <button
              key={idx}
              type="button"
              onClick={() => setActiveTab(idx)}
              style={{
                padding: '10px 16px',
                border: 'none',
                background: 'none',
                color: activeTab === idx ? 'var(--accent-primary)' : 'var(--text-secondary)',
                borderBottom: activeTab === idx ? '2px solid var(--accent-primary)' : '2px solid transparent',
                cursor: 'pointer',
                fontWeight: activeTab === idx ? '700' : '500',
                fontSize: '13px',
                transition: 'all 0.2s ease',
                whiteSpace: 'nowrap'
              }}
            >
              {tab.name}
            </button>
          ))}
        </div>
        
        {/* Active Tab Panel */}
        <div style={{ display: 'flex', flexDirection: 'column', gap: '20px', padding: '8px 0' }}>
          {message.children[activeTab].children?.map((childMsg, childIdx) => (
            <InstructionItem
              key={childIdx}
              message={childMsg}
              copiedIndexMap={copiedIndexMap}
              onCopy={onCopy}
              parentKey={`${parentKey}-tab-${activeTab}-${childIdx}`}
            />
          ))}
        </div>
      </div>
    );
  }

  // B. Handle Raw Tab Wrapper ("tab")
  if (message.message_type === 'tab') {
    return (
      <div style={{ display: 'flex', flexDirection: 'column', gap: '20px' }}>
        {message.children?.map((childMsg, childIdx) => (
          <InstructionItem
            key={childIdx}
            message={childMsg}
            copiedIndexMap={copiedIndexMap}
            onCopy={onCopy}
            parentKey={`${parentKey}-${childIdx}`}
          />
        ))}
      </div>
    );
  }

  // C. Standard Leaf Instruction Rendering
  const copiedKey = `${parentKey}-${message.name}-${message.message_type}`;
  return (
    <div>
      <h4 style={{ fontSize: '11px', fontWeight: '900', color: 'var(--text-muted)', textTransform: 'uppercase', letterSpacing: '0.1em', marginBottom: '10px', textAlign: 'left' }}>
        {message.name}
      </h4>
      
      {message.message_type === 'command' && (
        <div style={{ position: 'relative' }}>
          <div className="font-mono" style={{ background: '#000', padding: '24px 20px', borderRadius: '12px', border: '1px solid var(--border-color)', overflowX: 'auto' }}>
            <pre style={{ fontSize: '12px', color: '#10b981', margin: 0, whiteSpace: 'pre', lineHeight: '1.6', textAlign: 'left', paddingRight: '40px' }}>
              {message.content}
            </pre>
          </div>
          <button
            type="button"
            onClick={() => onCopy(message.content, copiedKey, "Command")}
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
              {copiedIndexMap[copiedKey] ? "check" : "content_copy"}
            </span>
          </button>
        </div>
      )}

      {message.message_type === 'copy' && (
        <div className="flex items-center gap-2" style={{ background: 'rgba(255,255,255,0.03)', padding: '12px', borderRadius: '8px', border: '1px solid var(--border-color)' }}>
          <code style={{ fontSize: '12px', color: 'var(--accent-primary)', flex: 1, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap', textAlign: 'left' }}>
            {message.content}
          </code>
          <button
            type="button"
            onClick={() => onCopy(message.content, copiedKey, "Key")}
            style={{ background: 'none', border: 'none', color: 'var(--text-muted)', display: 'flex', cursor: 'pointer' }}
          >
            <span className="material-symbols-outlined" style={{ fontSize: '18px' }}>
              {copiedIndexMap[copiedKey] ? "check" : "content_copy"}
            </span>
          </button>
        </div>
      )}

      {message.message_type === 'big_copy' && (
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
            <div className="flex flex-col text-left">
              <span style={{ fontSize: '14px', fontWeight: '700', color: 'var(--text-primary)' }}>
                {message.name || "Large Configuration File"}
              </span>
              <span style={{ fontSize: '12px', color: 'var(--text-secondary)', marginTop: '2px' }}>
                {message.description || "Content hidden for performance. Click copy to use."}
              </span>
            </div>
          </div>
          <button
            type="button"
            onClick={() => onCopy(message.content, copiedKey, "Content")}
            className="btn-primary mobile-full"
            style={{ 
              padding: '10px 20px', 
              borderRadius: '10px', 
              fontSize: '13px', 
              background: copiedIndexMap[copiedKey] ? 'var(--status-online)' : 'var(--accent-primary)',
              color: copiedIndexMap[copiedKey] ? '#000' : '#fff',
              border: 'none',
              cursor: 'pointer',
              display: 'flex',
              alignItems: 'center',
              justifyContent: 'center',
              gap: '8px',
              boxShadow: copiedIndexMap[copiedKey] ? 'none' : '0 4px 12px var(--accent-glow)'
            }}
          >
            <span className="material-symbols-outlined" style={{ fontSize: '20px' }}>
              {copiedIndexMap[copiedKey] ? "check" : "content_copy"}
            </span>
            {copiedIndexMap[copiedKey] ? "Copied!" : "Copy Content"}
          </button>
        </div>
      )}

      {message.message_type === 'note' && (
        <div
          style={{ 
            background: 'rgba(99, 102, 241, 0.05)', 
            padding: '16px', 
            borderRadius: '8px', 
            border: '1px solid var(--border-color)', 
            fontSize: '13px', 
            color: 'var(--text-secondary)', 
            lineHeight: '1.6', 
            whiteSpace: 'pre-wrap',
            textAlign: 'left'
          }}
          dangerouslySetInnerHTML={{ __html: message.content }}
        />
      )}

      {message.message_type === 'warning' && (
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
            gap: '12px',
            textAlign: 'left'
          }}
        >
          <span className="material-symbols-outlined" style={{ fontSize: '20px', color: 'var(--status-offline)' }}>warning</span>
          <div dangerouslySetInnerHTML={{ __html: message.content }} />
        </div>
      )}
    </div>
  );
}

export default function ReinstallModal({ data, onClose, showToast }: ReinstallModalProps) {
  const [copiedKeyMap, setCopiedKeyMap] = useState<Record<string, boolean>>({});

  if (!data) return null;

  const handleCopy = (text: string, key: string, label: string) => {
    navigator.clipboard.writeText(text);
    setCopiedKeyMap(prev => ({ ...prev, [key]: true }));
    if (showToast) {
      showToast(`${label} copied to clipboard`);
    }
    setTimeout(() => {
      setCopiedKeyMap(prev => ({ ...prev, [key]: false }));
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
          <h2 className="font-display" style={{ fontSize: '24px', fontWeight: '700', color: 'var(--text-primary)', textAlign: 'left' }}>
            {data.message || "Agent Reinstall"}
          </h2>
        </div>
        
        <div style={{ display: 'flex', flexDirection: 'column', gap: '24px' }}>
          {data.messages.map((m, idx) => (
            <InstructionItem
              key={idx}
              message={m}
              copiedIndexMap={copiedKeyMap}
              onCopy={handleCopy}
              parentKey={`root-${idx}`}
            />
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
