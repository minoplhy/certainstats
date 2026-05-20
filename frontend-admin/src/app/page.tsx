"use client";

import { useEffect, useState } from "react";
import { isPanelContext, resolvePanelPath } from "../lib/env";
import AdminPanel from "./agent/AdminPanel";

export default function RootSwitcher() {
  const [isReady, setIsReady] = useState(false);

  useEffect(() => {
    setIsReady(true);

    const isPanel = isPanelContext();
    if (isPanel) {
      const panelPath = resolvePanelPath("/");
      if (window.location.pathname !== panelPath && window.location.pathname === "/") {
        window.location.replace(panelPath);
      }
    }
  }, []);

  if (!isReady) {
    return (
      <div style={{ height: '100vh', width: '100%', background: 'var(--bg-primary)' }} />
    );
  }

  return <AdminPanel />;
}