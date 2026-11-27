import { useEffect, useState } from 'react';
import type { FormEvent } from 'react';
import { useNavigate } from 'react-router';

import { ApiError, apiRequest } from '../lib/api';

interface CurrentUser {
  id: string;
  username: string;
  email: string;
  totp_enabled: boolean;
}

interface TOTPSetup {
  secret: string;
  otpauth_url: string;
  recovery_codes: string[];
}

export function meta() {
  return [{ title: 'Account' }, { name: 'description', content: 'Authenticated account' }];
}

export default function Account() {
  const navigate = useNavigate();
  const [user, setUser] = useState<CurrentUser | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [isLoggingOut, setIsLoggingOut] = useState(false);

  useEffect(() => {
    let cancelled = false;

    async function loadCurrentUser() {
      try {
        const result = await apiRequest<CurrentUser>('/api/auth/me', { method: 'GET' });
        if (!cancelled) {
          setUser(result);
        }
      } catch (loadError) {
        if (cancelled) {
          return;
        }
        // No valid session: send the visitor to the login page.
        if (loadError instanceof ApiError && loadError.status === 401) {
          void navigate('/login', { replace: true });
          return;
        }
        setError(loadError instanceof Error ? loadError.message : 'Could not load account.');
      }
    }

    void loadCurrentUser();
    return () => {
      cancelled = true;
    };
  }, [navigate]);

  async function handleLogout() {
    setError(null);
    setIsLoggingOut(true);
    try {
      await apiRequest('/api/auth/logout');
      void navigate('/login');
    } catch (logoutError) {
      // A 401 means the session is already gone, which is what logout wants.
      if (logoutError instanceof ApiError && logoutError.status === 401) {
        void navigate('/login');
        return;
      }
      setError(logoutError instanceof Error ? logoutError.message : 'Could not log out.');
      setIsLoggingOut(false);
    }
  }

  return (
    <main className="flex min-h-screen items-center justify-center bg-slate-50 px-6 py-12 text-slate-900">
      <section className="w-full max-w-lg space-y-6 rounded-xl border border-slate-200 bg-white p-8 shadow-sm">
        <div>
          <h1 className="text-2xl font-semibold tracking-tight">You are logged in</h1>
          <p className="mt-2 text-sm text-slate-600">
            This page loaded the current user through the authenticated session cookie.
          </p>
        </div>

        {error && (
          <p className="rounded-md bg-red-50 px-3 py-2 text-sm text-red-700" role="alert">
            {error}
          </p>
        )}

        {user && (
          <>
            <dl className="space-y-3 rounded-md bg-slate-50 p-4 text-sm">
              <div className="flex justify-between gap-4">
                <dt className="font-medium text-slate-600">Username</dt>
                <dd>{user.username}</dd>
              </div>
              <div className="flex justify-between gap-4">
                <dt className="font-medium text-slate-600">Email</dt>
                <dd>{user.email}</dd>
              </div>
              <div className="flex justify-between gap-4">
                <dt className="font-medium text-slate-600">User ID</dt>
                <dd className="break-all text-right">{user.id}</dd>
              </div>
            </dl>
            <TOTPSettings
              enabled={user.totp_enabled}
              onEnabledChange={(enabled) => setUser({ ...user, totp_enabled: enabled })}
            />
          </>
        )}

        <button
          className="w-full rounded-md bg-slate-900 px-3 py-2 text-sm font-medium text-white transition hover:bg-slate-700 disabled:cursor-not-allowed disabled:opacity-50"
          type="button"
          onClick={() => {
            void handleLogout();
          }}
          disabled={isLoggingOut}
        >
          {isLoggingOut ? 'Logging out...' : 'Log out'}
        </button>
      </section>
    </main>
  );
}

