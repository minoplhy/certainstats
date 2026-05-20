import { ReactNode } from "react";
import { createPortal } from "react-dom";

interface DeleteConfirmModalProps {
  isOpen: boolean;
  title: string;
  message: ReactNode;
  confirmText: string;
  onClose: () => void;
  onConfirm: () => Promise<void> | void;
  deleting?: boolean;
  dangerColor?: string; // e.g. 'var(--status-offline)'
}

export default function DeleteConfirmModal({
  isOpen,
  title,
  message,
  confirmText,
  onClose,
  onConfirm,
  deleting = false,
  dangerColor = 'var(--status-offline)'
}: DeleteConfirmModalProps) {
  if (!isOpen) return null;

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
          maxWidth: '440px', 
          padding: '32px', 
          border: '1px solid var(--border-color)', 
          background: 'var(--bg-primary)', 
          animation: 'fadeIn 0.3s ease',
          zIndex: 1
        }} 
        onClick={e => e.stopPropagation()}
      >
        <div 
          style={{ 
            width: '56px', 
            height: '56px', 
            borderRadius: '50%', 
            background: 'rgba(239, 68, 68, 0.1)', 
            display: 'flex', 
            alignItems: 'center', 
            justifyContent: 'center', 
            color: dangerColor, 
            marginBottom: '20px' 
          }}
        >
          <span className="material-symbols-outlined" style={{ fontSize: '32px' }}>delete_forever</span>
        </div>
        <h3 className="text-xl font-display font-semibold mb-2" style={{ color: 'var(--text-primary)' }}>
          {title}
        </h3>
        <div className="text-sm mb-8" style={{ color: 'var(--text-secondary)', lineHeight: '1.6' }}>
          {message}
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
            onClick={onConfirm} 
            disabled={deleting} 
            className="btn-primary" 
            style={{ 
              background: dangerColor, 
              color: '#fff', 
              border: 'none', 
              padding: '10px 24px', 
              boxShadow: `0 4px 12px rgba(239, 68, 68, 0.2)` 
            }}
          >
            {deleting ? "Processing…" : confirmText}
          </button>
        </div>
      </div>
    </div>,
    document.body
  );
}
