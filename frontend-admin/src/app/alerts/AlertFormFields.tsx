import React from "react";
import { Link } from "react-router-dom";
import { Agent, TriggerType, Operator, DestinationType, AlertTarget } from "../../types";
import { fetchAPI } from "../../lib/api";
import PayloadTemplateGuide from "../common/PayloadTemplateGuide";

interface AlertFormFieldsProps {
  nickname: string;
  setNickname: (v: string) => void;
  enabled: boolean;
  setEnabled: (v: boolean) => void;
  type: TriggerType;
  setType: (v: TriggerType) => void;
  operator: Operator;
  setOperator: (v: Operator) => void;
  threshold: number;
  setThreshold: (v: number) => void;
  duration: string;
  setDuration: (v: string) => void;
  destType: DestinationType;
  setDestType: (v: DestinationType) => void;
  targetId?: string;
  setTargetId?: (v: string) => void;
  destination: string;
  setDestination: (v: string) => void;
  payload: string;
  setPayload: (v: string) => void;
  selectedAgents: string[];
  setSelectedAgents: React.Dispatch<React.SetStateAction<string[]>>;
  agents: Agent[];
}

export default function AlertFormFields({
  nickname,
  setNickname,
  enabled,
  setEnabled,
  type,
  setType,
  operator,
  setOperator,
  threshold,
  setThreshold,
  duration,
  setDuration,
  destType,
  setDestType,
  targetId = "",
  setTargetId = () => {},
  destination,
  setDestination,
  payload,
  setPayload,
  selectedAgents,
  setSelectedAgents,
  agents,
}: AlertFormFieldsProps) {

  const [targets, setTargets] = React.useState<AlertTarget[]>([]);
  React.useEffect(() => {
    fetchAPI<AlertTarget[]>("/api/alerts/targets")
      .then(res => setTargets(res || []))
      .catch((err: any) => {
        console.error("Failed to load targets", err);
        setTargets([]);
      });
  }, []);

  const toggleAgent = (id: string) => {
    setSelectedAgents(prev =>
      prev.includes(id) ? prev.filter(a => a !== id) : [...prev, id]
    );
  };

  const selectedTarget = (targets || []).find(t => t.target_id === targetId);

  return (
    <>
      {/* Alert Nickname */}
      <div className="card" style={{ padding: '24px' }}>
        <h2 style={{ fontSize: '12px', fontWeight: '700', color: 'var(--text-secondary)', textTransform: 'uppercase', letterSpacing: '0.05em', marginBottom: '16px', display: 'flex', alignItems: 'center', gap: '8px' }}>
          <span className="material-symbols-outlined" style={{ fontSize: '16px' }}>label</span>
          Alert Name / Nickname
        </h2>
        <div style={{ display: 'flex', flexDirection: 'column', gap: '8px' }}>
          <input
            required
            type="text"
            value={nickname}
            onChange={(e) => setNickname(e.target.value)}
            className="input-field"
            placeholder="e.g. Production Database Offline, CPU Spike Warning"
            style={{ width: '100%' }}
          />
          <p style={{ fontSize: '13px', color: 'var(--text-secondary)', opacity: 0.8, margin: '4px 0 0 0' }}>
            Give this alert a custom name to quickly identify it in history lists and notification headers.
          </p>
        </div>
      </div>

      {/* Tactical Switch Toggle for Enabled State */}
      <div className="card" style={{ padding: '24px', display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
        <div>
          <h3 style={{ fontSize: '15px', fontWeight: '600', color: 'var(--text-primary)', margin: '0 0 4px 0' }}>Alert Rule Enabled</h3>
          <p style={{ fontSize: '13px', color: 'var(--text-secondary)', opacity: 0.8, margin: 0 }}>Active rules continually monitor reporting nodes and dispatch incidents.</p>
        </div>
        <div
          onClick={() => setEnabled(!enabled)}
          style={{
            cursor: 'pointer',
            width: '40px',
            height: '24px',
            borderRadius: '12px',
            background: enabled ? 'var(--status-online)' : 'var(--bg-secondary)',
            position: 'relative',
            transition: 'var(--transition-fast)',
            flexShrink: 0,
            border: '1px solid var(--border-color)'
          }}
        >
          <div style={{
            position: 'absolute',
            top: '2px',
            left: enabled ? '18px' : '2px',
            width: '18px',
            height: '18px',
            borderRadius: '50%',
            background: '#fff',
            transition: 'var(--transition-fast)',
            boxShadow: '0 2px 4px rgba(0,0,0,0.2)'
          }} />
        </div>
      </div>

      {/* Trigger Configuration */}
      <div className="card" style={{ padding: '24px' }}>
        <h2 style={{ fontSize: '12px', fontWeight: '700', color: 'var(--text-secondary)', textTransform: 'uppercase', letterSpacing: '0.05em', marginBottom: '16px', display: 'flex', alignItems: 'center', gap: '8px' }}>
          <span className="material-symbols-outlined" style={{ fontSize: '16px' }}>bolt</span>
          1. Trigger Condition
        </h2>

        <div className="mobile-grid-1" style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: '20px' }}>
          <div style={{ display: 'flex', flexDirection: 'column', gap: '8px' }}>
            <label style={{ display: 'block', fontSize: '12px', fontWeight: '600', color: 'var(--text-secondary)', textTransform: 'uppercase', letterSpacing: '0.05em' }}>Metric Type</label>
            <select
              value={type}
              onChange={(e) => setType(e.target.value as TriggerType)}
              className="input-field"
              style={{ width: '100%', cursor: 'pointer' }}
            >
              <option value="agent_down">Node Down (Offline)</option>
              <option value="cpu_usage">CPU Usage (%)</option>
              <option value="cpu_iowait">CPU IO Wait (%)</option>
              <option value="cpu_steal">CPU Steal (%)</option>
              <option value="ram_usage">RAM Usage (%)</option>
              <option value="swap_usage">Swap Usage (%)</option>
              <option value="disk_usage">Disk Usage (%)</option>
              <option value="net_rx">Network In (KB/s)</option>
              <option value="net_tx">Network Out (KB/s)</option>
              <option value="disk_read">Disk Read (KB/s)</option>
              <option value="disk_write">Disk Write (KB/s)</option>
            </select>
          </div>

          {type !== 'agent_down' && (
            <div style={{ display: 'flex', flexDirection: 'column', gap: '8px' }}>
              <label style={{ display: 'block', fontSize: '12px', fontWeight: '600', color: 'var(--text-secondary)', textTransform: 'uppercase', letterSpacing: '0.05em' }}>
                Operator & Threshold {["net_rx", "net_tx", "disk_read", "disk_write"].includes(type) ? "(KB/s)" : "(%)"}
              </label>
              <div style={{ display: 'flex', gap: '10px' }}>
                <select
                  value={operator}
                  onChange={(e) => setOperator(e.target.value as Operator)}
                  className="input-field"
                  style={{ width: '80px', cursor: 'pointer' }}
                >
                  <option value=">">&gt;</option>
                  <option value="<">&lt;</option>
                  <option value="==">==</option>
                </select>
                <input
                  type="number"
                  value={threshold}
                  onChange={(e) => setThreshold(Number(e.target.value))}
                  className="input-field"
                  style={{ flex: 1 }}
                  min="0"
                  max={["net_rx", "net_tx", "disk_read", "disk_write"].includes(type) ? undefined : 100}
                />
              </div>
            </div>
          )}

          <div style={{ display: 'flex', flexDirection: 'column', gap: '8px' }}>
            <label style={{ display: 'block', fontSize: '12px', fontWeight: '600', color: 'var(--text-secondary)', textTransform: 'uppercase', letterSpacing: '0.05em' }}>Breach Duration</label>
            <input
              type="text"
              value={duration}
              onChange={(e) => setDuration(e.target.value)}
              className="input-field"
              placeholder="e.g. 5m"
            />
            <p style={{ fontSize: '13px', color: 'var(--text-secondary)', opacity: 0.8, marginTop: '4px', marginBottom: 0 }}>Threshold breach must persist continuously for this period before firing.</p>
          </div>
        </div>
      </div>

      {/* Action Configuration */}
      <div className="card" style={{ padding: '24px' }}>
        <h2 style={{ fontSize: '12px', fontWeight: '700', color: 'var(--text-secondary)', textTransform: 'uppercase', letterSpacing: '0.05em', marginBottom: '16px', display: 'flex', alignItems: 'center', gap: '8px' }}>
          <span className="material-symbols-outlined" style={{ fontSize: '16px' }}>notifications</span>
          2. Notification Action
        </h2>

        <div className="mobile-grid-1" style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: '20px', marginBottom: '20px' }}>
          <div style={{ display: 'flex', flexDirection: 'column', gap: '8px' }}>
            <label style={{ display: 'block', fontSize: '12px', fontWeight: '600', color: 'var(--text-secondary)', textTransform: 'uppercase', letterSpacing: '0.05em' }}>Channel Type</label>
            <select
              value={destType}
              onChange={(e) => {
                const val = e.target.value as DestinationType;
                setDestType(val);
                if (val === "preset" && (targets || []).length > 0 && !targetId) {
                  setTargetId(targets[0].target_id);
                }
              }}
              className="input-field"
              style={{ width: '100%', cursor: 'pointer' }}
            >
              <option value="preset">Preset Target Channel</option>
              <option value="discord">Discord Webhook</option>
              <option value="webhook">Custom Webhook URL</option>
            </select>
          </div>

          {destType === "preset" ? (
            <div style={{ display: 'flex', flexDirection: 'column', gap: '8px' }}>
              <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center" }}>
                <label style={{ display: 'block', fontSize: '12px', fontWeight: '600', color: 'var(--text-secondary)', textTransform: 'uppercase', letterSpacing: '0.05em' }}>
                  Select Preset Target
                </label>
                <Link to="/alerts?tab=targets" style={{ fontSize: "11px", color: "var(--accent-primary)", fontWeight: "600", textDecoration: "none" }}>
                  Manage Targets
                </Link>
              </div>
              {(targets || []).length === 0 ? (
                <div style={{
                  padding: "10px 14px",
                  borderRadius: "8px",
                  background: "rgba(255,255,255,0.02)",
                  border: "1px solid var(--border-color)",
                  color: "var(--text-secondary)",
                  fontSize: "13px"
                }}>
                  No targets configured. <Link to="/alerts?tab=targets" style={{ color: "var(--accent-primary)" }}>Configure here first.</Link>
                </div>
              ) : (
                <select
                  value={targetId}
                  onChange={(e) => setTargetId(e.target.value)}
                  className="input-field"
                  style={{ width: '100%', cursor: 'pointer' }}
                >
                  <option value="">-- Select a Preset Target --</option>
                  {(targets || []).map(t => (
                    <option key={t.target_id} value={t.target_id}>{t.name} ({t.type})</option>
                  ))}
                </select>
              )}
            </div>
          ) : (
            <div style={{ display: 'flex', flexDirection: 'column', gap: '8px' }}>
              <label style={{ display: 'block', fontSize: '12px', fontWeight: '600', color: 'var(--text-secondary)', textTransform: 'uppercase', letterSpacing: '0.05em' }}>Webhook Endpoint URL</label>
              <input
                type="text"
                value={destination}
                onChange={(e) => setDestination(e.target.value)}
                className="input-field"
                placeholder="https://..."
              />
            </div>
          )}
        </div>

        {destType === "preset" && selectedTarget && (
          <div style={{
            marginBottom: "20px",
            padding: "12px 16px",
            background: "rgba(255,255,255,0.01)",
            border: "1px solid var(--border-color)",
            borderRadius: "8px"
          }}>
            <div style={{ fontSize: "12px", color: "var(--text-secondary)", fontWeight: "600" }}>Active target details:</div>
            <div style={{ display: "flex", gap: "12px", marginTop: "8px" }}>
              <div style={{ fontSize: "11px", color: "var(--text-secondary)", opacity: 0.8 }}>URL:</div>
              <div style={{ fontSize: "11px", color: "var(--text-primary)", wordBreak: "break-all" }}>{selectedTarget.destination}</div>
            </div>
            {selectedTarget.payload && (
              <div style={{ display: "flex", flexDirection: "column", gap: "4px", marginTop: "8px" }}>
                <div style={{ fontSize: "11px", color: "var(--text-secondary)", opacity: 0.8 }}>Default template:</div>
                <pre style={{
                  fontSize: "11px",
                  background: "var(--bg-secondary)",
                  padding: "8px",
                  borderRadius: "6px",
                  overflowX: "auto",
                  margin: 0
                }}>{selectedTarget.payload}</pre>
              </div>
            )}
          </div>
        )}

        <div style={{ display: 'flex', flexDirection: 'column', gap: '8px' }}>
          <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
            <label style={{ display: 'block', fontSize: '12px', fontWeight: '600', color: 'var(--text-secondary)', textTransform: 'uppercase', letterSpacing: '0.05em' }}>
              {destType === "preset" ? "Payload Override (Optional JSON)" : "Custom Payload Template (Optional JSON)"}
            </label>
            {destType === "preset" && (
              <span style={{ fontSize: "11px" }} className="text-secondary">If left empty, the preset target's default template is used.</span>
            )}
          </div>
          <textarea
            value={payload}
            onChange={(e) => setPayload(e.target.value)}
            className="input-field"
            style={{ minHeight: '80px', fontFamily: 'var(--font-mono)', fontSize: '13px' }}
            placeholder={destType === "preset" ? 'Leave empty to use target default, or customize here...' : '{\n  "text": "Agent {{NICKNAME}} is {{STATUS}}"\n}'}
          />
          
          <PayloadTemplateGuide />
        </div>
      </div>

      {/* Target Nodes Selector Card */}
      <div className="card" style={{ padding: '24px' }}>
        <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: '16px' }}>
          <h2 style={{ fontSize: '12px', fontWeight: '700', color: 'var(--text-secondary)', textTransform: 'uppercase', letterSpacing: '0.05em', margin: 0, display: 'flex', alignItems: 'center', gap: '8px' }}>
            <span className="material-symbols-outlined" style={{ fontSize: '16px' }}>dns</span>
            3. Target Nodes
          </h2>
          <div style={{ display: 'flex', gap: '8px', alignItems: 'center' }}>
            <button type="button" onClick={() => setSelectedAgents(agents.map(a => a.agent_id))} style={{ fontSize: '12px', background: 'none', border: 'none', color: 'var(--accent-primary)', fontWeight: '600', cursor: 'pointer' }}>Select All</button>
            <span style={{ color: 'var(--text-secondary)', opacity: 0.5, fontSize: '12px' }}>|</span>
            <button type="button" onClick={() => setSelectedAgents([])} style={{ fontSize: '12px', background: 'none', border: 'none', color: 'var(--accent-primary)', fontWeight: '600', cursor: 'pointer' }}>Clear</button>
          </div>
        </div>

        <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fill, minmax(200px, 1fr))', gap: '12px', maxHeight: '300px', overflowY: 'auto' }}>
          {agents.map(a => {
            const isSelected = selectedAgents.includes(a.agent_id);
            return (
              <button
                type="button"
                key={a.agent_id}
                onClick={() => toggleAgent(a.agent_id)}
                style={{
                  display: 'flex',
                  alignItems: 'center',
                  gap: '12px',
                  padding: '12px 16px',
                  border: `1px solid ${isSelected ? 'var(--accent-primary)' : 'var(--border-color)'}`,
                  borderRadius: '8px',
                  background: isSelected ? 'rgba(99, 102, 241, 0.1)' : 'var(--bg-primary)',
                  color: isSelected ? 'var(--text-primary)' : 'var(--text-secondary)',
                  textAlign: 'left',
                  cursor: 'pointer',
                  transition: 'var(--transition-fast)'
                }}
              >
                <span className="material-symbols-outlined" style={{ fontSize: '20px', color: isSelected ? 'var(--accent-primary)' : 'var(--text-muted)' }}>
                  {isSelected ? "check_box" : "check_box_outline_blank"}
                </span>
                <div style={{ display: 'flex', flexDirection: 'column' }}>
                  <span style={{ fontSize: '13px', fontWeight: isSelected ? '600' : '500' }}>
                    {a.nickname || a.agent_id.substring(0, 8)}
                  </span>
                  <span style={{ fontSize: '11px', color: 'var(--text-secondary)', opacity: 0.7, fontFamily: 'var(--font-mono)', marginTop: '2px' }}>
                    {a.agent_id.substring(0, 12)}...
                  </span>
                </div>
              </button>
            );
          })}
        </div>
      </div>
    </>
  );
}
