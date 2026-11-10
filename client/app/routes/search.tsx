import { useEffect } from 'react';
import { useNavigate } from 'react-router';

import { ShieldIcon } from '~/components/auth-layout';
import { UserSearch } from '~/components/user-search';
import { ApiError, apiRequest } from '../lib/api';

export function meta() {
  return [{ title: 'Find people' }, { name: 'description', content: 'Search users by username' }];
}

export default function Search() {
  const navigate = useNavigate();

  // Make sure the visitor is logged in before showing the page.
  useEffect(() => {
    let cancelled = false;

    async function checkSession() {
      try {
        await apiRequest('/api/auth/me', { method: 'GET' });
      } catch (loadError) {
        if (!cancelled && loadError instanceof ApiError && loadError.status === 401) {
          void navigate('/login', { replace: true });
        }
      }
    }

    void checkSession();
    return () => {
      cancelled = true;
    };
  }, [navigate]);

  return (
    <main className="min-h-dvh bg-canvas px-3 py-4 text-fg sm:px-6 sm:py-8">
      <section className="mx-auto w-full max-w-2xl overflow-hidden rounded-xl border border-line bg-card shadow-card">
        <header className="border-b border-line px-4 py-5 sm:px-7">
          <p className="text-[11px] font-medium tracking-wider text-muted uppercase">Find people</p>
          <h1 className="mt-1 text-2xl font-semibold tracking-tight">Search by username</h1>
          <p className="mt-1 text-sm text-muted">
            Look up the people you want to talk to by their exact or partial username.
          </p>
        </header>

        <div className="px-4 py-5 sm:px-7">
          <UserSearch inputId="user-search" placeholder="Search usernames..." />
        </div>

        <footer className="flex items-center gap-2 border-t border-line px-4 py-4 text-[11px] text-muted sm:px-7">
          <ShieldIcon className="size-3.5 text-accent" />
          We never upload your address book or suggest people from your data.
        </footer>
      </section>
    </main>
  );
}
