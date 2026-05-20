import { BrowserRouter, Routes, Route, Navigate } from 'react-router-dom';
import { getPanelPath } from './lib/env';
import AdminPanel from './app/agent/AdminPanel';
import ManagementView from './app/management/ManagementView';
import LoginView from './app/auth/LoginView';
import SettingsView from './app/settings/SettingsView';
import DashboardCreateView from './app/dashboards/DashboardCreateView';
import DashboardEditView from './app/dashboards/DashboardEditView';
import AlertCreateView from './app/alerts/AlertCreateView';
import AlertEditView from './app/alerts/AlertEditView';

function App() {
  // We use the panel path from our Go environment as the basename
  const basename = getPanelPath().replace(/\/$/, "");

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
