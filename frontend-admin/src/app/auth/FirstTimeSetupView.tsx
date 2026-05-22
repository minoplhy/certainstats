import React, { useState, useEffect } from 'react';
import { useSearchParams, useNavigate } from 'react-router-dom';
import { fetchAPI } from '../../lib/api';

export default function FirstTimeSetupView() {
  const [searchParams, setSearchParams] = useSearchParams();
  const urlToken = searchParams.get('token') || '';
  const navigate = useNavigate();

  const [token, setToken] = useState(urlToken);
  const [inputToken, setInputToken] = useState('');
  const [verifying, setVerifying] = useState(false);
  const [tokenValid, setTokenValid] = useState(false);
  const [username, setUsername] = useState('');
  const [password, setPassword] = useState('');
  const [passwordConfirm, setPasswordConfirm] = useState('');
  const [error, setError] = useState('');
  const [success, setSuccess] = useState(false);

  // Check token validity on boot if a token was supplied in URL
  useEffect(() => {
    if (urlToken) {
      verifyToken(urlToken);
    }
  }, [urlToken]);

  // Clean automatic redirect after 5 seconds on success
  useEffect(() => {
    if (success) {
      const timer = setTimeout(() => {
        navigate('/');
        window.location.reload();
      }, 5000);
      return () => clearTimeout(timer);
    }
  }, [success, navigate]);

  const verifyToken = async (tokToVerify: string) => {
    if (!tokToVerify.trim()) return;
    setVerifying(true);
    setError('');
    try {
      const res = await fetchAPI<{ valid: boolean }>(
        `/api/first-time-setup/check?token=${encodeURIComponent(tokToVerify.trim())}`
      );
      if (res.valid) {
        setToken(tokToVerify.trim());
        setTokenValid(true);
        // Put the valid token in the URL for consistency/refresh support
        setSearchParams({ token: tokToVerify.trim() }, { replace: true });
      } else {
        setError('Invalid setup token');
        setTokenValid(false);
      }
    } catch (err: any) {
      setError('Invalid or expired setup token. Please check your server logs.');
      setTokenValid(false);
    } finally {
      setVerifying(false);
    }
  };

  const handleUnlock = async (formData: FormData) => {
    const tokenInput = (formData.get('setupToken') as string) || '';
    if (!tokenInput.trim()) {
      setError('Please enter your secure setup token');
      return;
    }
    verifyToken(tokenInput);
  };

  const handleSubmit = async () => {
    setError('');

    if (!username.trim()) {
      setError('Username is required');
      return;
    }
    if (password.length < 8) {
      setError('Password must be at least 8 characters long');
      return;
    }
    if (password !== passwordConfirm) {
      setError('Passwords do not match');
      return;
    }

    try {
      setVerifying(true);

      await fetchAPI('/api/first-time-setup', {
        method: 'POST',
        body: JSON.stringify({
          token,
          username,
          password,
          password_confirm: passwordConfirm,
        }),
      });

      await fetchAPI('/api/first-time-setup/restart', {
        method: 'POST',
      });

      setSuccess(true);
    } catch (err: any) {
      setError(err.message || 'An error occurred during account creation or system reboot');
    } finally {
      setVerifying(false);
    }
  };

  return (
    <div className="mobile-p-sm" style={{
      minHeight: '100vh',
      display: 'flex',
      alignItems: 'center',
      justifyContent: 'center',
      padding: '24px',
      position: 'relative',
      background: 'var(--bg-primary)'
    }}>
      <div style={{
        position: 'absolute',
        inset: 0,
        opacity: 0.05,
        backgroundImage: 'linear-gradient(var(--accent-primary) 1px, transparent 1px), linear-gradient(90deg, var(--accent-primary) 1px, transparent 1px)',
        backgroundSize: '48px 48px',
        zIndex: 0
      }} />

      <div style={{ position: 'relative', width: '100%', maxWidth: '380px', zIndex: 10 }}>
        {/* Brand Header */}
        <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'center', gap: '12px', marginBottom: '32px' }}>
          <div style={{
            width: '32px',
            height: '32px',
            borderRadius: '8px',
            background: 'linear-gradient(135deg, var(--accent-primary), var(--accent-secondary))',
            display: 'flex',
            alignItems: 'center',
            justifyContent: 'center',
            fontSize: '14px',
            fontWeight: 'bold',
            color: '#fff',
            boxShadow: '0 4px 16px var(--accent-glow)'
          }}>
            ⬡
          </div>
          <span style={{ fontSize: '24px', fontWeight: '800', fontFamily: 'var(--font-display)', letterSpacing: '0.02em', color: 'var(--text-primary)' }}>
            CertainStats
          </span>
        </div>

        {/* Lock Screen / Setup Required View */}
        {!tokenValid && !success && (
          <div className="glass-panel animate-fade-in" style={{ padding: '32px' }}>
            <div style={{
              width: '48px',
              height: '48px',
              borderRadius: '12px',
              background: 'linear-gradient(135deg, var(--accent-primary), var(--accent-secondary))',
              display: 'flex',
              alignItems: 'center',
              justifyContent: 'center',
              color: '#fff',
              boxShadow: '0 4px 16px var(--accent-glow)',
              marginBottom: '20px'
            }}>
              <span className="material-symbols-outlined" style={{ fontSize: '24px' }}>settings</span>
            </div>

            <h1 style={{ fontSize: '20px', fontWeight: '600', marginBottom: '8px', color: 'var(--text-primary)' }}>First-Time Setup Required</h1>
            <p style={{ fontSize: '13px', color: 'var(--text-secondary)', marginBottom: '24px', lineHeight: '1.5' }}>
              To secure your new CertainStats instance, please retrieve your secure 32-byte setup token from the startup logs.
            </p>

            {error && (
              <div style={{
                background: 'rgba(239, 68, 68, 0.1)',
                border: '1px solid rgba(239, 68, 68, 0.3)',
                borderRadius: '8px',
                padding: '12px 16px',
                marginBottom: '20px',
                fontSize: '13px',
                color: 'var(--status-offline)'
              }}>
                {error}
              </div>
            )}

            <form action={handleUnlock} style={{ display: 'flex', flexDirection: 'column', gap: '16px' }}>
              <div>
                <label style={{ display: 'block', fontSize: '11px', fontWeight: '600', color: 'var(--text-secondary)', textTransform: 'uppercase', letterSpacing: '0.05em', marginBottom: '8px' }}>
                  Secure Setup Token
                </label>
                <input
                  type="text"
                  name="setupToken"
                  required
                  disabled={verifying}
                  value={inputToken}
                  onChange={(e) => setInputToken(e.target.value)}
                  className="input-field"
                  placeholder="Paste secure 32-byte token here"
                />
              </div>

              <button
                type="submit"
                disabled={verifying}
                className="btn-primary"
                style={{ width: '100%', marginTop: '8px', padding: '12px' }}
              >
                {verifying ? 'Verifying Token...' : 'Continue to Setup'}
              </button>
            </form>
          </div>
        )}

        {/* Success Card / Restart View */}
        {success && (
          <div className="glass-panel animate-fade-in" style={{ padding: '32px' }}>
            <div style={{
              width: '48px',
              height: '48px',
              borderRadius: '12px',
              background: 'rgba(16, 185, 129, 0.1)',
              border: '1px solid rgba(16, 185, 129, 0.3)',
              display: 'flex',
              alignItems: 'center',
              justifyContent: 'center',
              color: 'var(--status-online)',
              boxShadow: '0 4px 16px rgba(16, 185, 129, 0.15)',
              marginBottom: '20px'
            }}>
              <span className="material-symbols-outlined" style={{ fontSize: '24px' }}>check</span>
            </div>

            <h1 style={{ fontSize: '20px', fontWeight: '600', marginBottom: '8px', color: 'var(--text-primary)' }}>Account Created!</h1>
            <p style={{ fontSize: '13px', color: 'var(--text-secondary)', marginBottom: '24px', lineHeight: '1.5' }}>
              Your administrator account <strong style={{ color: 'var(--text-primary)' }}>{username}</strong> has been provisioned.
            </p>

            <div style={{
              display: 'flex',
              alignItems: 'center',
              gap: '12px',
              background: 'rgba(255, 255, 255, 0.02)',
              border: '1px solid var(--border-color)',
              borderRadius: '8px',
              padding: '16px',
              justifyContent: 'center'
            }}>
              <div style={{
                width: '16px',
                height: '16px',
                borderRadius: '50%',
                border: '2px solid var(--border-hover)',
                borderTopColor: 'var(--accent-primary)',
                animation: 'spin 1s linear infinite'
              }} />
              <span style={{ fontSize: '13px', fontWeight: '600', color: 'var(--text-primary)' }}>
                Server is Restarting...
              </span>
            </div>
            <style>{`
              @keyframes spin {
                to { transform: rotate(360deg); }
              }
            `}</style>
          </div>
        )}

        {/* Credentials Form / Active Setup Form View */}
        {tokenValid && !success && (
          <div className="glass-panel animate-fade-in" style={{ padding: '32px' }}>
            <div style={{
              width: '48px',
              height: '48px',
              borderRadius: '12px',
              background: 'linear-gradient(135deg, var(--accent-primary), var(--accent-secondary))',
              display: 'flex',
              alignItems: 'center',
              justifyContent: 'center',
              color: '#fff',
              boxShadow: '0 4px 16px var(--accent-glow)',
              marginBottom: '20px'
            }}>
              <span className="material-symbols-outlined" style={{ fontSize: '24px' }}>person</span>
            </div>

            <h1 style={{ fontSize: '20px', fontWeight: '600', marginBottom: '8px', color: 'var(--text-primary)' }}>Setup Administrator Account</h1>
            <p style={{ fontSize: '13px', color: 'var(--text-secondary)', marginBottom: '24px', lineHeight: '1.5' }}>
              Configure your primary administrator credentials to complete the system installation.
            </p>

            {error && (
              <div style={{
                background: 'rgba(239, 68, 68, 0.1)',
                border: '1px solid rgba(239, 68, 68, 0.3)',
                borderRadius: '8px',
                padding: '12px 16px',
                marginBottom: '20px',
                fontSize: '13px',
                color: 'var(--status-offline)'
              }}>
                {error}
              </div>
            )}

            <form action={handleSubmit} style={{ display: 'flex', flexDirection: 'column', gap: '16px' }}>
              <div>
                <label style={{ display: 'block', fontSize: '11px', fontWeight: '600', color: 'var(--text-secondary)', textTransform: 'uppercase', letterSpacing: '0.05em', marginBottom: '8px' }}>
                  Username
                </label>
                <input
                  type="text"
                  required
                  disabled={verifying}
                  value={username}
                  onChange={(e) => setUsername(e.target.value)}
                  className="input-field"
                  placeholder="e.g., admin"
                />
              </div>

              <div>
                <label style={{ display: 'block', fontSize: '11px', fontWeight: '600', color: 'var(--text-secondary)', textTransform: 'uppercase', letterSpacing: '0.05em', marginBottom: '8px' }}>
                  Password
                </label>
                <input
                  type="password"
                  required
                  disabled={verifying}
                  value={password}
                  onChange={(e) => setPassword(e.target.value)}
                  className="input-field"
                  placeholder="Min. 8 characters"
                />
              </div>

              <div>
                <label style={{ display: 'block', fontSize: '11px', fontWeight: '600', color: 'var(--text-secondary)', textTransform: 'uppercase', letterSpacing: '0.05em', marginBottom: '8px' }}>
                  Confirm Password
                </label>
                <input
                  type="password"
                  required
                  disabled={verifying}
                  value={passwordConfirm}
                  onChange={(e) => setPasswordConfirm(e.target.value)}
                  className="input-field"
                  placeholder="Repeat password"
                />
              </div>

              <button
                type="submit"
                disabled={verifying}
                className="btn-primary"
                style={{ width: '100%', marginTop: '8px', padding: '12px' }}
              >
                {verifying ? 'Creating Account...' : 'Complete Setup & Register'}
              </button>
            </form>
          </div>
        )}
      </div>
    </div>
  );
}
