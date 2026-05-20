import { BrowserRouter, Routes, Route } from 'react-router-dom';
import { getPublicPath } from './lib/env';
import PublicDashboard from './app/PublicDashboard';

function App() {
  // We use the public path from our Go environment as the basename
  // This handles /dashboard or any custom path automatically.
  const basename = getPublicPath().replace(/\/$/, "");

  return (
    <BrowserRouter basename={basename}>
      <Routes>
        <Route path="/" element={<PublicDashboard />} />
        <Route path="/:slug" element={<PublicDashboard />} />
        <Route path="/:slug/:agentId" element={<PublicDashboard />} />
      </Routes>
    </BrowserRouter>
  );
}

export default App;
