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
  const [restarting, setRestarting] = useState(false);

  // Check token validity on boot if a token was supplied in URL
  useEffect(() => {
    if (urlToken) {
      verifyToken(urlToken);
    }
  }, [urlToken]);

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

  // Modern React 19 Form Action for token unlock - automatically intercepts form submits
  const handleUnlock = async (formData: FormData) => {
    const tokenInput = (formData.get('setupToken') as string) || '';
    if (!tokenInput.trim()) {
      setError('Please enter your secure setup token');
      return;
    }
    verifyToken(tokenInput);
  };

  // Modern React 19 Form Action for credentials submission
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
      setSuccess(true);
    } catch (err: any) {
      setError(err.message || 'An error occurred during account creation');
    } finally {
      setVerifying(false);
    }
  };

  const handleRestart = async () => {
    setError('');
    setRestarting(true);
    try {
      await fetchAPI('/api/first-time-setup/restart', {
        method: 'POST',
      });
      setTimeout(() => {
        navigate('/');
        window.location.reload();
      }, 5000);
    } catch (err: any) {
      setRestarting(false);
      setError('Failed to request server restart. You can safely refresh the page instead.');
    }
  };

  const handleGoToLogin = () => {
    navigate('/');
    window.location.reload();
  };

  // Lock Screen (if token is not provided or invalid)
  if (!tokenValid && !success) {
    return (
      <div className="flex items-center justify-center p-6 animate-fade-in" style={{ minHeight: '100vh', background: 'radial-gradient(circle at top, #141722 0%, #090a0f 100%)' }}>
        <div className="glass-panel card flex flex-col gap-6 w-full" style={{ maxWidth: '440px' }}>
          <div className="flex flex-col items-center gap-3">
            <div style={{ fontSize: '48px', filter: 'drop-shadow(0 0 10px rgba(239, 68, 68, 0.4))' }}>🔒</div>
            <h2 className="text-xl font-bold font-display text-primary text-center">CertainStats is Locked</h2>
            <p className="text-sm text-secondary text-center leading-relaxed">
              First-time setup is required. Please check your container/server startup logs to obtain your secure 32-byte setup token.
            </p>
          </div>

          {error && (
            <div style={{
              background: 'rgba(239, 68, 68, 0.08)',
              border: '1px solid rgba(239, 68, 68, 0.15)',
              color: 'var(--status-offline)',
              borderRadius: '8px',
              padding: '12px 16px',
              fontSize: '13px',
              lineHeight: '1.4'
            }}>
              <span className="font-semibold">Verification Failed: </span>
              {error}
            </div>
          )}

          <form action={handleUnlock} className="flex flex-col gap-4">
            <div className="flex flex-col gap-2">
              <label className="text-xs font-semibold text-secondary uppercase tracking-wider">Secure Setup Token</label>
              <input
                type="text"
                name="setupToken"
                value={inputToken}
                onChange={(e) => setInputToken(e.target.value)}
                placeholder="Paste secure 32-byte token here"
                required
                disabled={verifying}
                className="input-field"
              />
            </div>

            <button type="submit" disabled={verifying} className="btn-primary w-full">
              {verifying ? 'Verifying Token...' : 'Unlock Setup'}
            </button>
          </form>
        </div>
      </div>
    );
  }

  // Account Created Success Screen
  if (success) {
    return (
      <div className="flex items-center justify-center p-6 animate-fade-in" style={{ minHeight: '100vh', background: 'radial-gradient(circle at top, #141722 0%, #090a0f 100%)' }}>
        <div className="glass-panel card flex flex-col gap-6 w-full" style={{ maxWidth: '440px' }}>
          <div className="flex flex-col items-center gap-3">
            <div className="flex items-center justify-center" style={{
              width: '56px',
              height: '56px',
              borderRadius: '50%',
              border: '2px solid var(--status-online)',
              background: 'rgba(16, 185, 129, 0.05)',
              filter: 'drop-shadow(0 0 10px rgba(16, 185, 129, 0.4))'
            }}>
              <span style={{ color: 'var(--status-online)', fontSize: '24px', fontWeight: 'bold' }}>✓</span>
            </div>
            <h2 className="text-xl font-bold font-display text-primary text-center">Account Created Successfully!</h2>
            <p className="text-sm text-secondary text-center leading-relaxed">
              Your administrator account <strong className="text-primary">{username}</strong> has been provisioned. To finalize security, choose a de-registration strategy below:
            </p>
          </div>

          {restarting ? (
            <div className="flex flex-col gap-3 items-center">
              <div style={{
                width: '24px',
                height: '24px',
                borderRadius: '50%',
                border: '2px solid var(--border-hover)',
                borderTopColor: 'var(--accent-primary)',
                animation: 'spin 1s linear infinite'
              }} />
              <p className="text-sm text-secondary text-center leading-relaxed">
                Server is performing a clean system reboot. Redirecting to login in a few seconds...
              </p>
            </div>
          ) : (
            <div className="flex flex-col gap-3">
              <button onClick={handleGoToLogin} className="btn-primary w-full">
                Go to Login (Dynamic Deregistration)
              </button>
              <button onClick={handleRestart} className="btn-secondary w-full">
                Restart Server (Supervisor Boot)
              </button>
            </div>
          )}
        </div>
      </div>
    );
  }

  // Active Setup Form (shown once token is unlocked)
  return (
    <div className="flex items-center justify-center p-6 animate-fade-in" style={{ minHeight: '100vh', background: 'radial-gradient(circle at top, #141722 0%, #090a0f 100%)' }}>
      <div className="glass-panel card flex flex-col gap-6 w-full" style={{ maxWidth: '440px' }}>
        <div className="flex flex-col items-center gap-3">
          <div style={{ fontSize: '48px', filter: 'drop-shadow(0 0 10px rgba(99, 102, 241, 0.4))' }}>🛡️</div>
          <h2 className="text-xl font-bold font-display text-primary text-center">Setup Unlocked</h2>
          <p className="text-sm text-secondary text-center leading-relaxed">Configure your CertainStats administrator credentials.</p>
        </div>

        {error && (
          <div style={{
            background: 'rgba(239, 68, 68, 0.08)',
            border: '1px solid rgba(239, 68, 68, 0.15)',
            color: 'var(--status-offline)',
            borderRadius: '8px',
            padding: '12px 16px',
            fontSize: '13px',
            lineHeight: '1.4'
          }}>
            <span className="font-semibold">Error: </span>
            {error}
          </div>
        )}

        <form action={handleSubmit} className="flex flex-col gap-4">
          <div className="flex flex-col gap-2">
            <label className="text-xs font-semibold text-secondary uppercase tracking-wider">Admin Username</label>
            <input
              type="text"
              value={username}
              onChange={(e) => setUsername(e.target.value)}
              placeholder="e.g., admin"
              required
              disabled={verifying}
              className="input-field"
            />
          </div>

          <div className="flex flex-col gap-2">
            <label className="text-xs font-semibold text-secondary uppercase tracking-wider">Password</label>
            <input
              type="password"
              value={password}
              onChange={(e) => setPassword(e.target.value)}
              placeholder="Min. 8 characters"
              required
              disabled={verifying}
              className="input-field"
            />
          </div>

          <div className="flex flex-col gap-2">
            <label className="text-xs font-semibold text-secondary uppercase tracking-wider">Confirm Password</label>
            <input
              type="password"
              value={passwordConfirm}
              onChange={(e) => setPasswordConfirm(e.target.value)}
              placeholder="Repeat password"
              required
              disabled={verifying}
              className="input-field"
            />
          </div>

          <button type="submit" disabled={verifying} className="btn-primary w-full">
            {verifying ? 'Creating Account...' : 'Complete Setup & Register'}
          </button>
        </form>
      </div>
    </div>
  );
}
