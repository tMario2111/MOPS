import { useEffect, useState } from 'react';
import { useNavigate } from 'react-router';

import { ShieldIcon } from '~/components/auth-layout';
import { ApiError, apiRequest } from '../lib/api';

interface UserSearchResult {
  id: string;
  username: string;
}

interface SearchOutcome {
  query: string;
  users: UserSearchResult[];
  error: string | null;
}

const minQueryLength = 2;
const maxQueryLength = 64;
const searchDelayMs = 300;

export function meta() {
  return [{ title: 'Find people' }, { name: 'description', content: 'Search users by username' }];
}

export default function Search() {
  const navigate = useNavigate();
  const [query, setQuery] = useState('');
  // Outcome of the most recent completed search, keyed by the query it ran for.
  const [lastSearch, setLastSearch] = useState<SearchOutcome | null>(null);

  const normalizedQuery = query.trim().toLowerCase();
  const isQueryTooShort = normalizedQuery.length < minQueryLength;
  const currentSearch = lastSearch?.query === normalizedQuery ? lastSearch : null;

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

  // Search as the user types, waiting until they pause.
  useEffect(() => {
    if (normalizedQuery.length < minQueryLength) {
      return;
    }

    let cancelled = false;

    const timeout = setTimeout(() => {
      async function searchUsers() {
        try {
          const result = await apiRequest<{ users: UserSearchResult[] }>(
            `/api/users/search?q=${encodeURIComponent(normalizedQuery)}`,
            { method: 'GET' }
          );
          if (!cancelled) {
            setLastSearch({ query: normalizedQuery, users: result.users, error: null });
          }
        } catch (searchError) {
          if (cancelled) {
            return;
          }
          if (searchError instanceof ApiError && searchError.status === 401) {
            void navigate('/login', { replace: true });
            return;
          }
          setLastSearch({
            query: normalizedQuery,
            users: [],
            error: searchError instanceof Error ? searchError.message : 'Could not search users.',
          });
        }
      }

      void searchUsers();
    }, searchDelayMs);

    return () => {
      cancelled = true;
      clearTimeout(timeout);
    };
  }, [normalizedQuery, navigate]);

  return (
    <main className="min-h-dvh bg-canvas px-3 py-4 text-fg sm:px-6 sm:py-8">
      <section className="mx-auto w-full max-w-2xl overflow-hidden rounded-xl border border-line bg-card shadow-card">
        <header className="border-b border-line px-4 py-5 sm:px-7">
          <p className="text-[11px] font-medium tracking-wider text-muted uppercase">Find people</p>
          <h1 className="mt-1 text-2xl font-semibold tracking-tight">Search by username</h1>
          <p className="mt-1 text-sm text-muted">
            Look up the people you want to talk to by their exact or partial username.
          </p>

          <div className="mt-5">
            <label className="sr-only" htmlFor="user-search">
              Username
            </label>
            <input
              id="user-search"
              type="search"
              autoComplete="off"
              autoCapitalize="none"
              spellCheck={false}
              maxLength={maxQueryLength}
              placeholder="Search usernames..."
              value={query}
              onChange={(event) => setQuery(event.currentTarget.value)}
              className="block w-full rounded-lg border border-line bg-field px-3.5 py-2.5 text-sm text-fg transition outline-none placeholder:text-muted focus:border-accent focus:ring-2 focus:ring-accent/25"
            />
          </div>
        </header>

        <div className="px-4 py-5 sm:px-7" aria-live="polite">
          {isQueryTooShort ? (
            <p className="text-sm text-muted">
              Type at least {minQueryLength} characters to search.
            </p>
          ) : currentSearch === null ? (
            <p className="text-sm text-muted">Searching...</p>
          ) : currentSearch.error ? (
            <p className="rounded-lg bg-danger-soft px-3.5 py-2.5 text-xs text-danger" role="alert">
              {currentSearch.error}
            </p>
          ) : currentSearch.users.length === 0 ? (
            <p className="text-sm text-muted">No users match “{currentSearch.query}”.</p>
          ) : (
            <ul className="divide-y divide-line" aria-label="Search results">
              {currentSearch.users.map((user) => (
                <li className="flex items-center gap-3 py-3" key={user.id}>
                  <div
                    className="flex size-9 shrink-0 items-center justify-center rounded-full bg-accent-soft text-xs font-bold text-accent"
                    aria-hidden="true"
                  >
                    {user.username.substring(0, 2).toUpperCase()}
                  </div>
                  <p className="truncate text-sm font-medium">@{user.username}</p>
                </li>
              ))}
            </ul>
          )}
        </div>

        <footer className="flex items-center gap-2 border-t border-line px-4 py-4 text-[11px] text-muted sm:px-7">
          <ShieldIcon className="size-3.5 text-accent" />
          We never upload your address book or suggest people from your data.
        </footer>
      </section>
    </main>
  );
}
