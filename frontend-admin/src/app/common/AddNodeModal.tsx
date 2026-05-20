import { useState } from "react";
import { createPortal } from "react-dom";

interface AddNodeModalProps {
  isOpen: boolean;
  onClose: () => void;
  onConfirm: (type: string) => Promise<void> | void;
  confirming?: boolean;
}

export default function AddNodeModal({
  isOpen,
  onClose,
  onConfirm,
  confirming = false
}: AddNodeModalProps) {
  const [selectedType, setSelectedType] = useState<string>("beszel");

  if (!isOpen) return null;

  const handleContinue = () => {
    onConfirm(selectedType);
  };

  const agents = [
    { id: 'beszel', name: 'Beszel', desc: 'Lightweight server monitoring written in Go(Submission only support via WebSocket)' },
    { id: 'ltstats', name: 'LTstats', desc: 'Lightweight resource monitoring system written in C' },
    { id: 'hetrixtools', name: 'HetrixTools', desc: 'HetrixTools Server Monitoring Agent (Linux) written in Bash' }
  ];

  return createPortal(
    <div
      style={{
        position: 'fixed',
        inset: 0,
        display: 'flex',
        alignItems: 'center',
        justifyContent: 'center',
        zIndex: 99999,
        padding: '16px'
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
          maxWidth: '520px',
          padding: '32px',
          border: '1px solid var(--border-color)',
          background: 'var(--bg-primary)',
          animation: 'fadeIn 0.3s ease',
          zIndex: 1
        }}
        onClick={e => e.stopPropagation()}
      >
        <h2 className="font-display" style={{ fontSize: '24px', fontWeight: '700', marginBottom: '8px', color: 'var(--text-primary)' }}>
          Select Agent Type
        </h2>
        <p style={{ color: 'var(--text-secondary)', marginBottom: '24px', fontSize: '14px' }}>
          Choose the monitoring protocol for your new agent.
        </p>

        <div style={{ display: 'flex', flexDirection: 'column', gap: '12px', marginBottom: '32px' }}>
          {agents.map(t => {
            const isSelected = selectedType === t.id;
            return (
              <button
                key={t.id}
                type="button"
                onClick={() => setSelectedType(t.id)}
                style={{
                  padding: '16px',
                  borderRadius: '12px',
                  border: '2px solid',
                  borderColor: isSelected ? 'var(--accent-primary)' : 'var(--border-color)',
                  background: isSelected ? 'rgba(99, 102, 241, 0.08)' : 'var(--bg-primary)',
                  textAlign: 'left',
                  transition: 'var(--transition-fast)',
                  cursor: 'pointer',
                  display: 'block',
                  width: '100%'
                }}
              >
                <div style={{ fontSize: '15px', fontWeight: '700', color: isSelected ? 'var(--text-primary)' : 'var(--text-secondary)' }}>
                  {t.name}
                </div>
                <div style={{
                  fontSize: '13px',
                  color: 'var(--text-primary)',
                  opacity: isSelected ? 0.8 : 0.6,
                  marginTop: '4px'
                }}>
                  {t.desc}
                </div>
              </button>
            );
          })}
        </div>

        <div style={{ display: 'flex', justifyContent: 'flex-end', gap: '12px' }}>
          <button
            type="button"
            onClick={onClose}
            className="btn-secondary"
            style={{ padding: '10px 20px' }}
          >
            Cancel
          </button>
          <button
            type="button"
            onClick={handleContinue}
            disabled={confirming}
            className="btn-primary"
            style={{ padding: '10px 24px', background: 'var(--accent-primary)', color: '#fff', border: 'none' }}
          >
            {confirming ? "Generating…" : "Continue"}
          </button>
        </div>
      </div>
    </div>,
    document.body
  );
}
