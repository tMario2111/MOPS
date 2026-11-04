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

const INVALID_CREDENTIALS = 'Invalid username or password.';

export default function Login() {
  const navigate = useNavigate();
  const location = useLocation();
  const [isSubmitting, setIsSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const registeredUsername = getRegisteredUsername(location.state);

  async function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setError(null);

    const formData = new FormData(event.currentTarget);
    const username = getFormValue(formData, 'username').trim();
    const password = getFormValue(formData, 'password');
    if (username === '' || password === '') {
      setError('Enter your username and password.');
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
        await apiRequest('/api/auth/login/finish', {
          body: { login_id: start.login_id, opaque_finish: opaqueResult.finish },
        });
      } finally {
        clearOpaqueSecrets(opaqueResult);
      }

      void navigate('/account');
    } catch (loginError) {
      setError(toLoginMessage(loginError));
    } finally {
      setIsSubmitting(false);
    }
  }

  return (
    <AuthLayout
      icon={<LockIcon className="size-5" />}
      title="Welcome back"
      subtitle="Sign in to your private space."
      footerNote="Your conversations stay end-to-end encrypted."
    >
      <form
        className="space-y-4"
        onSubmit={(event) => {
          void handleSubmit(event);
        }}
      >
        {registeredUsername && (
          <p className="rounded-lg bg-accent-soft px-3 py-2 text-sm text-accent" role="status">
            Account created for {registeredUsername}. You can sign in now.
          </p>
        )}

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

        {error && <FormAlert>{error}</FormAlert>}

        <div className="pt-2">
          <SubmitButton pending={isSubmitting} idleLabel="Sign in" pendingLabel="Signing in..." />
        </div>

        <p className="pt-1 text-center text-[13px] text-muted">
          New to Still?{' '}
          <Link className="font-medium text-accent hover:underline" to="/">
            Create an account
          </Link>
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
