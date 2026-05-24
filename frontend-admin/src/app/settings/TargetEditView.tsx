import { useState, useEffect } from "react";
import { useNavigate, useSearchParams } from "react-router-dom";
import { fetchAPI } from "../../lib/api";
import { AlertTarget } from "../../types";
import DeleteConfirmModal from "../common/DeleteConfirmModal";
import TargetFormFields from "./TargetFormFields";

export default function TargetEditView() {
  const navigate = useNavigate();
  const [searchParams] = useSearchParams();
  const targetId = searchParams.get("id");

  const [isLoading, setIsLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [name, setName] = useState("");
  const [type, setType] = useState<"webhook" | "discord">("webhook");
  const [destination, setDestination] = useState("");
  const [payload, setPayload] = useState("");
  const [message, setMessage] = useState<{ type: "success" | "error"; text: string } | null>(null);
  const [testStatus, setTestStatus] = useState<{ type: 'success' | 'error', message: string } | null>(null);
  const [showDeleteModal, setShowDeleteModal] = useState(false);

  const handleTest = async () => {
    if (!destination) {
      setTestStatus({ type: 'error', message: 'Please provide a destination endpoint URL to test.' });
      return;
    }

    setTestStatus(null);
    setSaving(true);
    try {
      await fetchAPI<{ message: string }>("/api/alerts/targets/test", {
        method: "POST",
        body: JSON.stringify({ type, destination, payload }),
      });
      setTestStatus({ type: 'success', message: "Test notification sent successfully!" });
    } catch (err: any) {
      setTestStatus({ type: 'error', message: err.message || "Failed to send test notification" });
    } finally {
      setSaving(false);
    }
  };

  useEffect(() => {
    if (!targetId) {
      navigate("/alerts?tab=targets");
      return;
    }

    fetchAPI<AlertTarget>(`/api/alerts/targets/${targetId}`)
      .then(target => {
        setName(target.name);
        setType(target.type);
        setDestination(target.destination);
        setPayload(target.payload || "");
        setIsLoading(false);
      })
      .catch(err => {
        console.error(err);
        alert("Failed to load target details");
        navigate("/alerts?tab=targets");
      });
  }, [targetId, navigate]);

  const handleSave = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!name || !destination || !type) {
      setMessage({ type: "error", text: "Please fill out all required fields" });
      return;
    }

    setSaving(true);
    setMessage(null);

    try {
      await fetchAPI(`/api/alerts/targets/${targetId}`, {
        method: "PUT",
        body: JSON.stringify({ name, type, destination, payload }),
      });
      // Navigate back to targets list
      navigate("/alerts?tab=targets");
    } catch (err: any) {
      setMessage({ type: "error", text: err.message || "Failed to update alert target" });
    } finally {
      setSaving(false);
    }
  };

  const handleDeleteExecute = async () => {
    setShowDeleteModal(false);
    setSaving(true);
    try {
      await fetchAPI(`/api/alerts/targets/${targetId}`, { method: "DELETE" });
      navigate("/alerts?tab=targets");
    } catch (err: any) {
      alert(err.message || "Failed to delete target");
    } finally {
      setSaving(false);
    }
  };

  if (isLoading) {
    return (
      <div style={{ display: 'flex', height: '50vh', alignItems: 'center', justifyContent: 'center', background: 'var(--bg-primary)' }}>
        <span className="material-symbols-outlined" style={{ fontSize: '32px', animation: 'spin 1s linear infinite', color: 'var(--accent-primary)' }}>sync</span>
      </div>
    );
  }

  return (
    <div className="mobile-p-sm" style={{ padding: '40px 24px' }}>
      <div className="animate-fade-in mobile-gap-sm" style={{ maxWidth: '800px', margin: '0 auto' }}>
        <div className="mobile-stack" style={{ display: 'flex', alignItems: 'center', gap: '16px', marginBottom: '32px' }}>
          <button 
            type="button" 
            onClick={() => navigate("/alerts?tab=targets")} 
            className="btn-secondary" 
            style={{ width: '36px', height: '36px', borderRadius: '50%', display: 'flex', alignItems: 'center', justifyContent: 'center', flexShrink: 0 }}
          >
            <span className="material-symbols-outlined" style={{ fontSize: '20px' }}>arrow_back</span>
          </button>
          <div>
            <h1 className="font-display mobile-text-lg" style={{ fontSize: '24px', fontWeight: '700', marginBottom: '4px' }}>Edit Alert Target</h1>
            <p style={{ fontSize: '13px', color: 'var(--text-secondary)' }}>Centrally configure reusable notification destinations.</p>
          </div>
        </div>

        <form onSubmit={handleSave} style={{ display: 'flex', flexDirection: 'column', gap: '24px' }}>
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

          {testStatus && (
            <div style={{
              padding: '16px',
              borderRadius: '8px',
              display: 'flex',
              alignItems: 'center',
              gap: '12px',
              background: testStatus.type === 'success' ? 'rgba(16, 185, 129, 0.1)' : 'rgba(239, 68, 68, 0.1)',
              border: `1px solid ${testStatus.type === 'success' ? '#10b981' : '#ef4444'}`,
              color: testStatus.type === 'success' ? '#10b981' : '#ef4444',
              fontSize: '14px',
              fontWeight: '500'
            }}>
              <span className="material-symbols-outlined">
                {testStatus.type === 'success' ? 'check_circle' : 'error'}
              </span>
              {testStatus.message}
            </div>
          )}

          <TargetFormFields
            name={name}
            setName={setName}
            type={type}
            setType={setType}
            destination={destination}
            setDestination={setDestination}
            payload={payload}
            setPayload={setPayload}
          />

          <div style={{ display: 'flex', justifyContent: 'space-between', gap: '16px', marginTop: '16px' }} className="mobile-stack">
            <button
              type="button"
              onClick={() => setShowDeleteModal(true)}
              className="btn-secondary"
              style={{
                borderColor: "rgba(239, 68, 68, 0.4)",
                color: "var(--status-offline)",
                background: "rgba(239, 68, 68, 0.02)"
              }}
              disabled={saving}
            >
              Delete Target
            </button>

            <div style={{ display: 'flex', gap: '16px' }} className="mobile-stack">
              <button 
                type="button" 
                onClick={handleTest} 
                disabled={saving} 
                className="btn-secondary mobile-full"
                style={{ padding: '12px 24px', display: 'flex', alignItems: 'center', gap: '8px', justifyContent: 'center' }}
              >
                <span className="material-symbols-outlined" style={{ fontSize: '18px' }}>send</span>
                Test Connection
              </button>
              <button 
                type="button" 
                onClick={() => navigate("/alerts?tab=targets")} 
                disabled={saving} 
                className="btn-secondary mobile-full"
                style={{ padding: '12px 24px' }}
              >
                Cancel
              </button>
              <button 
                type="submit" 
                disabled={saving} 
                className="btn-primary mobile-full" 
                style={{ padding: '12px 32px' }}
              >
                {saving ? "Saving..." : "Save Changes"}
              </button>
            </div>
          </div>
        </form>
      </div>

      <DeleteConfirmModal
        isOpen={showDeleteModal}
        title="Delete Alert Target"
        message={`Are you sure you want to delete "${name}"? Any alerts referencing this preset target will fall back or fail.`}
        confirmText="Delete Target"
        onClose={() => setShowDeleteModal(false)}
        onConfirm={handleDeleteExecute}
      />
    </div>
  );
}
