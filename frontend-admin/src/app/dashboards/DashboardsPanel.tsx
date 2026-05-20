import React, { useEffect, useState } from "react";
import { useNavigate, Link } from "react-router-dom";
import { fetchAPI } from "../../lib/api";
import { getPublicPath } from "../../lib/env";
import { Dashboard } from "../../types";
import { isUnauthorized } from "../../lib/utils";
import DeleteConfirmModal from "../common/DeleteConfirmModal";

export const DashboardsPanel: React.FC = () => {
  const navigate = useNavigate();
  const [dashboards, setDashboards] = useState<Dashboard[]>([]);
  const [isLoading, setIsLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [deletingId, setDeletingId] = useState<string | null>(null);
  const [deleteConfirmDash, setDeleteConfirmDash] = useState<{ id: string; title: string } | null>(null);

  useEffect(() => {
    fetchAPI<Dashboard[]>("/api/dashboards")
      .then((data) => {
        setDashboards(data || []);
      })
      .catch((err) => {
        if (isUnauthorized(err)) {
          navigate("/login", { replace: true });
        } else {
          setError(err.message);
        }
      })
      .finally(() => {
        setIsLoading(false);
      });
  }, [navigate]);

  const handleDelete = (id: string, title: string) => {
    setDeleteConfirmDash({ id, title });
  };

  const handleDeleteExecute = async (id: string) => {
    setDeleteConfirmDash(null);
    setDeletingId(id);
    try {
      await fetchAPI(`/api/dashboard/${id}`, { method: "DELETE" });
      setDashboards((prev) => prev.filter((d) => d.dashboard_id !== id));
    } catch (err: unknown) {
      if (isUnauthorized(err)) {
        navigate("/login", { replace: true });
      } else {
        setError(err instanceof Error ? err.message : "Failed to delete");
      }
    } finally {
      setDeletingId(null);
    }
  };

  return (
    <div className="mobile-p-sm" style={{ padding: '32px' }}>
      <div className="animate-fade-in mobile-gap-sm" style={{ maxWidth: '1000px', margin: '0 auto' }}>

        <div className="mobile-stack" style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: '32px', gap: '16px', flexWrap: 'wrap' }}>
          <div>
            <h1 className="font-display mobile-text-lg" style={{ fontSize: '28px', fontWeight: '700', marginBottom: '8px' }}>Public Status Pages</h1>
            <p style={{ color: 'var(--text-secondary)', fontSize: '14px' }}>Manage and share external views of your agent telemetry.</p>
          </div>
          <Link to="/dashboards/create" className="btn-primary mobile-full" style={{ gap: '8px', justifyContent: 'center' }}>
            <span className="material-symbols-outlined" style={{ fontSize: '18px' }}>add</span>
            New Page
          </Link>
        </div>

        {isLoading ? (
          <div style={{ padding: '64px', textAlign: 'center', color: 'var(--text-muted)' }}>
            <span className="material-symbols-outlined" style={{ fontSize: '32px', animation: 'spin 1s linear infinite' }}>sync</span>
            <p style={{ marginTop: '16px', fontSize: '14px', fontWeight: '500' }}>Loading pages...</p>
          </div>
        ) : error ? (
          <div style={{ background: 'rgba(239, 68, 68, 0.1)', border: '1px solid rgba(239, 68, 68, 0.3)', borderRadius: '8px', padding: '16px', color: 'var(--status-offline)' }}>
            <div style={{ display: 'flex', alignItems: 'center', gap: '8px', fontWeight: '600', marginBottom: '4px' }}>
              <span className="material-symbols-outlined">error</span>
              Failed to load pages
            </div>
            <p style={{ fontSize: '13px' }}>{error}</p>
          </div>
        ) : dashboards.length === 0 ? (
          <div className="glass-panel" style={{ padding: '64px', textAlign: 'center' }}>
            <div style={{ width: '64px', height: '64px', borderRadius: '50%', background: 'var(--bg-secondary)', border: '1px solid var(--border-color)', display: 'flex', alignItems: 'center', justifyContent: 'center', margin: '0 auto 16px', color: 'var(--text-muted)' }}>
              <span className="material-symbols-outlined" style={{ fontSize: '32px' }}>public_off</span>
            </div>
            <h3 style={{ fontSize: '18px', fontWeight: '600', marginBottom: '8px', color: 'var(--text-primary)' }}>No public pages</h3>
            <p style={{ color: 'var(--text-secondary)', fontSize: '14px', marginBottom: '24px' }}>Create your first public status page to start sharing metrics externally.</p>
            <Link to="/dashboards/create" className="btn-secondary">
              Create Page
            </Link>
          </div>
        ) : (
          <div className="grid-charts mobile-grid-1" style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fill, minmax(300px, 1fr))', gap: '20px' }}>
            {dashboards.map((dash) => (
              <div key={dash.dashboard_id} className="card" style={{ padding: '24px', display: 'flex', flexDirection: 'column' }}>
                <h2 className="font-display mobile-text-lg" style={{ fontSize: '18px', fontWeight: '600', marginBottom: '4px' }}>{dash.title}</h2>
                <p className="font-mono" style={{ fontSize: '12px', color: 'var(--accent-primary)', marginBottom: '24px' }}>/{dash.slug}</p>

                <div style={{ marginTop: 'auto', paddingTop: '16px', borderTop: '1px solid var(--border-color)', display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
                  <a
                    href={`${getPublicPath().replace(/\/$/, '')}/${dash.slug}`}
                    target="_blank"
                    style={{ fontSize: '13px', fontWeight: '600', color: 'var(--text-secondary)', display: 'flex', alignItems: 'center', gap: '6px', transition: 'var(--transition-fast)' }}
                    onMouseOver={(e) => e.currentTarget.style.color = 'var(--text-primary)'}
                    onMouseOut={(e) => e.currentTarget.style.color = 'var(--text-secondary)'}
                  >
                    <span className="material-symbols-outlined" style={{ fontSize: '16px' }}>open_in_new</span>
                    View Page
                  </a>

                  <div className="flex items-center gap-2">
                    <Link
                      to={`/dashboards/edit?id=${dash.dashboard_id}`}
                      style={{ padding: '6px', borderRadius: '6px', background: 'var(--bg-secondary)', color: 'var(--text-secondary)', transition: 'var(--transition-fast)', display: 'flex', alignItems: 'center', justifyContent: 'center' }}
                      onMouseOver={(e) => { e.currentTarget.style.color = 'var(--text-primary)'; e.currentTarget.style.background = 'rgba(255,255,255,0.1)'; }}
                      onMouseOut={(e) => { e.currentTarget.style.color = 'var(--text-secondary)'; e.currentTarget.style.background = 'var(--bg-secondary)'; }}
                      title="Edit"
                    >
                      <span className="material-symbols-outlined" style={{ fontSize: '18px' }}>edit</span>
                    </Link>
                    <button
                      onClick={() => handleDelete(dash.dashboard_id, dash.title)}
                      disabled={deletingId === dash.dashboard_id}
                      style={{ padding: '6px', borderRadius: '6px', background: 'var(--bg-secondary)', color: 'var(--status-offline)', transition: 'var(--transition-fast)', display: 'flex', alignItems: 'center', justifyContent: 'center' }}
                      onMouseOver={(e) => { if (deletingId !== dash.dashboard_id) e.currentTarget.style.background = 'rgba(239, 68, 68, 0.15)'; }}
                      onMouseOut={(e) => { if (deletingId !== dash.dashboard_id) e.currentTarget.style.background = 'var(--bg-secondary)'; }}
                      title="Delete"
                    >
                      <span className="material-symbols-outlined" style={{ fontSize: '18px' }}>delete</span>
                    </button>
                  </div>
                </div>
              </div>
            ))}
          </div>
        )}
      </div>

      <DeleteConfirmModal
        isOpen={deleteConfirmDash !== null}
        title="Delete Dashboard?"
        message={
          <>
            Are you sure you want to delete "<strong>{deleteConfirmDash?.title}</strong>"? This action cannot be undone and will permanently remove this public dashboard page.
          </>
        }
        confirmText="Delete Dashboard"
        onClose={() => setDeleteConfirmDash(null)}
        onConfirm={() => {
          if (deleteConfirmDash) {
            handleDeleteExecute(deleteConfirmDash.id);
          }
        }}
      />
    </div>
  );
};
