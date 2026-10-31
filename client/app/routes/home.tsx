import { useState } from 'react';
import type { FormEvent } from 'react';
import { Link } from 'react-router';

import {
  clearOpaqueSecrets,
  finishOpaqueRegistration,
  generateAccountKeys,
  startOpaqueRegistration,
} from '../lib/crypto';

export function meta() {
  return [
    { title: 'Create account' },
    { name: 'description', content: 'Create a Chat App account' },
  ];
}

export default function Home() {
  const [isSubmitting, setIsSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [createdUsername, setCreatedUsername] = useState<string | null>(null);

  async function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setError(null);
    setCreatedUsername(null);
    setIsSubmitting(true);

    const formData = new FormData(event.currentTarget);
    const username = getFormValue(formData, 'username').trim();
    const email = getFormValue(formData, 'email').trim();
    const password = getFormValue(formData, 'password');
    const configuredApiBaseUrl: unknown = import.meta.env.VITE_API_BASE_URL;
    const apiBaseUrl =
      typeof configuredApiBaseUrl === 'string' && configuredApiBaseUrl.length > 0
        ? configuredApiBaseUrl
        : 'http://localhost:8080';

    try {
      const opaque = await runRegistrationStep('Could not initialize OPAQUE registration.', () =>
        startOpaqueRegistration(password)
      );
      const startResponse = await fetch(`${apiBaseUrl}/api/auth/register/start`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          username,
          email,
          opaque_request: opaque.request,
        }),
      });

      if (!startResponse.ok) {
        throw new Error(await readApiError(startResponse, 'Could not start registration.'));
      }

      const startResult = (await startResponse.json()) as unknown as {
        registration_id: string;
        opaque_response: string;
      };
      if (
        typeof startResult.registration_id !== 'string' ||
        typeof startResult.opaque_response !== 'string'
      ) {
        throw new Error('The server returned an invalid OPAQUE registration response.');
      }

      const wrappedKeys = await runRegistrationStep('Could not generate account keys.', () =>
        generateAccountKeys(password)
      );
      const opaqueResult = await runRegistrationStep('Could not finish OPAQUE registration.', () =>
        finishOpaqueRegistration(opaque.session, startResult.opaque_response)
      );

      try {
        const finishResponse = await fetch(`${apiBaseUrl}/api/auth/register/finish`, {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({
            registration_id: startResult.registration_id,
            opaque_record: opaqueResult.record,
            public_key: wrappedKeys.publicX25519Key,
            signing_public_key: wrappedKeys.publicEd25519Key,
            wrapped_x25519_private_key: wrappedKeys.wrappedX25519PrivateKey,
            wrapped_ed25519_private_key: wrappedKeys.wrappedEd25519PrivateKey,
            key_wrap_nonce: wrappedKeys.keyWrapNonce,
            key_wrap_salt: wrappedKeys.keyWrapSalt,
            key_wrap_params: wrappedKeys.keyWrapParams,
          }),
        });

        if (!finishResponse.ok) {
          throw new Error(await readApiError(finishResponse, 'Could not create account.'));
        }
      } finally {
        clearOpaqueSecrets(opaqueResult);
      }

      setCreatedUsername(username);
      event.currentTarget.reset();
    } catch (submissionError) {
      setError(
        submissionError instanceof Error
          ? submissionError.message
          : 'Registration failed. Please try again.'
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
        <h1 className="text-2xl font-semibold tracking-tight">Chat App</h1>

        <div className="space-y-4">
          <label className="block text-sm font-medium text-slate-700">
            Username
            <input
              className="mt-2 block w-full rounded-md border border-slate-300 px-3 py-2 text-base font-normal outline-none transition focus:border-slate-500 focus:ring-2 focus:ring-slate-200"
              name="username"
              type="text"
              autoComplete="username"
              minLength={3}
              maxLength={64}
              required
            />
          </label>

          <label className="block text-sm font-medium text-slate-700">
            Email
            <input
              className="mt-2 block w-full rounded-md border border-slate-300 px-3 py-2 text-base font-normal outline-none transition focus:border-slate-500 focus:ring-2 focus:ring-slate-200"
              name="email"
              type="email"
              autoComplete="email"
              required
            />
          </label>

          <label className="block text-sm font-medium text-slate-700">
            Password
            <input
              className="mt-2 block w-full rounded-md border border-slate-300 px-3 py-2 text-base font-normal outline-none transition focus:border-slate-500 focus:ring-2 focus:ring-slate-200"
              name="password"
              type="password"
              autoComplete="new-password"
              required
            />
          </label>
        </div>

        {error && (
          <p className="rounded-md bg-red-50 px-3 py-2 text-sm text-red-700" role="alert">
            {error}
          </p>
        )}

        {createdUsername && (
          <p className="rounded-md bg-green-50 px-3 py-2 text-sm text-green-700" role="status">
            Account created for {createdUsername}.
          </p>
        )}

        <button
          className="w-full rounded-md bg-slate-900 px-3 py-2 text-sm font-medium text-white transition hover:bg-slate-700 disabled:cursor-not-allowed disabled:opacity-50"
          type="submit"
          disabled={isSubmitting}
        >
          {isSubmitting ? 'Creating account...' : 'Create account'}
        </button>

        <p className="text-center text-sm text-slate-600">
          Already have an account?{' '}
          <Link className="font-medium text-slate-900 underline" to="/login">
            Log in
          </Link>
        </p>
      </form>
    </main>
  );
}

async function runRegistrationStep<T>(message: string, operation: () => Promise<T>): Promise<T> {
  try {
    return await operation();
  } catch (error) {
    if (error instanceof Error && error.message !== '') {
      throw new Error(`${message} ${error.message}`, { cause: error });
    }
    throw new Error(message, { cause: error });
  }
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
