import { useState } from 'react';
import type { FormEvent } from 'react';
import { Link, useNavigate } from 'react-router';

import { clearOpaqueSecrets, finishOpaqueLogin, startOpaqueLogin } from '../lib/crypto';

export function meta() {
  return [{ title: 'Log in' }, { name: 'description', content: 'Log in to Chat App' }];
}

export default function Login() {
  const navigate = useNavigate();
  const [isSubmitting, setIsSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);

  async function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setError(null);
    setIsSubmitting(true);

    const formData = new FormData(event.currentTarget);
    const username = getFormValue(formData, 'username').trim();
    const password = getFormValue(formData, 'password');
    const apiBaseUrl = getApiBaseUrl();

    try {
      const login = await runLoginStep('Could not initialize OPAQUE login.', () =>
        startOpaqueLogin(password)
      );
      const startResponse = await fetch(`${apiBaseUrl}/api/auth/login/start`, {
        method: 'POST',
        credentials: 'include',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          username,
          opaque_request: login.request,
        }),
      });

      if (!startResponse.ok) {
        throw new Error(await readApiError(startResponse, 'Could not start login.'));
      }

      const startResult = (await startResponse.json()) as unknown as {
        login_id: string;
        opaque_response: string;
      };
      if (
        typeof startResult.login_id !== 'string' ||
        typeof startResult.opaque_response !== 'string'
      ) {
        throw new Error('The server returned an invalid OPAQUE login response.');
      }

      const opaqueResult = await runLoginStep('Could not finish OPAQUE login.', () =>
        finishOpaqueLogin(login.session, startResult.opaque_response)
      );

      try {
        const finishResponse = await fetch(`${apiBaseUrl}/api/auth/login/finish`, {
          method: 'POST',
          credentials: 'include',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({
            login_id: startResult.login_id,
            opaque_finish: opaqueResult.finish,
          }),
        });

        if (!finishResponse.ok) {
          throw new Error(await readApiError(finishResponse, 'Could not finish login.'));
        }
      } finally {
        clearOpaqueSecrets(opaqueResult);
      }

      void navigate('/account');
    } catch (loginError) {
      setError(
        loginError instanceof Error ? loginError.message : 'Login failed. Please try again.'
      );
    } finally {
      setIsSubmitting(false);
    }
  }

  return (
    <main className="flex min-h-screen items-center justify-center bg-slate-50 px-6 py-12 text-slate-900">
      <form
        className="w-full max-w-sm space-y-6 rounded-xl border border-slate-200 bg-white p-8 shadow-sm"
        onSubmit={(event) => {
          void handleSubmit(event);
        }}
      >
        <div>
          <h1 className="text-2xl font-semibold tracking-tight">Log in</h1>
          <p className="mt-2 text-sm text-slate-600">Use your username and password.</p>
        </div>

        <div className="space-y-4">
          <label className="block text-sm font-medium text-slate-700">
            Username
            <input
              className="mt-2 block w-full rounded-md border border-slate-300 px-3 py-2 text-base font-normal outline-none transition focus:border-slate-500 focus:ring-2 focus:ring-slate-200"
              name="username"
              type="text"
              autoComplete="username"
              required
            />
          </label>

          <label className="block text-sm font-medium text-slate-700">
            Password
            <input
              className="mt-2 block w-full rounded-md border border-slate-300 px-3 py-2 text-base font-normal outline-none transition focus:border-slate-500 focus:ring-2 focus:ring-slate-200"
              name="password"
              type="password"
              autoComplete="current-password"
              required
            />
          </label>
        </div>

        {error && (
          <p className="rounded-md bg-red-50 px-3 py-2 text-sm text-red-700" role="alert">
            {error}
          </p>
        )}

        <button
          className="w-full rounded-md bg-slate-900 px-3 py-2 text-sm font-medium text-white transition hover:bg-slate-700 disabled:cursor-not-allowed disabled:opacity-50"
          type="submit"
          disabled={isSubmitting}
        >
          {isSubmitting ? 'Logging in...' : 'Log in'}
        </button>

        <p className="text-center text-sm text-slate-600">
          Need an account?{' '}
          <Link className="font-medium text-slate-900 underline" to="/">
            Create one
          </Link>
        </p>
      </form>
    </main>
  );
}

async function runLoginStep<T>(message: string, operation: () => Promise<T>): Promise<T> {
  try {
    return await operation();
  } catch (error) {
    if (error instanceof Error && error.message !== '') {
      throw new Error(`${message} ${error.message}`, { cause: error });
    }
    throw new Error(message, { cause: error });
  }
}

function getApiBaseUrl(): string {
  const configured: unknown = import.meta.env.VITE_API_BASE_URL;
  return typeof configured === 'string' && configured.length > 0
    ? configured
    : 'http://localhost:8080';
}

function getFormValue(formData: FormData, name: string): string {
  const value = formData.get(name);
  return typeof value === 'string' ? value : '';
}

async function readApiError(response: Response, fallback: string): Promise<string> {
  try {
    const body = (await response.json()) as { error?: unknown };
    return typeof body.error === 'string' ? body.error : fallback;
  } catch {
    return fallback;
  }
}
