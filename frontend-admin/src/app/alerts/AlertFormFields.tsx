import React from "react";
import { Agent, TriggerType, Operator, DestinationType } from "../../types";

interface AlertFormFieldsProps {
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
  destination: string;
  setDestination: (v: string) => void;
  payload: string;
  setPayload: (v: string) => void;
  selectedAgents: string[];
  setSelectedAgents: React.Dispatch<React.SetStateAction<string[]>>;
  agents: Agent[];
}

export default function AlertFormFields({
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
  destination,
  setDestination,
  payload,
  setPayload,
  selectedAgents,
  setSelectedAgents,
  agents,
}: AlertFormFieldsProps) {

  const toggleAgent = (id: string) => {
    setSelectedAgents(prev =>
      prev.includes(id) ? prev.filter(a => a !== id) : [...prev, id]
    );
  };

  return (
    <>
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
              onChange={(e) => setDestType(e.target.value as DestinationType)}
              className="input-field"
              style={{ width: '100%', cursor: 'pointer' }}
            >
              <option value="discord">Discord Webhook</option>
              <option value="webhook">Custom Webhook URL</option>
            </select>
          </div>

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
        </div>

        <div style={{ display: 'flex', flexDirection: 'column', gap: '8px' }}>
          <label style={{ display: 'block', fontSize: '12px', fontWeight: '600', color: 'var(--text-secondary)', textTransform: 'uppercase', letterSpacing: '0.05em' }}>Custom Payload Template (Optional JSON)</label>
          <textarea
            value={payload}
            onChange={(e) => setPayload(e.target.value)}
            className="input-field"
            style={{ minHeight: '80px', fontFamily: 'var(--font-mono)', fontSize: '13px' }}
            placeholder={'{\n  "text": "Agent {{NICKNAME}} is {{STATUS}}"\n}'}
          />
          
          <div style={{
            marginTop: '12px',
            padding: '16px',
            background: 'var(--bg-secondary)',
            borderRadius: '8px',
            borderLeft: '4px solid var(--accent-primary)',
            fontSize: '13px',
            lineHeight: '1.6'
          }}>
            <p style={{ fontWeight: 'bold', marginBottom: '8px', display: 'flex', alignItems: 'center', gap: '6px', color: 'var(--text-primary)' }}>
              <span className="material-symbols-outlined" style={{ fontSize: '16px', color: 'var(--accent-primary)' }}>info</span>
              Custom Payload Template Guide
            </p>
            <p style={{ color: 'var(--text-secondary)', marginBottom: '12px', fontSize: '13px' }}>
              Customize the JSON body of your webhook. If left empty, a standard notification payload is dispatched. Use the following placeholders to inject dynamic telemetry state:
            </p>
            
            <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: '8px 16px', fontFamily: 'var(--font-mono)', fontSize: '12px' }}>
              <div>
                <span style={{ color: 'var(--accent-primary)', fontWeight: 'bold' }}>{"{{AGENT_ID}}"}</span>
                <span style={{ color: 'var(--text-secondary)', opacity: 0.8, marginLeft: '8px' }}>— Node ID</span>
              </div>
              <div>
                <span style={{ color: 'var(--accent-primary)', fontWeight: 'bold' }}>{"{{NICKNAME}}"}</span>
                <span style={{ color: 'var(--text-secondary)', opacity: 0.8, marginLeft: '8px' }}>— Readable Nickname</span>
              </div>
              <div>
                <span style={{ color: 'var(--accent-primary)', fontWeight: 'bold' }}>{"{{STATUS}}"}</span>
                <span style={{ color: 'var(--text-secondary)', opacity: 0.8, marginLeft: '8px' }}>— Alert state (FIRING/RESOLVED)</span>
              </div>
              <div>
                <span style={{ color: 'var(--accent-primary)', fontWeight: 'bold' }}>{"{{VALUE}}"}</span>
                <span style={{ color: 'var(--text-secondary)', opacity: 0.8, marginLeft: '8px' }}>— Current Value (e.g. 85.50%)</span>
              </div>
              <div>
                <span style={{ color: 'var(--accent-primary)', fontWeight: 'bold' }}>{"{{TRIGGER_LABEL}}"}</span>
                <span style={{ color: 'var(--text-secondary)', opacity: 0.8, marginLeft: '8px' }}>— Metric Label (e.g. CPU IO Wait)</span>
              </div>
              <div>
                <span style={{ color: 'var(--accent-primary)', fontWeight: 'bold' }}>{"{{THRESHOLD}}"}</span>
                <span style={{ color: 'var(--text-secondary)', opacity: 0.8, marginLeft: '8px' }}>— Condition Boundary</span>
              </div>
              <div>
                <span style={{ color: 'var(--accent-primary)', fontWeight: 'bold' }}>{"{{TIME_TRIGGER}}"}</span>
                <span style={{ color: 'var(--text-secondary)', opacity: 0.8, marginLeft: '8px' }}>— Breach/Offline Timestamp</span>
              </div>
              <div>
                <span style={{ color: 'var(--accent-primary)', fontWeight: 'bold' }}>{"{{TIME_RESOLVED}}"}</span>
                <span style={{ color: 'var(--text-secondary)', opacity: 0.8, marginLeft: '8px' }}>— Recovery/Online Timestamp</span>
              </div>
            </div>
            
            <div style={{
              marginTop: '12px',
              paddingTop: '8px',
              borderTop: '1px solid var(--border-color)',
              fontSize: '12px'
            }}>
              <p style={{ fontWeight: 'bold', color: 'var(--text-primary)', marginBottom: '4px' }}>Downtime & Discord Integration Note:</p>
              <p style={{ color: 'var(--text-secondary)', opacity: 0.8, fontSize: '12px', margin: 0, lineHeight: '1.5' }}>
                For Discord Webhooks, <code style={{ color: 'var(--accent-primary)' }}>{"{{TIME_TRIGGER}}"}</code> and <code style={{ color: 'var(--accent-primary)' }}>{"{{TIME_RESOLVED}}"}</code> render client-side relative timestamps dynamically. You can also use <code style={{ color: 'var(--accent-primary)' }}>{"{{DOWN_DURATION}}"}</code> (e.g., <code style={{ color: 'var(--text-primary)' }}>5m 12s</code>) on resolved notifications for offline events.
              </p>
            </div>
          </div>
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
