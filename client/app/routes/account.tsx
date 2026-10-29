import { useEffect, useState } from 'react';
import { useNavigate } from 'react-router';

interface CurrentUser {
  id: string;
  username: string;
  email: string;
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
        const response = await fetch(`${getApiBaseUrl()}/api/auth/me`, {
          credentials: 'include',
        });
        if (!response.ok) {
          throw new Error('Your session is missing or expired.');
        }
        const result = (await response.json()) as unknown as CurrentUser;
        if (!cancelled) {
          setUser(result);
        }
      } catch (loadError) {
        if (!cancelled) {
          setError(loadError instanceof Error ? loadError.message : 'Could not load account.');
        }
      }
    }

    void loadCurrentUser();
    return () => {
      cancelled = true;
    };
  }, []);

  async function handleLogout() {
    setIsLoggingOut(true);
    try {
      await fetch(`${getApiBaseUrl()}/api/auth/logout`, {
        method: 'POST',
        credentials: 'include',
      });
    } finally {
      void navigate('/login');
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
        )}

        <button
          className="w-full rounded-md bg-slate-900 px-3 py-2 text-sm font-medium text-white transition hover:bg-slate-700 disabled:opacity-50"
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

function getApiBaseUrl(): string {
  const configured: unknown = import.meta.env.VITE_API_BASE_URL;
  return typeof configured === 'string' && configured.length > 0
    ? configured
    : 'http://localhost:8080';
}
