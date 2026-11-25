import { useState } from 'react';
import type { FormEvent } from 'react';
import { Link, useLocation, useNavigate } from 'react-router';

import {
  AuthLayout,
  CheckboxField,
  FormAlert,
  LockIcon,
  SubmitButton,
  TextField,
} from '../components/auth-layout';
import { ApiError, apiRequest, getFormValue } from '../lib/api';
import {
  OpaqueProtocolError,
  clearOpaqueSecrets,
  finishOpaqueLogin,
  startOpaqueLogin,
} from '../lib/crypto';

export function meta() {
  return [{ title: 'Log in' }, { name: 'description', content: 'Log in to Chat App' }];
}

interface LoginStartResult {
  login_id: string;
  opaque_response: string;
}

interface LoginFinishResult {
  user_id?: string;
  requires_totp?: boolean;
  challenge_id?: string;
}

const INVALID_CREDENTIALS = 'Invalid username or password.';
const USERNAME_PATTERN = /^(?=.{4,64}$)(?!_)(?!.*_$)(?!.*__)[a-z0-9_]+$/;

export default function Login() {
  const navigate = useNavigate();
  const location = useLocation();
  const [isSubmitting, setIsSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [totpChallengeID, setTotpChallengeID] = useState<string | null>(null);
  const [totpCode, setTotpCode] = useState('');
  const registeredUsername = getRegisteredUsername(location.state);

  async function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setError(null);

    const formData = new FormData(event.currentTarget);
    const username = getFormValue(formData, 'username').trim();
    const password = getFormValue(formData, 'password');
    if (!USERNAME_PATTERN.test(username) || password === '') {
      setError('Enter your username and password.');
      return;
    }
    if (password.length < 12) {
      setError('Password must be at least 12 characters.');
      return;
    }

    setIsSubmitting(true);
    try {
      const login = await startOpaqueLogin(password);
      const start = await apiRequest<LoginStartResult>('/api/auth/login/start', {
        body: { username, opaque_request: login.request },
      });
      if (typeof start.login_id !== 'string' || typeof start.opaque_response !== 'string') {
        throw new Error('The server returned an invalid login response.');
      }

      // A wrong password makes the client reject the server's OPAQUE response.
      const opaqueResult = await finishOpaqueLogin(login.session, start.opaque_response);
      try {
        const finish = await apiRequest<LoginFinishResult>('/api/auth/login/finish', {
          body: { login_id: start.login_id, opaque_finish: opaqueResult.finish },
        });
        if (finish.requires_totp) {
          if (typeof finish.challenge_id !== 'string') {
            throw new Error('The server returned an invalid two-factor challenge.');
          }
          setTotpChallengeID(finish.challenge_id);
          setError(null);
          return;
        }
      } finally {
        clearOpaqueSecrets(opaqueResult);
      }

      void navigate('/friends');
    } catch (loginError) {
      setError(toLoginMessage(loginError));
    } finally {
      setIsSubmitting(false);
    }
  }

  async function handleTOTPSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setError(null);
    if (totpChallengeID === null || totpCode.trim() === '') {
      setError('Enter your authenticator or recovery code.');
      return;
    }

    setIsSubmitting(true);
    try {
      await apiRequest('/api/auth/login/totp', {
        body: { challenge_id: totpChallengeID, code: totpCode.trim() },
      });
      void navigate('/friends');
    } catch (loginError) {
      if (loginError instanceof ApiError && loginError.status === 410) {
        setTotpChallengeID(null);
        setTotpCode('');
        setError('This sign-in attempt has expired. Please sign in again.');
      } else if (loginError instanceof ApiError && loginError.status === 401) {
        setTotpCode('');
        setError('That code was not accepted. Check it and try again.');
      } else {
        setError(toLoginMessage(loginError));
      }
    } finally {
      setIsSubmitting(false);
    }
  }

  return (
    <AuthLayout
      icon={<LockIcon className="size-5" />}
      title={totpChallengeID ? 'Two-step verification' : 'Welcome back'}
      subtitle={
        totpChallengeID
          ? 'Enter a code from your authenticator app or use a recovery code.'
          : 'Sign in to your private space.'
      }
      footerNote="Your conversations stay end-to-end encrypted."
    >
      <form
        className="space-y-4"
        onSubmit={(event) => {
          if (totpChallengeID) {
            void handleTOTPSubmit(event);
          } else {
            void handleSubmit(event);
          }
        }}
      >
        {!totpChallengeID && registeredUsername && (
          <p className="rounded-lg bg-accent-soft px-3 py-2 text-sm text-accent" role="status">
            Account created for {registeredUsername}. You can sign in now.
          </p>
        )}

        {totpChallengeID ? (
          <TextField
            id="totp-code"
            name="totp-code"
            label="Authenticator or recovery code"
            type="text"
            autoComplete="one-time-code"
            placeholder="Enter your code"
            value={totpCode}
            onChange={(event) => setTotpCode(event.target.value.toUpperCase())}
          />
        ) : (
          <>
            <TextField
              id="username"
              label="Username"
              type="text"
              autoComplete="username"
              placeholder="Enter your username"
              defaultValue={registeredUsername ?? ''}
              required
            />

            <TextField
              id="password"
              label="Password"
              type="password"
              autoComplete="current-password"
              placeholder="Enter your password"
              trailingLock
              required
            />

            <div className="flex items-center justify-between pt-1">
              <CheckboxField id="remember" defaultChecked>
                Remember this device
              </CheckboxField>
              <span className="text-[11px] font-medium text-accent">Forgot password?</span>
            </div>
          </>
        )}

        {error && <FormAlert>{error}</FormAlert>}

        <div className="pt-2">
          <SubmitButton
            pending={isSubmitting}
            idleLabel={totpChallengeID ? 'Verify and sign in' : 'Sign in'}
            pendingLabel={totpChallengeID ? 'Verifying code...' : 'Signing in...'}
          />
        </div>

        <p className="pt-1 text-center text-[13px] text-muted">
          {totpChallengeID ? (
            <button
              className="font-medium text-accent hover:underline"
              type="button"
              onClick={() => {
                setTotpChallengeID(null);
                setTotpCode('');
                setError(null);
              }}
            >
              Restart sign in
            </button>
          ) : (
            <>
              New to Still?{' '}
              <Link className="font-medium text-accent hover:underline" to="/">
                Create an account
              </Link>
            </>
          )}
        </p>
      </form>
    </AuthLayout>
  );
}

function toLoginMessage(error: unknown): string {
  if (error instanceof OpaqueProtocolError) {
    return INVALID_CREDENTIALS;
  }
  if (error instanceof ApiError) {
    return error.status === 401 ? INVALID_CREDENTIALS : error.message;
  }
  return error instanceof Error ? error.message : 'Login failed. Please try again.';
}

function getRegisteredUsername(state: unknown): string | null {
  if (typeof state === 'object' && state !== null && 'registeredUsername' in state) {
    const value: unknown = state.registeredUsername;
    return typeof value === 'string' ? value : null;
  }
  return null;
}
