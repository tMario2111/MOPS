import { useEffect, useState } from 'react';
import { useNavigate } from 'react-router';

import { ApiError, apiRequest } from '../lib/api';

export interface UserSearchResult {
  id: string;
  username: string;
}

export interface UserSearchAction {
  /** Trailing label for the result row, e.g. "Add" or "Friends". */
  label: string;
  /** Disables the row when the action is not available. */
  disabled?: boolean;
}

interface UserSearchProps {
  /** Id used to associate the search input with its label. */
  inputId: string;
  placeholder?: string;
  /** When provided, each result row becomes a button that calls this. */
  onSelect?: (user: UserSearchResult) => void;
  /** Derives the trailing action label and enabled state for a result row. */
  getAction?: (user: UserSearchResult) => UserSearchAction;
}

interface SearchOutcome {
  query: string;
  users: UserSearchResult[];
  error: string | null;
}

const minQueryLength = 2;
const maxQueryLength = 64;
const searchDelayMs = 300;

function ResultAvatar({ username }: { username: string }) {
  return (
    <div
      className="flex size-9 shrink-0 items-center justify-center rounded-full bg-accent-soft text-xs font-bold text-accent"
      aria-hidden="true"
    >
      {username.substring(0, 2).toUpperCase()}
    </div>
  );
}

/**
 * Debounced username search shared by the "Find people" page and the
 * "Add friend" panel. Results come from the trigram-based search endpoint, so
 * partial usernames match in addition to exact ones.
 */
export function UserSearch({
  inputId,
  placeholder = 'Search usernames...',
  onSelect,
  getAction,
}: UserSearchProps) {
  const navigate = useNavigate();
  const [query, setQuery] = useState('');
  // Outcome of the most recent completed search, keyed by the query it ran for.
  const [lastSearch, setLastSearch] = useState<SearchOutcome | null>(null);

  const normalizedQuery = query.trim().toLowerCase();
  const isQueryTooShort = normalizedQuery.length < minQueryLength;
  const currentSearch = lastSearch?.query === normalizedQuery ? lastSearch : null;

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
    <div>
      <label className="sr-only" htmlFor={inputId}>
        Username
      </label>
      <input
        id={inputId}
        type="search"
        autoComplete="off"
        autoCapitalize="none"
        spellCheck={false}
        maxLength={maxQueryLength}
        placeholder={placeholder}
        value={query}
        onChange={(event) => setQuery(event.currentTarget.value)}
        className="block w-full rounded-lg border border-line bg-field px-3.5 py-2.5 text-sm text-fg transition outline-none placeholder:text-muted focus:border-accent focus:ring-2 focus:ring-accent/25"
      />

      <div className="mt-4" aria-live="polite">
        {isQueryTooShort ? (
          <p className="text-sm text-muted">Type at least {minQueryLength} characters to search.</p>
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
            {currentSearch.users.map((user) => {
              const action = getAction?.(user);
              const row = (
                <>
                  <ResultAvatar username={user.username} />
                  <span className="min-w-0 flex-1 truncate text-sm font-medium">
                    @{user.username}
                  </span>
                  {action && (
                    <span
                      className={`shrink-0 text-xs ${
                        action.disabled ? 'text-muted' : 'font-medium text-accent'
                      }`}
                    >
                      {action.label}
                    </span>
                  )}
                </>
              );

              return (
                <li key={user.id}>
                  {onSelect ? (
                    <button
                      type="button"
                      onClick={() => onSelect(user)}
                      disabled={action?.disabled}
                      className="-mx-2 flex w-full items-center gap-3 rounded-lg px-2 py-3 text-left transition enabled:hover:bg-field disabled:cursor-not-allowed disabled:opacity-60"
                    >
                      {row}
                    </button>
                  ) : (
                    <div className="flex items-center gap-3 py-3">{row}</div>
                  )}
                </li>
              );
            })}
          </ul>
        )}
      </div>
    </div>
  );
}
