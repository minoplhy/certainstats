import { useState, useEffect } from "react";
import { useNavigate, useSearchParams } from "react-router-dom";
import { fetchAPI } from "../../lib/api";
import { Agent, TriggerType, Operator, DestinationType, Alert } from "../../types";
import DeleteConfirmModal from "../common/DeleteConfirmModal";
import AlertFormFields from "./AlertFormFields";

export default function AlertEditView() {
  const navigate = useNavigate();
  const [searchParams] = useSearchParams();
  const alertId = searchParams.get("id");

  const [agents, setAgents] = useState<Agent[]>([]);
  const [isLoading, setIsLoading] = useState(true);
  const [saving, setSaving] = useState(false);

  // Form State
  const [nickname, setNickname] = useState("");
  const [enabled, setEnabled] = useState(true);
  const [type, setType] = useState<TriggerType>("cpu_usage");
  const [operator, setOperator] = useState<Operator>(">");
  const [threshold, setThreshold] = useState(80);
  const [duration, setDuration] = useState("5m");
  const [destType, setDestType] = useState<DestinationType>("webhook");
  const [destination, setDestination] = useState("");
  const [payload, setPayload] = useState("");
  const [selectedAgents, setSelectedAgents] = useState<string[]>([]);
  const [showDeleteModal, setShowDeleteModal] = useState(false);

  const handleDeleteExecute = async () => {
    setShowDeleteModal(false);
    setSaving(true);
    try {
      await fetchAPI(`/api/alerts/${alertId}`, { method: "DELETE" });
      navigate("/alerts");
    } catch (err) {
      alert(err instanceof Error ? err.message : "Failed to delete alert");
    } finally {
      setSaving(false);
    }
  };

  useEffect(() => {
    if (!alertId) {
      navigate("/alerts");
      return;
    }

    Promise.all([
      fetchAPI<Agent[]>("/api/agents"),
      fetchAPI<Alert>(`/api/alerts/${alertId}`)
    ]).then(([agentsData, alertData]) => {
      setAgents(agentsData);

      // Fill form
      setNickname(alertData.nickname || "");
      setEnabled(alertData.enabled);
      setType(alertData.trigger.type);
      setOperator(alertData.trigger.operator);
      setThreshold(alertData.trigger.threshold);
      setDuration(alertData.trigger.duration);
      setDestType(alertData.action.type);
      setDestination(alertData.action.destination);
      setPayload(alertData.action.payload || "");
      setSelectedAgents(alertData.agents.map(a => a.agent_id));

      setIsLoading(false);
    }).catch(err => {
      console.error(err);
      alert("Failed to load alert details");
      navigate("/alerts");
    });
  }, [alertId, navigate]);

  const handleSave = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!nickname.trim()) {
      alert("Please provide an alert name / nickname.");
      return;
    }
    if (selectedAgents.length === 0) {
      alert("Please select at least one node.");
      return;
    }
    if (!destination) {
      alert("Please provide a notification destination.");
      return;
    }

    setSaving(true);
    try {
      await fetchAPI(`/api/alerts/${alertId}`, {
        method: "PUT",
        body: JSON.stringify({
          nickname,
          enabled,
          trigger: {
            type,
            operator: type === 'agent_down' ? '' : operator,
            threshold: type === 'agent_down' ? 0 : threshold,
            duration
          },
          action: {
            type: destType,
            destination,
            payload: payload
          },
          agents: selectedAgents
        })
      });
      navigate("/alerts");
    } catch (err) {
      alert(err instanceof Error ? err.message : "Failed to save alert");
    } finally {
      setSaving(false);
    }
  };

  const [testStatus, setTestStatus] = useState<{ type: 'success' | 'error', message: string } | null>(null);

  const handleTest = async () => {
    if (!destination) {
      setTestStatus({ type: 'error', message: 'Please provide a notification destination to test.' });
      return;
    }

    setTestStatus(null);
    setSaving(true);
    try {
      const res = await fetchAPI<{ message: string }>("/api/alerts/test", {
        method: "POST",
        body: JSON.stringify({
          action: {
            type: destType,
            destination,
            payload: payload
          }
        })
      });
      setTestStatus({ type: 'success', message: res.message || "Test notification sent successfully!" });
    } catch (err) {
      setTestStatus({ type: 'error', message: err instanceof Error ? err.message : "Failed to send test notification" });
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

          <div className="mobile-stack" style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', marginBottom: '32px', gap: '16px' }}>
            <div style={{ display: 'flex', alignItems: 'center', gap: '16px' }}>
              <button 
                type="button" 
                onClick={() => navigate("/alerts")} 
                className="btn-secondary" 
                style={{ padding: '8px', borderRadius: '50%', display: 'flex' }}
              >
                <span className="material-symbols-outlined" style={{ fontSize: '20px' }}>arrow_back</span>
              </button>
              <div>
                <h1 className="font-display mobile-text-lg" style={{ fontSize: '24px', fontWeight: '700', marginBottom: '4px' }}>Edit Alert</h1>
                <p style={{ fontSize: '13px', color: 'var(--text-secondary)' }}>Update your notification thresholds.</p>
              </div>
            </div>
            <button 
              type="button" 
              onClick={() => setShowDeleteModal(true)} 
              className="btn-secondary mobile-full" 
              style={{ color: 'var(--status-offline)', borderColor: 'rgba(239, 68, 68, 0.3)', display: 'flex', alignItems: 'center', gap: '8px', justifyContent: 'center' }} 
              onMouseOver={(e) => e.currentTarget.style.background = 'rgba(239, 68, 68, 0.1)'} 
              onMouseOut={(e) => e.currentTarget.style.background = 'rgba(255, 255, 255, 0.05)'}
            >
              <span className="material-symbols-outlined" style={{ fontSize: '18px' }}>delete</span> Delete
            </button>
          </div>

          <form onSubmit={handleSave} style={{ display: 'flex', flexDirection: 'column', gap: '24px' }}>

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

            <AlertFormFields
              nickname={nickname}
              setNickname={setNickname}
              enabled={enabled}
              setEnabled={setEnabled}
              type={type}
              setType={setType}
              operator={operator}
              setOperator={setOperator}
              threshold={threshold}
              setThreshold={setThreshold}
              duration={duration}
              setDuration={setDuration}
              destType={destType}
              setDestType={setDestType}
              destination={destination}
              setDestination={setDestination}
              payload={payload}
              setPayload={setPayload}
              selectedAgents={selectedAgents}
              setSelectedAgents={setSelectedAgents}
              agents={agents}
            />

            <div style={{ display: 'flex', justifyContent: 'flex-end', gap: '16px', marginTop: '16px' }} className="mobile-stack">
              <button 
                type="button" 
                onClick={handleTest} 
                disabled={saving} 
                className="btn-secondary mobile-full" 
                style={{ padding: '12px 24px', display: 'flex', alignItems: 'center', gap: '8px', justifyContent: 'center' }}
              >
                <span className="material-symbols-outlined" style={{ fontSize: '18px' }}>send</span>
                Test Notification
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
          </form>
        </div>

        <DeleteConfirmModal
          isOpen={showDeleteModal}
          title="Delete Alert?"
          message="Are you sure you want to delete this alert? This action cannot be undone and will permanently remove this notification rule."
          confirmText="Delete Alert"
          onClose={() => setShowDeleteModal(false)}
          onConfirm={handleDeleteExecute}
        />
      </div>
  );
}