import React from "react";
import PayloadTemplateGuide from "../common/PayloadTemplateGuide";

interface TargetFormFieldsProps {
  name: string;
  setName: (v: string) => void;
  type: "webhook" | "discord";
  setType: (v: "webhook" | "discord") => void;
  destination: string;
  setDestination: (v: string) => void;
  payload: string;
  setPayload: (v: string) => void;
}

export default function TargetFormFields({
  name,
  setName,
  type,
  setType,
  destination,
  setDestination,
  payload,
  setPayload,
}: TargetFormFieldsProps) {
  return (
    <div className="card w-full" style={{ display: "flex", flexDirection: "column", gap: "20px" }}>
      <div style={{ display: "grid", gridTemplateColumns: "2fr 1fr", gap: "16px" }} className="mobile-grid-1">
        <div className="flex-col gap-2">
          <label className="text-xs font-semibold text-muted uppercase">Target Name</label>
          <input
            type="text"
            className="input-field"
            placeholder="e.g. Ops Slack Channel, Team Discord"
            value={name}
            onChange={e => setName(e.target.value)}
            required
          />
        </div>

        <div className="flex-col gap-2">
          <label className="text-xs font-semibold text-muted uppercase">Channel Type</label>
          <select
            className="input-field"
            value={type}
            onChange={e => setType(e.target.value as "webhook" | "discord")}
            required
          >
            <option value="webhook">Custom Webhook URL</option>
            <option value="discord">Discord Webhook</option>
          </select>
        </div>
      </div>

      <div className="flex-col gap-2">
        <label className="text-xs font-semibold text-muted uppercase">Destination Endpoint URL</label>
        <input
          type="url"
          className="input-field"
          placeholder="https://hooks.slack.com/services/... or https://discord.com/api/webhooks/..."
          value={destination}
          onChange={e => setDestination(e.target.value)}
          required
        />
      </div>

      <div className="flex-col gap-2">
        <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center" }}>
          <label className="text-xs font-semibold text-muted uppercase">Custom Payload Template (Optional JSON)</label>
        </div>
        <span style={{ fontSize: "11px", display: "block", color: "var(--text-muted)", marginTop: "-4px" }}>
          Variables like {"{{STATUS}}"}, {"{{NICKNAME}}"}, {"{{VALUE}}"} supported
        </span>
        <textarea
          className="input-field"
          rows={5}
          style={{ fontFamily: "monospace", fontSize: "12px" }}
          placeholder='{ "content": "Alert triggered: {{NICKNAME}} is {{STATUS}}!" }'
          value={payload}
          onChange={e => setPayload(e.target.value)}
        />
      </div>

      <PayloadTemplateGuide />
    </div>
  );
}
