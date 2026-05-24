import { useState } from "react";
import { useNavigate } from "react-router-dom";
import { fetchAPI } from "../../lib/api";
import { AlertTarget } from "../../types";

function formatDisplayURL(url: string): string {
  if (!url) return "";
  if (url.length > 40) {
    return url.substring(0, 37) + "...";
  }
  return url;
}


export default function AlertTargetsTab({
  targets,
  setTargets,
  isLoadingTargets,
}: {
  targets: AlertTarget[];
  setTargets: React.Dispatch<React.SetStateAction<AlertTarget[]>>;
  isLoadingTargets: boolean;
}) {
  const navigate = useNavigate();
  const [loading, setLoading] = useState(false);
  const [message, setMessage] = useState<{ type: "success" | "error"; text: string } | null>(null);

  const handleDelete = async (targetID: string) => {
    if (!confirm("Are you sure you want to delete this alert target? Any alerts referencing it will fall back or fail.")) return;
    setLoading(true);
    setMessage(null);
    try {
      await fetchAPI(`/api/alerts/targets/${targetID}`, { method: "DELETE" });
      setTargets(prev => prev.filter(t => t.target_id !== targetID));
      setMessage({ type: "success", text: "Alert target deleted successfully" });
    } catch (err: any) {
      setMessage({ type: "error", text: err.message || "Failed to delete target" });
    } finally {
      setLoading(false);
    }
  };

  return (
    <div className="flex-col gap-6 w-full">
      {message && (
        <div style={{
          padding: "12px 16px",
          borderRadius: "8px",
          fontSize: "13px",
          display: "flex",
          alignItems: "center",
          gap: "8px",
          background: message.type === "success" ? "rgba(16, 185, 129, 0.1)" : "rgba(239, 68, 68, 0.1)",
          color: message.type === "success" ? "var(--status-online)" : "var(--status-offline)",
          border: `1px solid ${message.type === "success" ? "rgba(16, 185, 129, 0.2)" : "rgba(239, 68, 68, 0.2)"}`,
        }}>
          <span className="material-symbols-outlined" style={{ fontSize: "18px" }}>
            {message.type === "success" ? "check_circle" : "error"}
          </span>
          {message.text}
        </div>
      )}

      {isLoadingTargets && targets.length === 0 ? (
        <div style={{ textAlign: "center", padding: "40px", color: "var(--text-secondary)" }}>
          Loading targets...
        </div>
      ) : targets.length === 0 ? (
        <div style={{
          textAlign: "center",
          padding: "48px 24px",
          background: "rgba(255, 255, 255, 0.01)",
          border: "1px dashed var(--border-color)",
          borderRadius: "12px",
          color: "var(--text-secondary)",
        }}>
          <span className="material-symbols-outlined" style={{ fontSize: "40px", opacity: 0.3, marginBottom: "8px" }}>
            notifications_off
          </span>
          <p style={{ fontSize: "14px", fontWeight: "500" }}>No Preset Alert Targets configured yet.</p>
          <p style={{ fontSize: "12px", opacity: 0.7, marginTop: "4px" }}>Configure targets here and link them easily when editing alert rules.</p>
        </div>
      ) : (
        <div style={{ display: "flex", flexDirection: "column", gap: "12px" }}>
          <h2 style={{ fontSize: '14px', fontWeight: '700', color: 'var(--text-secondary)', textTransform: 'uppercase', letterSpacing: '0.05em', marginBottom: '4px' }}>Configured Targets</h2>
          {targets.map(target => (
            <div
              key={target.target_id}
              className="card mobile-stack mobile-p-sm"
              style={{
                padding: '20px',
                display: 'flex',
                alignItems: 'center',
                gap: '20px',
              }}
            >
              <div className="mobile-gap-sm" style={{ display: 'flex', alignItems: 'center', gap: '20px', width: '100%' }}>
                <div style={{ flex: 1 }}>
                  <div style={{ display: 'flex', alignItems: 'center', gap: '8px', marginBottom: '4px', flexWrap: 'wrap' }}>
                    <span className="material-symbols-outlined" style={{ fontSize: '18px', color: 'var(--text-secondary)' }}>
                      {target.type === "discord" ? "forum" : "webhook"}
                    </span>
                    <h3 style={{ fontSize: '15px', fontWeight: '600' }}>
                      {target.name}
                    </h3>
                    <span className="badge" style={{ background: 'var(--bg-secondary)', color: 'var(--text-secondary)', fontSize: '10px' }}>
                      {target.type}
                    </span>
                  </div>
                  <p style={{ fontSize: '12px', color: 'var(--text-muted)', wordBreak: 'break-all' }}>
                    {formatDisplayURL(target.destination)}
                  </p>
                </div>
              </div>

              <div className="flex items-center gap-2 mobile-full" style={{ justifyContent: 'flex-end' }}>
                <button
                  onClick={() => navigate(`/targets/edit?id=${target.target_id}`)}
                  className="mobile-full"
                  style={{ padding: '8px', borderRadius: '8px', background: 'var(--bg-secondary)', color: 'var(--text-secondary)', transition: 'var(--transition-fast)', display: 'flex', alignItems: 'center', justifyContent: 'center', border: 'none', cursor: 'pointer' }}
                  onMouseOver={(e) => { e.currentTarget.style.color = 'var(--text-primary)'; e.currentTarget.style.background = 'rgba(255,255,255,0.1)'; }}
                  onMouseOut={(e) => { e.currentTarget.style.color = 'var(--text-secondary)'; e.currentTarget.style.background = 'var(--bg-secondary)'; }}
                  title="Edit Target"
                >
                  <span className="material-symbols-outlined" style={{ fontSize: '20px' }}>edit</span>
                </button>
                <button
                  onClick={() => handleDelete(target.target_id)}
                  disabled={loading}
                  className="mobile-full"
                  style={{ padding: '8px', borderRadius: '8px', background: 'var(--bg-secondary)', color: 'var(--status-offline)', transition: 'var(--transition-fast)', display: 'flex', alignItems: 'center', justifyContent: 'center', border: 'none', cursor: 'pointer' }}
                  onMouseOver={(e) => { if (!loading) e.currentTarget.style.background = 'rgba(239, 68, 68, 0.15)'; }}
                  onMouseOut={(e) => { if (!loading) e.currentTarget.style.background = 'var(--bg-secondary)'; }}
                  title="Delete Target"
                >
                  <span className="material-symbols-outlined" style={{ fontSize: '20px' }}>delete</span>
                </button>
              </div>
            </div>
          ))}
        </div>
      )}
    </div>
  );
}
