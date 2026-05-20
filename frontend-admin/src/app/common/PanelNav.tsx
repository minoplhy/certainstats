import { useState, useEffect } from "react";
import { Link, useLocation, useNavigate } from "react-router-dom";
import { fetchAPI } from "../../lib/api";
import { getPanelPath, resolvePanelPath } from "../../lib/env";

interface PanelNavProps {
  section?: string;
}

export default function PanelNav({
  section
}: PanelNavProps) {
  const location = useLocation();
  const path = location.pathname;
  const navigate = useNavigate();

  // Auto-detect section if not provided
  const currentSection = section || (() => {
    if (path.includes("/dashboards")) return "dashboards";
    if (path.includes("/alerts")) return "alerts";
    if (path.includes("/management")) return "management";
    if (path.includes("/settings")) return "settings";
    return "agent";
  })();

  const [theme, setTheme] = useState<'light' | 'dark'>(() => {
    const saved = localStorage.getItem('theme');
    if (saved === 'light' || saved === 'dark') return saved;
    return window.matchMedia('(prefers-color-scheme: light)').matches ? 'light' : 'dark';
  });

  useEffect(() => {
    document.documentElement.setAttribute('data-theme', theme);
    localStorage.setItem('theme', theme);
  }, [theme]);

  const toggleTheme = () => setTheme(prev => prev === 'light' ? 'dark' : 'light');

  const [showProfileMenu, setShowProfileMenu] = useState(false);

  useEffect(() => {
    if (!showProfileMenu) return;
    const closeMenu = () => setShowProfileMenu(false);
    document.addEventListener("click", closeMenu);
    return () => document.removeEventListener("click", closeMenu);
  }, [showProfileMenu]);

  const handleSignOut = async () => {
    try {
      await fetchAPI("/api/logout", { method: "POST" });
    } catch (e) {
      console.error("Logout failed", e);
    }
    navigate("/login");
  };

  const navLink = (targetSection: string, label: string, href: string, className?: string) => {
    const active = currentSection === targetSection;
    return (
      <Link
        to={href}
        className={className}
        style={{
          fontSize: '13px',
          fontWeight: active ? '600' : '500',
          color: active ? 'var(--accent-primary)' : 'var(--text-secondary)',
          borderBottom: active ? '2px solid var(--accent-primary)' : '2px solid transparent',
          padding: '0 4px',
          height: '100%',
          display: 'flex',
          alignItems: 'center',
          transition: 'var(--transition-fast)',
          textDecoration: 'none',
          whiteSpace: 'nowrap'
        }}
        onMouseOver={(e) => { if (!active) e.currentTarget.style.color = 'var(--text-primary)'; }}
        onMouseOut={(e) => { if (!active) e.currentTarget.style.color = 'var(--text-secondary)'; }}
      >
        {label}
      </Link>
    );
  };

  const menuLink = (label: string, icon: string, to?: string, onClick?: () => void, color?: string) => (
    <div
      onClick={onClick || (() => to && navigate(to))}
      style={{
        display: 'flex',
        alignItems: 'center',
        gap: '12px',
        padding: '10px 16px',
        borderRadius: '8px',
        fontSize: '13px',
        fontWeight: '500',
        color: color || 'var(--text-primary)',
        cursor: 'pointer',
        transition: 'var(--transition-fast)',
      }}
      className="menu-item-hover"
      onMouseOver={(e) => e.currentTarget.style.background = 'rgba(255,255,255,0.05)'}
      onMouseOut={(e) => e.currentTarget.style.background = 'transparent'}
    >
      <span className="material-symbols-outlined" style={{ fontSize: '18px', opacity: 0.8 }}>{icon}</span>
      <span>{label}</span>
    </div>
  );

  return (
    <>
      <nav style={{
        width: '100%',
        minHeight: '64px',
        borderBottom: '1px solid var(--border-color)',
        position: 'sticky',
        top: 0,
        zIndex: 50,
        background: 'var(--bg-primary)',
        display: 'flex',
        alignItems: 'center',
        justifyContent: 'center',
        padding: '0 32px',
        flexShrink: 0
      }}>
        <div style={{ maxWidth: '1200px', width: '100%', height: '100%', display: 'flex', alignItems: 'center', justifyContent: 'space-between' }}>
          <div style={{ display: 'flex', alignItems: 'center', gap: '16px', height: '100%' }}>
            <Link to="/" style={{ display: 'flex', alignItems: 'center', gap: '12px', textDecoration: 'none', flexShrink: 0 }}>
              <div style={{
                width: '32px',
                height: '32px',
                background: 'linear-gradient(135deg, #6366f1, #8b5cf6)',
                borderRadius: '8px',
                display: 'flex',
                alignItems: 'center',
                justifyContent: 'center',
                boxShadow: '0 4px 12px rgba(99, 102, 241, 0.3)'
              }}>
                <span className="material-symbols-outlined" style={{ fontSize: '20px', color: '#fff' }}>hub</span>
              </div>
              <span style={{ fontSize: '18px', fontWeight: '800', letterSpacing: '-0.02em', color: 'var(--text-primary)', fontFamily: 'var(--font-display)' }}>CertainStats</span>
            </Link>

            <div className="nav-links mobile-hide" style={{ display: 'flex', height: '100%', alignItems: 'center', gap: '24px', marginLeft: '32px' }}>
              {navLink("agents", "Agents", "/")}
              {navLink("dashboards", "Dashboards", "/dashboards")}
              {navLink("alerts", "Alerts", "/alerts")}
            </div>
          </div>

          <div style={{ display: 'flex', alignItems: 'center', gap: '20px', flexShrink: 0 }}>

            <button
              onClick={toggleTheme}
              style={{ color: 'var(--text-secondary)', display: 'flex' }}
              title={`Switch to ${theme === 'light' ? 'dark' : 'light'} mode`}
            >
              <span className="material-symbols-outlined" style={{ fontSize: '22px' }}>
                {theme === 'light' ? 'dark_mode' : 'light_mode'}
              </span>
            </button>
            <div style={{ width: '1px', height: '20px', background: 'var(--border-color)', margin: '0 4px' }} />

            <div style={{ position: 'relative' }}>
              <button
                onClick={(e) => { e.stopPropagation(); setShowProfileMenu(!showProfileMenu); }}
                style={{
                  display: 'flex',
                  alignItems: 'center',
                  gap: '8px',
                  color: showProfileMenu ? 'var(--text-primary)' : 'var(--text-secondary)',
                  transition: 'var(--transition-fast)'
                }}
                onMouseOver={(e) => e.currentTarget.style.color = 'var(--text-primary)'}
                onMouseOut={(e) => { if (!showProfileMenu) e.currentTarget.style.color = 'var(--text-secondary)'; }}
                title="Profile"
              >
                <span className="material-symbols-outlined" style={{ fontSize: '28px' }}>account_circle</span>
              </button>

              {showProfileMenu && (
                <div
                  className="glass-panel"
                  style={{
                    position: 'absolute',
                    top: '48px',
                    right: '0',
                    width: '220px',
                    padding: '8px',
                    zIndex: 110,
                    display: 'flex',
                    flexDirection: 'column',
                    gap: '2px',
                    boxShadow: '0 8px 32px rgba(0,0,0,0.5)',
                    border: '1px solid var(--border-color)',
                    background: 'var(--bg-secondary)',
                    borderRadius: '12px'
                  }}
                >
                  <div style={{ padding: '8px 16px 12px', borderBottom: '1px solid var(--border-color)', marginBottom: '4px' }}>
                    <div style={{ fontSize: '11px', fontWeight: '800', color: 'var(--text-muted)', textTransform: 'uppercase', letterSpacing: '0.05em' }}>User Account</div>
                  </div>
                  {menuLink("Agents Management", "settings", "/agents/management")}
                  {menuLink("User Management", "key", "/settings")}
                  <div style={{ height: '1px', background: 'var(--border-color)', margin: '4px 0' }} />
                  {menuLink("Sign Out", "logout", undefined, handleSignOut, 'var(--status-offline)')}
                </div>
              )}
            </div>
          </div>
        </div>
      </nav>

      {/* MOBILE-ONLY SECONDARY TAB BAR */}
      <div
        className="mobile-only"
        style={{
          display: 'none',
          width: '100%',
          background: 'var(--bg-primary)',
          borderBottom: '1px solid var(--border-color)',
          padding: '0 16px',
          height: '48px',
          alignItems: 'center',
          justifyContent: 'center',
          gap: '24px',
          overflowX: 'auto',
          flexShrink: 0
        }}
      >
        {navLink("agents", "Agents", "/")}
        {navLink("dashboards", "Dashboards", "/dashboards")}
        {navLink("alerts", "Alerts", "/alerts")}
      </div>
    </>
  );
}