import React, { createContext, useContext, useState, useEffect, useRef, useCallback } from "react";
import { fetchAPI, getWSURL } from "../lib/api";
import { Agent, AgentSnapshot } from "../types";
import { isUnauthorized } from "../lib/utils";
import { useNavigate } from "react-router-dom";

interface AppContextType {
  agents: Agent[];
  setAgents: React.Dispatch<React.SetStateAction<Agent[]>>;
  liveMetrics: Record<string, AgentSnapshot>;
  loadingAgents: boolean;
  isSidebarExpanded: boolean;
  setIsSidebarExpanded: (expanded: boolean) => void;
  loadAgents: () => Promise<void>;
  showToast: (msg: string, ok?: boolean) => void;
  toast: { msg: string; ok: boolean } | null;
  filter: string;
  setFilter: (val: string) => void;
  filteredAgents: Agent[];
}

const AppContext = createContext<AppContextType | undefined>(undefined);

export function AppProvider({ children }: { children: React.ReactNode }) {
  const navigate = useNavigate();
  const [agents, setAgents] = useState<Agent[]>([]);
  const [liveMetrics, setLiveMetrics] = useState<Record<string, AgentSnapshot>>({});
  const [loadingAgents, setLoadingAgents] = useState(true);
  const [isSidebarExpanded, setIsSidebarExpanded] = useState(() => {
    return localStorage.getItem("certainstats_sidebar_expanded") === "true";
  });
  const [toast, setToast] = useState<{ msg: string; ok: boolean } | null>(null);
  const [filter, setFilter] = useState("");

  const filteredAgents = agents.filter(a => {
    const term = filter.toLowerCase();
    return (
      (a.nickname || "").toLowerCase().includes(term) ||
      (a.agent_id || "").toLowerCase().includes(term) ||
      (a.cpu_model || "").toLowerCase().includes(term) ||
      (a.agent_type || "").toLowerCase().includes(term) ||
      (a.linux_version || "").toLowerCase().includes(term)
    );
  });

  const agentsRef = useRef<Agent[]>([]);
  const toastTimer = useRef<ReturnType<typeof setTimeout> | null>(null);

  useEffect(() => {
    agentsRef.current = agents;
  }, [agents]);

  useEffect(() => {
    localStorage.setItem("certainstats_sidebar_expanded", String(isSidebarExpanded));
  }, [isSidebarExpanded]);

  const showToast = useCallback((msg: string, ok = true) => {
    setToast({ msg, ok });
    if (toastTimer.current) clearTimeout(toastTimer.current);
    toastTimer.current = setTimeout(() => setToast(null), 3200);
  }, []);

  const loadAgents = useCallback(async () => {
    try {
      const data = await fetchAPI<Agent[]>("/api/agents");
      setAgents(data ?? []);
    } catch (err: unknown) {
      if (isUnauthorized(err)) {
        navigate("/login", { replace: true });
      }
    } finally {
      setLoadingAgents(false);
    }
  }, [navigate]);

  // Initial HTTP Fetch & Background Polling
  useEffect(() => {
    loadAgents();
    const timer = setInterval(loadAgents, 30000);
    return () => clearInterval(timer);
  }, [loadAgents]);

  // Persistent WebSocket Connection
  useEffect(() => {
    const wsUrl = getWSURL("/api/ws");
    let ws: WebSocket;
    let reconnectTimer: ReturnType<typeof setTimeout>;

    function connect() {
      ws = new WebSocket(wsUrl);

      ws.onmessage = (e) => {
        try {
          const pkg = JSON.parse(e.data);
          if (pkg.type === "agent_update") {
            const snaps = pkg.data || {};
            const enriched: Record<string, any> = {};

            for (const id in snaps) {
              const snap = snaps[id];
              const agent = agentsRef.current.find(a => a.agent_id === id);
              const item = { ...snap };

              if (agent) {
                if (agent.ram_size > 0) item.RAMUsagePercent = (snap.RAMUsedBytes / agent.ram_size) * 100;
                if (agent.swap_size > 0) item.RAMSwapUsagePercent = (snap.RAMSwapUsedBytes / agent.swap_size) * 100;
                if (agent.disk_size > 0) item.DiskUsagePercent = (snap.DiskUsedBytes / agent.disk_size) * 100;
              }
              enriched[id] = item;
            }

            setLiveMetrics(prev => ({ ...prev, ...enriched }));
          }
        } catch (err) {
          // Parse error
        }
      };

      ws.onclose = () => {
        reconnectTimer = setTimeout(connect, 3000);
      };

      ws.onerror = () => {
        ws.close();
      };
    }

    connect();

    return () => {
      if (ws) ws.close();
      clearTimeout(reconnectTimer);
    };
  }, []);

  return (
    <AppContext.Provider
      value={{
        agents,
        setAgents,
        liveMetrics,
        loadingAgents,
        isSidebarExpanded,
        setIsSidebarExpanded,
        loadAgents,
        showToast,
        toast,
        filter,
        setFilter,
        filteredAgents,
      }}
    >
      {children}
    </AppContext.Provider>
  );
}

export function useApp() {
  const context = useContext(AppContext);
  if (!context) throw new Error("useApp must be used within an AppProvider");
  return context;
}