function TOTPSettings({
  enabled,
  onEnabledChange,
}: {
  enabled: boolean;
  onEnabledChange: (enabled: boolean) => void;
}) {
  const [setup, setSetup] = useState<TOTPSetup | null>(null);
  const [recoveryCodes, setRecoveryCodes] = useState<string[]>([]);
  const [code, setCode] = useState('');
  const [setupAttemptsRemaining, setSetupAttemptsRemaining] = useState(5);
  const [savedCodes, setSavedCodes] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [message, setMessage] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  async function startSetup() {
    setError(null);
    setMessage(null);
    setBusy(true);
    try {
      const result = await apiRequest<TOTPSetup>('/api/auth/totp/setup');
      setSetup(result);
      setRecoveryCodes(result.recovery_codes);
      setSavedCodes(false);
      setSetupAttemptsRemaining(5);
      setCode('');
    } catch (setupError) {
      setError(toTOTPSettingsMessage(setupError, 'setup'));
    } finally {
      setBusy(false);
    }
  }

  async function enableTOTP(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setError(null);
    setMessage(null);
    if (!savedCodes) {
      setError('Save your recovery codes before continuing.');
      return;
    }
    setBusy(true);
    try {
      await apiRequest('/api/auth/totp/enable', { body: { code } });
      onEnabledChange(true);
      setRecoveryCodes(setup?.recovery_codes ?? []);
      setSetup(null);
      setCode('');
      setMessage('Two-factor authentication is enabled.');
    } catch (enableError) {
      if (enableError instanceof ApiError && enableError.status === 400) {
        setSetupAttemptsRemaining((remaining) => Math.max(0, remaining - 1));
      }
      if (enableError instanceof ApiError && enableError.status === 410) {
        setSetup(null);
        setRecoveryCodes([]);
      }
      setError(toTOTPSettingsMessage(enableError, 'enable'));
    } finally {
      setBusy(false);
    }
  }

  async function disableTOTP(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setError(null);
    setMessage(null);
    setBusy(true);
    try {
      await apiRequest('/api/auth/totp/disable', { body: { code } });
      onEnabledChange(false);
      setRecoveryCodes([]);
      setCode('');
      setMessage('Two-factor authentication is disabled.');
    } catch (disableError) {
      setError(toTOTPSettingsMessage(disableError, 'disable'));
    } finally {
      setBusy(false);
    }
  }

  return (
    <section className="space-y-4 rounded-md border border-slate-200 p-4">
      <div>
        <h2 className="font-semibold">Two-factor authentication</h2>
        <p className="mt-1 text-sm text-slate-600">
          {enabled
            ? 'An authenticator code is required when you sign in.'
            : 'Add an authenticator code as an extra sign-in step.'}
        </p>
      </div>

      {error && (
        <p className="rounded-md bg-red-50 px-3 py-2 text-sm text-red-700" role="alert">
          {error}
        </p>
      )}
      {message && (
        <p className="rounded-md bg-green-50 px-3 py-2 text-sm text-green-700" role="status">
          {message}
        </p>
      )}

      {!enabled && setup === null && (
        <button
          className="rounded-md bg-slate-900 px-3 py-2 text-sm font-medium text-white disabled:opacity-50"
          type="button"
          onClick={() => void startSetup()}
          disabled={busy}
        >
          {busy ? 'Preparing...' : 'Set up authenticator'}
        </button>
      )}

      {!enabled && setup && (
        <form className="space-y-4" onSubmit={(event) => void enableTOTP(event)}>
          <div className="space-y-2 text-sm">
            <p>In your authenticator app, add an account using this setup key:</p>
            <code className="block break-all rounded bg-slate-100 p-3 font-mono">
              {setup.secret}
            </code>
            <a className="break-all text-blue-700 underline" href={setup.otpauth_url}>
              Open in authenticator app
            </a>
          </div>
          <div>
            <p className="text-sm font-medium">
              Save these one-time recovery codes somewhere safe:
            </p>
            <ul className="mt-2 grid grid-cols-2 gap-2 rounded bg-slate-100 p-3 font-mono text-xs">
              {setup.recovery_codes.map((recoveryCode) => (
                <li key={recoveryCode}>{recoveryCode}</li>
              ))}
            </ul>
          </div>
          <label className="flex items-center gap-2 text-sm">
            <input
              checked={savedCodes}
              type="checkbox"
              onChange={(event) => setSavedCodes(event.target.checked)}
            />
            I have saved these recovery codes
          </label>
          <label className="block text-sm font-medium" htmlFor="totp-enable-code">
            Enter the current authenticator code
          </label>
          <input
            id="totp-enable-code"
            className="w-full rounded-md border border-slate-300 px-3 py-2 font-mono"
            value={code}
            inputMode="numeric"
            autoComplete="one-time-code"
            onChange={(event) => setCode(event.target.value.replace(/\D/g, '').slice(0, 6))}
          />
          <p className="text-xs text-slate-500">
            {setupAttemptsRemaining} {setupAttemptsRemaining === 1 ? 'attempt' : 'attempts'}{' '}
            remaining
          </p>
          <div className="flex gap-2">
            <button
              className="rounded-md bg-slate-900 px-3 py-2 text-sm font-medium text-white disabled:opacity-50"
              type="submit"
              disabled={busy || !savedCodes || code.length !== 6}
            >
              {busy ? 'Verifying...' : 'Confirm and enable'}
            </button>
            <button
              className="rounded-md border border-slate-300 px-3 py-2 text-sm"
              type="button"
              disabled={busy}
              onClick={() => {
                setSetup(null);
                setRecoveryCodes([]);
                setCode('');
              }}
            >
              Cancel
            </button>
          </div>
        </form>
      )}

      {enabled && (
        <form className="space-y-3" onSubmit={(event) => void disableTOTP(event)}>
          {recoveryCodes.length > 0 && (
            <div>
              <p className="text-sm font-medium">Your recovery codes:</p>
              <ul className="mt-2 grid grid-cols-2 gap-2 rounded bg-slate-100 p-3 font-mono text-xs">
                {recoveryCodes.map((recoveryCode) => (
                  <li key={recoveryCode}>{recoveryCode}</li>
                ))}
              </ul>
            </div>
          )}
          <label className="block text-sm font-medium" htmlFor="totp-disable-code">
            Enter an authenticator or unused recovery code to disable
          </label>
          <input
            id="totp-disable-code"
            className="w-full rounded-md border border-slate-300 px-3 py-2 font-mono"
            value={code}
            autoComplete="one-time-code"
            onChange={(event) => setCode(event.target.value.toUpperCase())}
          />
          <button
            className="rounded-md border border-red-300 px-3 py-2 text-sm font-medium text-red-700 disabled:opacity-50"
            type="submit"
            disabled={busy || code.trim() === ''}
          >
            {busy ? 'Disabling...' : 'Disable two-factor authentication'}
          </button>
        </form>
      )}
    </section>
  );
}

function toTOTPSettingsMessage(error: unknown, action: 'setup' | 'enable' | 'disable'): string {
  if (error instanceof ApiError) {
    if (error.status === 400 && action === 'enable') {
      return 'That authenticator code was not accepted. Check your app and try again.';
    }
    if (error.status === 410 && action === 'enable') {
      return 'Setup expired or too many codes were entered. Start setup again.';
    }
    if (error.status === 401 && action === 'disable') {
      return 'That authenticator or recovery code was not accepted.';
    }
    if (error.status === 429 && action === 'disable') {
      return 'Too many incorrect codes. Please wait 15 minutes before trying again.';
    }
    if (error.status === 503) {
      return 'Two-factor authentication is not available right now.';
    }
  }
  if (error instanceof Error) return error.message;
  return `Could not ${action} two-factor authentication. Please try again.`;
}
