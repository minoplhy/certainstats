import { useEffect, useState, useMemo } from "react";
import { useNavigate } from "react-router-dom";
import { fetchAPI } from "../../lib/api";
import { AgentManagement } from "../../types";
import ReinstallModal from "../common/ReinstallModal";
import DeleteConfirmModal from "../common/DeleteConfirmModal";
import { useApp } from "../../context/AppContext";

export default function ManagementView() {
  const navigate = useNavigate();
  const { showToast } = useApp();
  const [agents, setAgents] = useState<AgentManagement[]>([]);
  const [loading, setLoading] = useState(true);
  const [filter, setFilter] = useState("");
  const [resetting, setResetting] = useState<string | null>(null);

  const [confirmModal, setConfirmModal] = useState<{ type: 'token' | 'ssh' | 'revoke', agent: AgentManagement } | null>(null);
  const [successModal, setSuccessModal] = useState<{ type: 'token' | 'ssh', agent: AgentManagement, value: string } | null>(null);
  const [provisionResult, setProvisionResult] = useState<any | null>(null);
  const [copied, setCopied] = useState(false);

  const loadData = async () => {
    try {
      const data = await fetchAPI<AgentManagement[]>("/api/agents/management");
      setAgents(data || []);
    } catch (err: any) {
      if (err.status === 401) {
        navigate("/login", { replace: true });
      } else {
        showToast("Failed to load management data", false);
      }
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    loadData();
  }, []);



  const copyToClipboard = (text: string, label: string) => {
    navigator.clipboard.writeText(text).then(() => {
      showToast(`${label} copied to clipboard`);
    });
  };

  const handleResetToken = async () => {
    if (!confirmModal) return;
    const { agent } = confirmModal;
    setResetting(agent.agent_id);
    setConfirmModal(null);
    try {
      const res = await fetchAPI<{ token: string }>(`/api/agent/reset/token/${agent.agent_id}`, { method: "POST" });
      setAgents(prev => prev.map(a => a.agent_id === agent.agent_id ? { ...a, token: res.token } : a));
      setSuccessModal({ type: 'token', agent, value: res.token });
    } catch (err: any) {
      showToast(err.message || "Reset failed", false);
    } finally {
      setResetting(null);
    }
  };

  const handleResetSSH = async () => {
    if (!confirmModal) return;
    const { agent } = confirmModal;
    setResetting(agent.agent_id);
    setConfirmModal(null);
    try {
      const res = await fetchAPI<{ public_key: string }>(`/api/agent/reset/ssh/${agent.agent_id}`, { method: "POST" });
      setAgents(prev => prev.map(a => a.agent_id === agent.agent_id ? { ...a, beszel_public_key: res.public_key } : a));
      setSuccessModal({ type: 'ssh', agent, value: res.public_key });
    } catch (err: any) {
      showToast(err.message || "Reset failed", false);
    } finally {
      setResetting(null);
    }
  };

  const handleRevoke = async () => {
    if (!confirmModal) return;
    const { agent } = confirmModal;
    setResetting(agent.agent_id);
    setConfirmModal(null);
    try {
      await fetchAPI(`/api/agent?agent_id=${agent.agent_id}`, { method: "DELETE" });
      showToast("Agent revoked successfully");
      setAgents(prev => prev.filter(a => a.agent_id !== agent.agent_id));
    } catch (err: any) {
      showToast(err.message || "Revoke failed", false);
    } finally {
      setResetting(null);
    }
  };

  const handleReinstall = async (agentId: string) => {
    try {
      const res = await fetchAPI<any>(`/api/agent/install/${agentId}`);
      setProvisionResult(res);
    } catch (err: any) {
      showToast(err.message || "Failed to fetch installation instructions", false);
    }
  };

  const filtered = useMemo(() => {
    const q = filter.toLowerCase();
    return agents.filter(a =>
      a.nickname.toLowerCase().includes(q) ||
      a.agent_id.toLowerCase().includes(q) ||
      a.agent_type.toLowerCase().includes(q)
    );
  }, [agents, filter]);

  return (
    <div style={{ padding: '32px', maxWidth: '1400px', margin: '0 auto', width: '100%' }}>
      <div className="animate-fade-in">
            <div className="flex justify-between items-center" style={{ marginBottom: '32px' }}>
              <div>
                <h1 className="font-display" style={{ fontSize: '28px', fontWeight: '700', marginBottom: '8px' }}>Agents Management</h1>
                <p style={{ color: 'var(--text-secondary)', fontSize: '14px' }}>Securely manage authentication tokens and unique SSH identities for your agent.</p>
              </div>
              <div className="flex gap-4">
                <input
                  type="text"
                  placeholder="Search agents..."
                  value={filter}
                  onChange={(e) => setFilter(e.target.value)}
                  className="input-field"
                  style={{ width: '300px' }}
                />
              </div>
            </div>

            {loading ? (
              <div style={{ padding: '100px', textAlign: 'center', color: 'var(--text-muted)' }}>
                <span className="material-symbols-outlined animate-spin" style={{ fontSize: '32px' }}>sync</span>
              </div>
            ) : (
              <div className="glass-panel" style={{ overflow: 'hidden', border: '1px solid var(--border-color)' }}>
                <div style={{ overflowX: 'auto' }}>
                  <table style={{ width: '100%', borderCollapse: 'collapse', textAlign: 'left', fontSize: '13px' }}>
                    <thead>
                      <tr style={{ borderBottom: '1px solid var(--border-color)', background: 'var(--bg-secondary)' }}>
                        <th style={{ padding: '16px 24px', fontWeight: '600', color: 'var(--text-secondary)', textTransform: 'uppercase', fontSize: '11px', letterSpacing: '0.1em' }}>Agent</th>
                        <th style={{ padding: '16px 24px', fontWeight: '600', color: 'var(--text-secondary)', textTransform: 'uppercase', fontSize: '11px', letterSpacing: '0.1em' }}>Type</th>
                        <th style={{ padding: '16px 24px', fontWeight: '600', color: 'var(--text-secondary)', textTransform: 'uppercase', fontSize: '11px', letterSpacing: '0.1em' }}>Auth Token</th>
                        <th style={{ padding: '16px 24px', fontWeight: '600', color: 'var(--text-secondary)', textTransform: 'uppercase', fontSize: '11px', letterSpacing: '0.1em' }}>Beszel SSH Public Key</th>
                        <th style={{ padding: '16px 24px', fontWeight: '600', color: 'var(--text-secondary)', textTransform: 'uppercase', fontSize: '11px', letterSpacing: '0.1em' }}>Management</th>
                        <th style={{ padding: '16px 24px', fontWeight: '600', color: 'var(--text-secondary)', textTransform: 'uppercase', fontSize: '11px', letterSpacing: '0.1em', textAlign: 'right' }}>Actions</th>
                      </tr>
                    </thead>
                    <tbody>
                      {filtered.map((agent) => (
                        <tr key={agent.agent_id} style={{ borderBottom: '1px solid var(--border-color)', transition: 'background 0.2s' }} className="hover-row">
                          <td style={{ padding: '16px 24px' }}>
                            <div style={{ fontWeight: '600', color: 'var(--text-primary)' }}>{agent.nickname}</div>
                            <div style={{ fontSize: '11px', color: 'var(--text-muted)', fontFamily: 'var(--font-mono)', marginTop: '2px' }}>{agent.agent_id}</div>
                          </td>
                          <td style={{ padding: '16px 24px' }}>
                            <span className="badge" style={{ background: 'var(--bg-secondary)', color: 'var(--text-secondary)', fontSize: '10px', textTransform: 'uppercase' }}>
                              {agent.agent_type}
                            </span>
                          </td>
                          <td style={{ padding: '16px 24px' }}>
                            <div className="flex items-center gap-2">
                              <code style={{ background: 'rgba(0,0,0,0.2)', padding: '4px 8px', borderRadius: '4px', fontSize: '12px', color: 'var(--status-online)' }}>
                                {agent.token.substring(0, 8)}...{agent.token.substring(agent.token.length - 4)}
                              </code>
                              <button onClick={() => copyToClipboard(agent.token, "Token")} className="text-muted hover:text-primary transition-colors">
                                <span className="material-symbols-outlined" style={{ fontSize: '16px' }}>content_copy</span>
                              </button>
                            </div>
                          </td>
                          <td style={{ padding: '16px 24px' }}>
                            {agent.agent_type === 'beszel' ? (
                              <div className="flex items-center gap-2">
                                <div style={{
                                  maxWidth: '240px',
                                  overflow: 'hidden',
                                  textOverflow: 'ellipsis',
                                  whiteSpace: 'nowrap',
                                  fontSize: '11px',
                                  fontFamily: 'var(--font-mono)',
                                  color: 'var(--text-secondary)',
                                  background: 'rgba(255,255,255,0.03)',
                                  padding: '4px 8px',
                                  borderRadius: '4px'
                                }}>
                                  {agent.beszel_public_key || "Not generated"}
                                </div>
                                {agent.beszel_public_key && (
                                  <button onClick={() => copyToClipboard(agent.beszel_public_key, "SSH Key")} className="text-muted hover:text-primary transition-colors">
                                    <span className="material-symbols-outlined" style={{ fontSize: '16px' }}>content_copy</span>
                                  </button>
                                )}
                              </div>
                            ) : (
                              <span style={{ color: 'var(--text-muted)', fontStyle: 'italic', fontSize: '12px' }}>N/A</span>
                            )}
                          </td>
                          <td style={{ padding: '16px 24px' }}>
                            <div className="flex gap-2">
                              <button
                                onClick={() => handleReinstall(agent.agent_id)}
                                className="btn-secondary"
                                style={{ fontSize: '12px', padding: '6px 12px', background: 'rgba(59, 130, 246, 0.1)', color: 'var(--accent-primary)', borderColor: 'rgba(59, 130, 246, 0.2)' }}
                                title="Show Installation Instructions"
                              >
                                <span className="material-symbols-outlined" style={{ fontSize: '16px', marginRight: '4px' }}>terminal</span>
                                Reinstall
                              </button>
                              <button
                                onClick={() => setConfirmModal({ type: 'token', agent })}
                                disabled={resetting === agent.agent_id}
                                className="btn-secondary"
                                style={{ fontSize: '12px', padding: '6px 12px' }}
                                title="Reset Auth Token"
                              >
                                <span className="material-symbols-outlined" style={{ fontSize: '16px', marginRight: '4px' }}>refresh</span>
                                Token
                              </button>
                              {agent.agent_type === 'beszel' && (
                                <button
                                  onClick={() => setConfirmModal({ type: 'ssh', agent })}
                                  disabled={resetting === agent.agent_id}
                                  className="btn-primary"
                                  style={{ fontSize: '12px', padding: '6px 12px' }}
                                  title="Reset SSH Key"
                                >
                                  <span className="material-symbols-outlined" style={{ fontSize: '16px', marginRight: '4px' }}>vpn_key</span>
                                  SSH
                                </button>
                              )}
                            </div>
                          </td>
                          <td style={{ padding: '16px 24px', textAlign: 'right' }}>
                            <button
                              onClick={() => setConfirmModal({ type: 'revoke', agent })}
                              disabled={resetting === agent.agent_id}
                              className="btn-secondary"
                              style={{ fontSize: '12px', padding: '8px', color: 'var(--status-offline)', border: '1px solid rgba(239, 68, 68, 0.2)' }}
                              title="Revoke Agent"
                            >
                              <span className="material-symbols-outlined" style={{ fontSize: '18px' }}>delete</span>
                            </button>
                          </td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                </div>
                {filtered.length === 0 && (
                  <div style={{ padding: '48px', textAlign: 'center', color: 'var(--text-muted)' }}>
                    No agents found matching your search.
                  </div>
                )}
              </div>
            )}
          </div>

          {/* CONFIRM MODAL */}
          {confirmModal && confirmModal.type === 'revoke' && (
            <DeleteConfirmModal
              isOpen={true}
              title="Revoke Agent?"
              message={
                <>Are you sure you want to permanently revoke <strong style={{ color: 'var(--text-primary)' }}>{confirmModal.agent.nickname}</strong>? This will delete all associated data and identities.</>
              }
              confirmText="Revoke Permanently"
              onClose={() => setConfirmModal(null)}
              onConfirm={handleRevoke}
            />
          )}

          {confirmModal && confirmModal.type !== 'revoke' && (
            <div
              style={{ position: 'fixed', inset: 0, display: 'flex', alignItems: 'center', justifyContent: 'center', zIndex: 9999, padding: '16px' }}
            >
              <div
                onClick={() => setConfirmModal(null)}
                className="modal-backdrop"
              />
              <div className="card" style={{ width: '100%', maxWidth: '440px', padding: '32px', border: '1px solid var(--border-color)', background: 'var(--bg-primary)', animation: 'fadeIn 0.3s ease', zIndex: 1 }} onClick={e => e.stopPropagation()}>
                <div style={{ width: '56px', height: '56px', borderRadius: '50%', background: 'rgba(239, 68, 68, 0.1)', display: 'flex', alignItems: 'center', justifyContent: 'center', color: 'var(--status-offline)', marginBottom: '20px' }}>
                  <span className="material-symbols-outlined" style={{ fontSize: '32px' }}>warning</span>
                </div>
                <h3 className="text-xl font-display font-semibold mb-2" style={{ color: 'var(--text-primary)' }}>
                  Reset {confirmModal.type === 'ssh' ? 'SSH Key' : 'Auth Token'}?
                </h3>
                <p className="text-sm mb-8" style={{ color: 'var(--text-secondary)', lineHeight: '1.6' }}>
                  Are you sure you want to reset the {confirmModal.type === 'ssh' ? 'SSH identity' : 'security token'} for <strong style={{ color: 'var(--text-primary)' }}>{confirmModal.agent.nickname}</strong>? This will immediately disconnect the agent until its configuration is updated.
                </p>
                <div style={{ display: 'flex', justifyContent: 'flex-end', gap: '12px' }}>
                  <button type="button" onClick={() => setConfirmModal(null)} className="btn-secondary" style={{ padding: '10px 20px' }}>Cancel</button>
                  <button
                    type="button"
                    onClick={confirmModal.type === 'ssh' ? handleResetSSH : handleResetToken}
                    className="btn-primary"
                    style={{ background: 'var(--status-offline)', color: '#fff', border: 'none', padding: '10px 24px', boxShadow: '0 4px 12px rgba(239, 68, 68, 0.2)' }}
                  >
                    Reset Identity
                  </button>
                </div>
              </div>
            </div>
          )}

          {/* SUCCESS MODAL */}
          {successModal && (
            <div
              style={{ position: 'fixed', inset: 0, display: 'flex', alignItems: 'center', justifyContent: 'center', zIndex: 9999, padding: '16px' }}
            >
              <div
                onClick={() => setSuccessModal(null)}
                className="modal-backdrop"
              />
              <div className="card" style={{ width: '100%', maxWidth: '520px', padding: '40px', border: '1px solid var(--border-color)', background: 'var(--bg-primary)', animation: 'fadeIn 0.4s cubic-bezier(0.16, 1, 0.3, 1)', zIndex: 1 }} onClick={e => e.stopPropagation()}>
                <div className="flex items-center gap-4 mb-6">
                  <div style={{ width: '48px', height: '48px', borderRadius: '50%', background: 'rgba(16, 185, 129, 0.1)', display: 'flex', alignItems: 'center', justifyContent: 'center', color: 'var(--status-online)' }}>
                    <span className="material-symbols-outlined" style={{ fontSize: '28px' }}>check</span>
                  </div>
                  <div>
                    <h3 className="text-2xl font-display font-bold" style={{ color: 'var(--text-primary)' }}>Reset Successful</h3>
                    <p className="text-xs uppercase tracking-wider" style={{ color: 'var(--text-muted)' }}>Agent: {successModal.agent.nickname}</p>
                  </div>
                </div>

                <div className="mb-8">
                  <p className="text-sm font-semibold mb-3" style={{ color: 'var(--text-primary)' }}>New {successModal.type === 'ssh' ? 'SSH Public Key' : 'Security Token'}</p>
                  <div style={{ position: 'relative' }}>
                    <div style={{
                      background: 'rgba(0,0,0,0.3)',
                      border: '1px solid var(--border-color)',
                      borderRadius: '12px',
                      padding: '20px',
                      fontFamily: 'var(--font-mono)',
                      fontSize: '12px',
                      color: 'var(--accent-primary)',
                      wordBreak: 'break-all',
                      lineHeight: '1.6'
                    }}>
                      {successModal.value}
                    </div>
                    <button
                      type="button"
                      onClick={() => copyToClipboard(successModal.value, successModal.type === 'ssh' ? "SSH Key" : "Token")}
                      style={{ position: 'absolute', top: '12px', right: '12px', padding: '6px', background: 'var(--bg-secondary)', borderRadius: '6px', border: '1px solid var(--border-color)', cursor: 'pointer' }}
                    >
                      <span className="material-symbols-outlined" style={{ fontSize: '16px' }}>content_copy</span>
                    </button>
                  </div>
                  <p className="text-xs mt-3" style={{ color: 'var(--text-muted)' }}>
                    {successModal.type === 'ssh' ? 'Update the public key in your agent config file.' : 'Replace the old token in your agent environment variables.'}
                  </p>
                </div>

                <div style={{ display: 'flex', justifyContent: 'flex-end' }}>
                  <button type="button" onClick={() => setSuccessModal(null)} className="btn-primary" style={{ minWidth: '120px', padding: '10px 24px', background: 'var(--accent-primary)', color: '#fff', border: 'none' }}>Close</button>
                </div>
              </div>
            </div>
          )}

          {/* PROVISION RESULT MODAL */}
          <ReinstallModal
            data={provisionResult}
            onClose={() => setProvisionResult(null)}
            showToast={(msg) => showToast(msg, true)}
          />

          <style>{`
        .hover-row:hover {
          background: rgba(255, 255, 255, 0.02);
        }
        @keyframes slideInRight {
          from { transform: translateX(100%); opacity: 0; }
          to { transform: translateX(0); opacity: 1; }
        }
        @keyframes fadeIn {
          from { opacity: 0; transform: scale(0.95); }
          to { opacity: 1; transform: scale(1); }
        }
      `}</style>
        </div>
  );
}
