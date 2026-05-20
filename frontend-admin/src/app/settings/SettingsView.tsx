import { useState, useEffect } from "react";
import { fetchAPI } from "../../lib/api";
import PanelNav from "../common/PanelNav";
import { useNavigate } from "react-router-dom";

interface Session {
  token_prefix: string;
  is_current: boolean;
  ip_address: string;
  user_agent: string;
  created_at: string;
  last_connected_at: string;
}

export default function SettingsView() {
  const [oldPassword, setOldPassword] = useState("");
  const [newPassword, setNewPassword] = useState("");
  const [confirmPassword, setConfirmPassword] = useState("");
  const [message, setMessage] = useState<{ type: "success" | "error"; text: string } | null>(null);
  const [loading, setLoading] = useState(false);
  const [sessions, setSessions] = useState<Session[]>([]);
  const navigate = useNavigate();

  const loadSessions = async () => {
    try {
      const res = await fetchAPI<Session[]>("/api/user/sessions");
      setSessions(res);
    } catch (err) {
      console.error("Failed to load sessions", err);
    }
  };

  useEffect(() => {
    loadSessions();
  }, []);

  const handleEject = async (prefix: string) => {
    try {
      await fetchAPI(`/api/user/session/${prefix}`, { method: "DELETE" });
      const ejectedSelf = sessions.find(s => s.token_prefix === prefix)?.is_current;
      if (ejectedSelf) {
        navigate('/login');
        return;
      }
      loadSessions();
    } catch (err) {
      alert("Failed to eject session");
    }
  };

  const handleEjectOthers = async () => {
    if (!confirm("Are you sure you want to revoke all other active sessions?")) return;
    try {
      await fetchAPI("/api/user/sessions/other", { method: "DELETE" });
      loadSessions();
    } catch (err) {
      alert("Failed to eject other sessions");
    }
  };

  const parseUA = (ua: string) => {
    const browser = (() => {
      if (ua.includes("Firefox")) return { name: "Firefox", icon: "firefox" };
      if (ua.includes("Chrome")) return { name: "Chrome", icon: "chrome" };
      if (ua.includes("Safari")) return { name: "Safari", icon: "browser_updated" };
      if (ua.includes("Edge")) return { name: "Edge", icon: "edge" };
      return { name: "Browser", icon: "desktop_windows" };
    })();
    const platform = (() => {
      if (ua.includes("Mac")) return "macOS";
      if (ua.includes("Windows")) return "Windows";
      if (ua.includes("Linux")) return "Linux";
      if (ua.includes("Android")) return "Android";
      if (ua.includes("iPhone") || ua.includes("iPad")) return "iOS";
      return "OS";
    })();
    return { browser, platform };
  };

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (newPassword !== confirmPassword) {
      setMessage({ type: "error", text: "New passwords do not match" });
      return;
    }

    setLoading(true);
    setMessage(null);

    try {
      await fetchAPI("/api/user/password", {
        method: "POST",
        body: JSON.stringify({
          old_password: oldPassword,
          new_password: newPassword,
        }),
      });
      setMessage({ type: "success", text: "Password changed successfully" });
      setOldPassword("");
      setNewPassword("");
      setConfirmPassword("");
    } catch (err: any) {
      setMessage({ type: "error", text: err.message || "Failed to change password" });
    } finally {
      setLoading(false);
    }
  };

  return (
    <>
      <PanelNav />
      <div className="flex flex-col w-full" style={{ height: 'calc(100vh - 56px)', overflowY: 'auto', background: 'var(--bg-primary)' }}>
        <div style={{ padding: "40px 24px", maxWidth: "960px", margin: "0 auto", width: '100%' }} className="animate-fade-in mobile-p-sm">
          <header style={{ marginBottom: "32px" }}>
            <h1 className="font-display font-bold text-2xl text-primary mobile-text-lg" style={{ letterSpacing: "-0.02em" }}>
              User Settings
            </h1>
            <p className="text-secondary text-sm" style={{ marginTop: "4px" }}>
              Manage your account security, devices, and preferences.
            </p>
          </header>

          <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(360px, 1fr))', gap: '32px', alignItems: 'start' }}>
            {/* Password Change Card */}
            <div className="card" style={{ width: "100%" }}>
              <div style={{ display: "flex", alignItems: "center", gap: "12px", marginBottom: "24px" }}>
                <span className="material-symbols-outlined text-accent" style={{ fontSize: "24px" }}>lock</span>
                <h2 className="font-display font-semibold text-lg text-primary">Security</h2>
              </div>

              <form onSubmit={handleSubmit} className="flex-col gap-4">
                <div className="flex-col gap-2">
                  <label className="text-xs font-semibold text-muted uppercase" style={{ letterSpacing: "0.05em" }}>
                    Current Password
                  </label>
                  <input
                    type="password"
                    className="input-field"
                    placeholder="••••••••"
                    value={oldPassword}
                    onChange={(e) => setOldPassword(e.target.value)}
                    required
                  />
                </div>

                <div className="flex-col gap-2">
                  <label className="text-xs font-semibold text-muted uppercase" style={{ letterSpacing: "0.05em" }}>
                    New Password
                  </label>
                  <input
                    type="password"
                    className="input-field"
                    placeholder="••••••••"
                    value={newPassword}
                    onChange={(e) => setNewPassword(e.target.value)}
                    required
                  />
                </div>

                <div className="flex-col gap-2">
                  <label className="text-xs font-semibold text-muted uppercase" style={{ letterSpacing: "0.05em" }}>
                    Confirm New Password
                  </label>
                  <input
                    type="password"
                    className="input-field"
                    placeholder="••••••••"
                    value={confirmPassword}
                    onChange={(e) => setConfirmPassword(e.target.value)}
                    required
                  />
                </div>

                {message && (
                  <div style={{
                    padding: "12px 16px",
                    borderRadius: "8px",
                    fontSize: "13px",
                    display: "flex",
                    alignItems: "center",
                    gap: "8px",
                    background: message.type === "success" ? "rgba(16, 185, 129, 0.1)" : "rgba(239, 68, 68, 0.1)",
                    color: message.type === "success" ? "var(--status-online)" : "var(--status-offline)",
                    border: `1px solid ${message.type === "success" ? "rgba(16, 185, 129, 0.2)" : "rgba(239, 68, 68, 0.2)"}`,
                    marginTop: "8px"
                  }}>
                    <span className="material-symbols-outlined" style={{ fontSize: "18px" }}>
                      {message.type === "success" ? "check_circle" : "error"}
                    </span>
                    {message.text}
                  </div>
                )}

                <div style={{ marginTop: "12px" }}>
                  <button
                    type="submit"
                    className="btn-primary w-full"
                    disabled={loading}
                  >
                    {loading ? "Updating..." : "Update Password"}
                  </button>
                </div>
              </form>
            </div>

            {/* Active Sessions Card */}
            <div className="card" style={{ width: "100%" }}>
              <div style={{ display: "flex", alignItems: "center", justifyContent: "space-between", marginBottom: "24px" }}>
                <div style={{ display: "flex", alignItems: "center", gap: "12px" }}>
                  <span className="material-symbols-outlined text-accent" style={{ fontSize: "24px" }}>devices</span>
                  <h2 className="font-display font-semibold text-lg text-primary">Connected Devices</h2>
                </div>
                {sessions.length > 1 && (
                  <button
                    onClick={handleEjectOthers}
                    style={{
                      fontSize: "11px",
                      padding: "6px 12px",
                      background: "rgba(239, 68, 68, 0.1)",
                      border: "1px solid rgba(239, 68, 68, 0.2)",
                      color: "var(--status-offline)",
                      cursor: "pointer",
                      borderRadius: "6px",
                      fontWeight: "600"
                    }}
                  >
                    Revoke Others
                  </button>
                )}
              </div>

              <div style={{ display: "flex", flexDirection: "column", gap: "16px" }}>
                {sessions.map((sess) => {
                  const { browser, platform } = parseUA(sess.user_agent);
                  const createdTime = new Date(sess.created_at).toLocaleDateString();
                  const lastTime = new Date(sess.last_connected_at).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' });

                  return (
                    <div
                      key={sess.token_prefix}
                      style={{
                        display: "flex",
                        alignItems: "center",
                        justifyContent: "space-between",
                        padding: "16px",
                        borderRadius: "10px",
                        background: sess.is_current ? "rgba(99, 102, 241, 0.05)" : "rgba(255, 255, 255, 0.02)",
                        border: sess.is_current ? "1px solid rgba(99, 102, 241, 0.2)" : "1px solid var(--border-color)",
                      }}
                    >
                      <div style={{ display: "flex", alignItems: "center", gap: "14px" }}>
                        <span className="material-symbols-outlined" style={{ fontSize: "28px", color: sess.is_current ? "var(--accent-primary)" : "var(--text-secondary)" }}>
                          {sess.user_agent.toLowerCase().includes("mobi") ? "phone_iphone" : "desktop_windows"}
                        </span>
                        <div>
                          <div style={{ display: "flex", alignItems: "center", gap: "8px" }}>
                            <span style={{ fontSize: "14px", fontWeight: "600", color: "var(--text-primary)" }}>
                              {browser.name} on {platform}
                            </span>
                            {sess.is_current && (
                              <span style={{
                                fontSize: "10px",
                                fontWeight: "bold",
                                padding: "2px 6px",
                                borderRadius: "4px",
                                background: "rgba(99, 102, 241, 0.2)",
                                color: "var(--accent-primary)"
                              }}>
                                This Device
                              </span>
                            )}
                          </div>
                          <div style={{ fontSize: "12px", color: "var(--text-secondary)", marginTop: "4px", opacity: 0.8 }}>
                            IP: {sess.ip_address} • Connected {createdTime}
                          </div>
                          <div style={{ fontSize: "11px", color: "var(--text-secondary)", marginTop: "2px", opacity: 0.6 }}>
                            Last active: {sess.is_current ? "Active now" : `Last seen at ${lastTime}`}
                          </div>
                        </div>
                      </div>

                      <button
                        onClick={() => handleEject(sess.token_prefix)}
                        style={{
                          padding: "6px",
                          borderRadius: "6px",
                          border: "none",
                          background: "transparent",
                          cursor: "pointer",
                          display: "flex",
                          alignItems: "center",
                          color: "var(--text-secondary)",
                          transition: "color 0.2s"
                        }}
                        onMouseOver={(e) => e.currentTarget.style.color = "var(--status-offline)"}
                        onMouseOut={(e) => e.currentTarget.style.color = "var(--text-secondary)"}
                        title="Revoke session"
                      >
                        <span className="material-symbols-outlined" style={{ fontSize: "20px" }}>logout</span>
                      </button>
                    </div>
                  );
                })}
              </div>
            </div>
          </div>
        </div>
      </div>
    </>
  );
}
