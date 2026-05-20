import { useEffect, useState } from "react";
import { useNavigate, useSearchParams, Link } from "react-router-dom";
import { fetchAPI } from "../../lib/api";
import { Agent, MetricKey } from "../../types";
import PanelNav from "../common/PanelNav";
import DeleteConfirmModal from "../common/DeleteConfirmModal";
import DashboardFormFields from "./DashboardFormFields";

interface DashboardAcl {
  public?: {
    allowed_fields?: string[];
    allowed_metrics?: MetricKey[];
    max_days?: number;
  };
}

interface DashboardAgent {
  agent_id: string;
  public_agent_id: string;
  public_agent_nickname: string;
}

interface Dashboard {
  dashboard_id: string;
  slug: string;
  title: string;
  access_control?: DashboardAcl;
  agents?: DashboardAgent[];
}

function EditDashboardForm() {
  const navigate = useNavigate();
  const [searchParams] = useSearchParams();
  const id = searchParams.get("id");

  const [isClient, setIsClient] = useState(false);
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const [dashboardId, setDashboardId] = useState<string>("");
  const [title, setTitle] = useState("");
  const [slug, setSlug] = useState("");
  const [maxDays, setMaxDays] = useState(7);
  const [allowedFeatures, setAllowedFeatures] = useState<string[]>([]);
  const [allowedFields, setAllowedFields] = useState<MetricKey[]>([]);

  const [availableAgents, setAvailableAgents] = useState<Agent[]>([]);
  const [selectedAgents, setSelectedAgents] = useState<Record<string, string>>({});

  const [showDeleteModal, setShowDeleteModal] = useState(false);

  useEffect(() => {
    setIsClient(true);
    if (!id) { setError("No dashboard ID provided."); setLoading(false); return; }

    Promise.all([
      fetchAPI<Agent[]>("/api/agents"),
      fetchAPI<Dashboard>(`/api/dashboard/${encodeURIComponent(id)}`)
    ])
      .then(([agentsData, target]) => {
        const agentAgents = agentsData || [];
        setAvailableAgents(agentAgents);

        if (!target || !target.dashboard_id) {
          setError("Dashboard not found.");
        } else {
          setDashboardId(target.dashboard_id);
          setTitle(target.title || "");
          setSlug(target.slug || "");

          const pub = target.access_control?.public;
          if (pub) {
            setAllowedFeatures(pub.allowed_fields || []);
            setAllowedFields(pub.allowed_metrics || []);
            setMaxDays(pub.max_days || 7);
          }

          const agentsMap: Record<string, string> = {};
          if (target.agents) {
            target.agents.forEach(a => {
              const agentMatch = agentAgents.find(fa => fa.agent_id === a.agent_id);
              agentsMap[a.agent_id] = a.public_agent_nickname || agentMatch?.nickname || "Server";
            });
          }
          setSelectedAgents(agentsMap);
        }
      })
      .catch((err) => setError("Failed to load data: " + err.message))
      .finally(() => setLoading(false));
  }, [id]);

  const handleDelete = () => {
    setShowDeleteModal(true);
  };

  const handleDeleteExecute = async () => {
    setShowDeleteModal(false);
    try {
      await fetchAPI(`/api/dashboard/${encodeURIComponent(dashboardId)}`, { method: "DELETE" });
      navigate("/dashboards");
    } catch (err: unknown) {
      setError(err instanceof Error ? err.message : "Failed to delete dashboard");
    }
  };

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault(); setError(null);
    const agentsPayload = Object.entries(selectedAgents).map(([agent_id, alias]) => ({ agent_id, alias }));
    if (agentsPayload.length === 0) { setError("Select at least one agent."); return; }

    setSaving(true);
    const accessRulesMap = { public: { allowed_fields: allowedFeatures, allowed_metrics: allowedFields, max_days: maxDays } };
    const payload = { title, slug, access_control: JSON.stringify(accessRulesMap), agents: agentsPayload };

    try {
      await fetchAPI(`/api/dashboard/${encodeURIComponent(dashboardId)}`, { method: "PUT", body: JSON.stringify(payload) });
      navigate("/dashboards");
    } catch (err: unknown) {
      setError(err instanceof Error ? err.message : "Failed to update dashboard");
      setSaving(false);
    }
  };

  if (!isClient) return <div style={{ minHeight: '100vh', background: 'var(--bg-primary)' }} />;
  if (loading) return <div style={{ height: '100vh', display: 'flex', alignItems: 'center', justifyContent: 'center', color: 'var(--text-muted)' }}><span className="material-symbols-outlined" style={{ fontSize: '32px', animation: 'spin 1s linear infinite' }}>sync</span></div>;

  return (
    <>
      <PanelNav section="dashboards" />
      <div className="mobile-p-sm" style={{ minHeight: 'calc(100vh - 56px)', padding: '40px 24px', background: 'var(--bg-primary)' }}>
        <div className="animate-fade-in mobile-gap-sm" style={{ maxWidth: '800px', margin: '0 auto' }}>

          <div className="mobile-stack" style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', marginBottom: '32px', gap: '16px' }}>
            <div style={{ display: 'flex', alignItems: 'center', gap: '16px' }}>
              <button
                type="button"
                onClick={() => navigate("/dashboards")}
                className="btn-secondary"
                style={{ padding: '8px', borderRadius: '50%', display: 'flex' }}
              >
                <span className="material-symbols-outlined" style={{ fontSize: '20px' }}>arrow_back</span>
              </button>
              <div>
                <h1 className="font-display mobile-text-lg" style={{ fontSize: '24px', fontWeight: '700', marginBottom: '4px' }}>Edit Page</h1>
                <p style={{ fontSize: '13px', color: 'var(--text-secondary)', fontFamily: 'var(--font-mono)' }}>ID: {dashboardId}</p>
              </div>
            </div>
            <button type="button" onClick={handleDelete} className="btn-secondary mobile-full" style={{ color: 'var(--status-offline)', borderColor: 'rgba(239, 68, 68, 0.3)', gap: '8px', justifyContent: 'center' }} onMouseOver={(e) => e.currentTarget.style.background = 'rgba(239, 68, 68, 0.1)'} onMouseOut={(e) => e.currentTarget.style.background = 'rgba(255, 255, 255, 0.05)'}>
              <span className="material-symbols-outlined" style={{ fontSize: '18px' }}>delete</span> Delete
            </button>
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
              availableAgents={availableAgents}
              loadingAgents={loading}
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

            <div className="mobile-stack" style={{ display: 'flex', justifyContent: 'flex-end', gap: '12px', marginTop: '16px' }}>
              <Link to="/dashboards" className="btn-secondary mobile-full" style={{ padding: '12px 24px', textAlign: 'center' }}>Cancel</Link>
              <button type="submit" disabled={saving} className="btn-primary mobile-full" style={{ padding: '12px 32px' }}>
                {saving ? "Saving..." : "Save Dashboard"}
              </button>
            </div>
          </form>
        </div>
      </div>

      <DeleteConfirmModal
        isOpen={showDeleteModal}
        title="Delete Dashboard?"
        message={
          <>
            Are you sure you want to delete "<strong>{title}</strong>"? This action cannot be undone and will permanently remove this public dashboard page.
          </>
        }
        confirmText="Delete Dashboard"
        onClose={() => setShowDeleteModal(false)}
        onConfirm={handleDeleteExecute}
      />
    </>
  );
}

export default function DashboardEditView() {
  return (
    <EditDashboardForm />
  );
}
