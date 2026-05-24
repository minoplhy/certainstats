import { useState, useEffect } from "react";
import { useNavigate } from "react-router-dom";
import { fetchAPI } from "../../lib/api";
import { Agent, MetricKey } from "../../types";
import DashboardFormFields from "./DashboardFormFields";

export default function DashboardCreateView() {
  const navigate = useNavigate();
  const [isClient, setIsClient] = useState(false);

  const [title, setTitle] = useState("");
  const [slug, setSlug] = useState("");
  const [maxDays, setMaxDays] = useState(7);
  const [allowedFeatures, setAllowedFeatures] = useState<string[]>(["is_online", "uptime", "cpu_model", "ram_size", "disk_size"]);
  const [allowedFields, setAllowedFields] = useState<MetricKey[]>(["agent_cpu_usage", "agent_ram_used"]);

  const [availableAgents, setAvailableAgents] = useState<Agent[]>([]);
  const [selectedAgents, setSelectedAgents] = useState<Record<string, string>>({});
  const [selectedAgentsOrder, setSelectedAgentsOrder] = useState<string[]>([]);
  const [isDragged, setIsDragged] = useState(false);

  const [loadingAgents, setLoadingAgents] = useState(true);
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    setIsClient(true);
    fetchAPI<Agent[]>("/api/agents")
      .then((d) => setAvailableAgents(d || []))
      .catch((e) => setError("Failed to load agents: " + e.message))
      .finally(() => setLoadingAgents(false));
  }, []);

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    setError(null);
    const agentsPayload = selectedAgentsOrder.map((agent_id, index) => ({
      agent_id,
      alias: selectedAgents[agent_id] || "Server",
      sort_key: isDragged ? String(index).padStart(8, '0') : ""
    }));
    if (agentsPayload.length === 0) { setError("Select at least one agent."); return; }

    setSubmitting(true);
    const accessRulesMap = { public: { allowed_fields: allowedFeatures, allowed_metrics: allowedFields, max_days: maxDays } };
    const payload = { title, slug, access_control: JSON.stringify(accessRulesMap), agents: agentsPayload };

    try {
      await fetchAPI("/api/dashboard", { method: "POST", body: JSON.stringify(payload) });
      navigate("/dashboards");
    } catch (err: unknown) {
      setError(err instanceof Error ? err.message : "Failed to create dashboard");
      setSubmitting(false);
    }
  };

  if (!isClient) return null;

  return (
    <div className="mobile-p-sm" style={{ padding: '40px 24px' }}>

        <div className="animate-fade-in mobile-gap-sm" style={{ maxWidth: '800px', margin: '0 auto' }}>

          <div className="mobile-stack" style={{ display: 'flex', alignItems: 'center', gap: '16px', marginBottom: '32px' }}>
            <button 
              type="button" 
              onClick={() => navigate("/dashboards")} 
              className="btn-secondary" 
              style={{ width: '36px', height: '36px', borderRadius: '50%', display: 'flex', alignItems: 'center', justifyContent: 'center', flexShrink: 0 }}
            >
              <span className="material-symbols-outlined" style={{ fontSize: '20px' }}>arrow_back</span>
            </button>
            <div>
              <h1 className="font-display mobile-text-lg" style={{ fontSize: '24px', fontWeight: '700', marginBottom: '4px' }}>New Status Page</h1>
              <p style={{ fontSize: '13px', color: 'var(--text-secondary)' }}>Configure a new external telemetry dashboard</p>
            </div>
          </div>

          {error && (
            <div style={{ background: 'rgba(239, 68, 68, 0.1)', border: '1px solid rgba(239, 68, 68, 0.3)', borderRadius: '8px', padding: '16px', color: 'var(--status-offline)', marginBottom: '24px' }}>
              {error}
            </div>
          )}

          <form onSubmit={handleSubmit} style={{ display: 'flex', flexDirection: 'column', gap: '24px' }}>
            <DashboardFormFields
              title={title}
              setTitle={setTitle}
              slug={slug}
              setSlug={setSlug}
              maxDays={maxDays}
              setMaxDays={setMaxDays}
              allowedFeatures={allowedFeatures}
              setAllowedFeatures={setAllowedFeatures}
              allowedFields={allowedFields}
              setAllowedFields={setAllowedFields}
              selectedAgents={selectedAgents}
              setSelectedAgents={setSelectedAgents}
              selectedAgentsOrder={selectedAgentsOrder}
              setSelectedAgentsOrder={setSelectedAgentsOrder}
              isDragged={isDragged}
              setIsDragged={setIsDragged}
              availableAgents={availableAgents}
              loadingAgents={loadingAgents}
            />

            <style>{`
              .custom-scrollbar::-webkit-scrollbar {
                width: 6px;
              }
              .custom-scrollbar::-webkit-scrollbar-track {
                background: transparent;
              }
              .custom-scrollbar::-webkit-scrollbar-thumb {
                background: var(--border-color);
                border-radius: 3px;
              }
              .custom-scrollbar::-webkit-scrollbar-thumb:hover {
                background: var(--text-muted);
              }
            `}</style>

            <button type="submit" disabled={submitting} className="btn-primary mobile-full" style={{ padding: '12px 32px' }}>
              {submitting ? "Creating..." : "Create Dashboard"}
            </button>
          </form>
        </div>
      </div>
  );
}
