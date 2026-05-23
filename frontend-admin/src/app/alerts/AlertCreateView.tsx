import { useState, useEffect } from "react";
import { useNavigate } from "react-router-dom";
import { fetchAPI } from "../../lib/api";
import { Agent, TriggerType, Operator, DestinationType } from "../../types";
import AlertFormFields from "./AlertFormFields";

export default function AlertCreateView() {
  const navigate = useNavigate();
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

  useEffect(() => {
    fetchAPI<Agent[]>("/api/agents")
      .then(setAgents)
      .catch(console.error)
      .finally(() => setIsLoading(false));
  }, []);

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
      await fetchAPI("/api/alerts", {
        method: "POST",
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

      navigate("/alerts"); // Redirect back to dashboard (where AlertsPanel will be)
    } catch (err) {
      alert(err instanceof Error ? err.message : "Failed to create alert");
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

          <div className="mobile-stack" style={{ display: 'flex', alignItems: 'center', gap: '16px', marginBottom: '32px' }}>
            <button 
              type="button" 
              onClick={() => navigate("/alerts")} 
              className="btn-secondary" 
              style={{ padding: '8px', borderRadius: '50%', display: 'flex' }}
            >
              <span className="material-symbols-outlined" style={{ fontSize: '20px' }}>arrow_back</span>
            </button>
            <div>
              <h1 className="font-display mobile-text-lg" style={{ fontSize: '24px', fontWeight: '700', marginBottom: '4px' }}>New Alert</h1>
              <p style={{ fontSize: '13px', color: 'var(--text-secondary)' }}>Define how and when you want to be notified.</p>
            </div>
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
                {saving ? "Saving..." : "Create Alert"}
              </button>
            </div>
          </form>
        </div>
      </div>
  );
}