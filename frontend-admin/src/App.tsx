import { useState, useEffect } from 'react';
import { BrowserRouter, Routes, Route, Navigate } from 'react-router-dom';
import { getPanelPath } from './lib/env';
import { fetchAPI } from './lib/api';
import AdminPanel from './app/agent/AdminPanel';
import ManagementView from './app/management/ManagementView';
import LoginView from './app/auth/LoginView';
import SettingsView from './app/settings/SettingsView';
import DashboardCreateView from './app/dashboards/DashboardCreateView';
import DashboardEditView from './app/dashboards/DashboardEditView';
import AlertCreateView from './app/alerts/AlertCreateView';
import AlertEditView from './app/alerts/AlertEditView';
import FirstTimeSetupView from './app/auth/FirstTimeSetupView';

function App() {
  // We use the panel path from our Go environment as the basename
  const basename = getPanelPath().replace(/\/$/, "");
  const [setupRequired, setSetupRequired] = useState<boolean | null>(null);

  useEffect(() => {
    fetchAPI<{ setup_required: boolean }>("/api/first-time-setup/status")
      .then((res) => {
        setSetupRequired(res.setup_required);
      })
      .catch(() => {
        setSetupRequired(false);
      });
  }, []);

  if (setupRequired === null) {
    return (
      <div style={{
        minHeight: '100vh',
        display: 'flex',
        alignItems: 'center',
        justifyContent: 'center',
        background: '#090a0f',
        color: '#8f9bb3'
      }}>
        <div style={{ display: 'flex', flexDirection: 'column', alignItems: 'center', gap: '12px' }}>
          <div style={{
            width: '32px',
            height: '32px',
            borderRadius: '50%',
            border: '2px solid rgba(255,255,255,0.1)',
            borderTopColor: '#3b82f6',
            animation: 'spin 1s linear infinite'
          }} />
          <style>{`
            @keyframes spin {
              to { transform: rotate(360deg); }
            }
          `}</style>
          <span style={{ fontSize: '13px', fontWeight: 500 }}>Initializing CertainStats...</span>
        </div>
      </div>
    );
  }

  if (setupRequired) {
    return (
      <BrowserRouter basename={basename}>
        <Routes>
          <Route path="/first-time-setup" element={<FirstTimeSetupView />} />
          <Route path="*" element={<Navigate to="/first-time-setup" replace />} />
        </Routes>
      </BrowserRouter>
    );
  }

  return (
    <BrowserRouter basename={basename}>
      <Routes>
        <Route path="/" element={<AdminPanel />} />
        <Route path="/agent/:id" element={<AdminPanel />} />
        <Route path="/dashboards" element={<AdminPanel />} />
        <Route path="/alerts" element={<AdminPanel />} />

        <Route path="/login" element={<LoginView />} />
        <Route path="/settings" element={<SettingsView />} />
        <Route path="/agents/management" element={<ManagementView />} />
        <Route path="/dashboards/create" element={<DashboardCreateView />} />
        <Route path="/dashboards/edit" element={<DashboardEditView />} />
        <Route path="/alerts/create" element={<AlertCreateView />} />
        <Route path="/alerts/edit" element={<AlertEditView />} />

        {/* Catch all - redirect back to home */}
        <Route path="*" element={<Navigate to="/" replace />} />
      </Routes>
    </BrowserRouter>
  );
}

export default App;
