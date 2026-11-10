import { useEffect, useState } from 'react';
import { useNavigate } from 'react-router';
import { ApiError, apiRequest } from '../lib/api';
import { ShieldIcon } from '~/components/auth-layout';
import { Sidebar } from '~/components/sidebar';

interface CurrentUser {
  id: string;
  username: string;
  email: string;
}

interface Friend {
  id: string;
  username: string;
  public_key: string;
  since: string;
}

interface FriendRequest {
  id: number;
  from_username: string;
  from_user_id: string;
  created_at: string;
  is_incoming: boolean;
}

export function meta() {
  return [{ title: 'Friends' }, { name: 'description', content: 'Your people and connections' }];
}

export default function Friends() {
  const navigate = useNavigate();
  const [currentUser, setCurrentUser] = useState<CurrentUser | null>(null);
  const [activeTab, setActiveTab] = useState<'all' | 'requests'>('all');
  const [friends, setFriends] = useState<Friend[]>([]);
  const [requests, setRequests] = useState<FriendRequest[]>([]);
  const [loading, setLoading] = useState<boolean>(true);

  // Add friend state
  const [showAddModal, setShowAddModal] = useState<boolean>(false);
  const [recipientUsername, setRecipientUsername] = useState<string>('');
  const [actionError, setActionError] = useState<string | null>(null);
  const [actionSuccess, setActionSuccess] = useState<string | null>(null);

  useEffect(() => {
    const fetchData = async () => {
      try {
        const [userRes, friendsRes, requestsRes] = await Promise.all([
          apiRequest<CurrentUser>('/api/auth/me', { method: 'GET' }),
          apiRequest<{ friends: Friend[] }>('/api/friends', { method: 'GET' }),
          apiRequest<{ requests: FriendRequest[] }>('/api/friends/requests', { method: 'GET' }),
        ]);
        setCurrentUser(userRes);
        setFriends(friendsRes.friends || []);
        setRequests(requestsRes.requests || []);
      } catch (err) {
        if (err instanceof ApiError && err.status === 401) {
          void navigate('/login', { replace: true });
          return;
        }
        console.error('Failed to load friends data', err);
      } finally {
        setLoading(false);
      }
    };

    void fetchData();
  }, [navigate]);

  const handleSendRequest = async (e: React.FormEvent) => {
    e.preventDefault();
    setActionError(null);
    setActionSuccess(null);
    try {
      await apiRequest('/api/friends/request', {
        method: 'POST',
        body: { recipient_username: recipientUsername },
      });
      setActionSuccess('Friend request sent successfully!');
      setRecipientUsername('');

      // Re-fetch data upon sending request
      const [friendsRes, requestsRes] = await Promise.all([
        apiRequest<{ friends: Friend[] }>('/api/friends', { method: 'GET' }),
        apiRequest<{ requests: FriendRequest[] }>('/api/friends/requests', { method: 'GET' }),
      ]);
      setFriends(friendsRes.friends || []);
      setRequests(requestsRes.requests || []);
    } catch (err) {
      setActionError(err instanceof Error ? err.message : 'Failed to send request');
    }
  };

  const handleRespondRequest = async (requestId: number, action: 'accept' | 'decline') => {
    try {
      await apiRequest(`/api/friends/requests/${requestId}`, {
        method: 'PUT',
        body: { action },
      });
      const [friendsRes, requestsRes] = await Promise.all([
        apiRequest<{ friends: Friend[] }>('/api/friends', { method: 'GET' }),
        apiRequest<{ requests: FriendRequest[] }>('/api/friends/requests', { method: 'GET' }),
      ]);
      setFriends(friendsRes.friends || []);
      setRequests(requestsRes.requests || []);
    } catch (err) {
      alert(err instanceof Error ? err.message : 'Failed to process request');
    }
  };

  const incomingRequests = requests.filter((r) => r.is_incoming);

  return (
    <div className="flex h-screen w-screen overflow-hidden bg-canvas text-fg">
      {/* Left Navigation Sidebar */}
      <Sidebar currentUser={currentUser} />

      {/* Main View Wrapper */}
      <div className="flex flex-1 flex-col overflow-hidden">
        {/* Full-Width Top Header (Extends all the way across to the right edge) */}
        <header className="flex items-center justify-between border-b border-line px-10 py-6">
          <div>
            <p className="text-[11px] font-medium uppercase tracking-wider text-muted">
              Your private space
            </p>
            <h1 className="mt-1 text-3xl font-semibold tracking-tight">Better, together.</h1>
            <p className="mt-1 text-sm text-muted">
              Your people. Your conversations. Always private.
            </p>
          </div>

          <div className="flex items-center gap-4">
            <span className="inline-flex items-center gap-1.5 rounded-full border border-line bg-card px-3 py-1.5 font-mono text-[11px] text-accent">
              🔒 End-to-end encrypted
            </span>
            <button
              onClick={() => setShowAddModal(!showAddModal)}
              className="rounded-lg bg-accent px-4 py-2 text-sm font-medium text-on-accent transition hover:bg-accent-hover"
            >
              + Add friend
            </button>
          </div>
        </header>

        {/* Content Area split into Main List and Right Panel */}
        <div className="flex flex-1 overflow-hidden">
          {/* Main Friends List Pane */}
          <main className="flex flex-1 flex-col overflow-y-auto px-10 py-8 space-y-6">
            {/* Add Friend Form */}
            {showAddModal && (
              <form
                onSubmit={(e) => void handleSendRequest(e)}
                className="rounded-xl border border-line bg-card p-4 shadow-sm"
              >
                <h3 className="text-sm font-medium">Send Friend Request</h3>
                <div className="mt-3 flex gap-3">
                  <input
                    type="text"
                    placeholder="Recipient username"
                    value={recipientUsername}
                    onChange={(e) => setRecipientUsername(e.target.value)}
                    className="flex-1 rounded-lg border border-line bg-field px-3.5 py-2 text-sm outline-none focus:border-accent"
                  />
                  <button
                    type="submit"
                    className="rounded-lg bg-accent px-4 py-2 text-sm font-medium text-on-accent"
                  >
                    Send
                  </button>
                </div>
                {actionError && <p className="mt-2 text-xs text-danger">{actionError}</p>}
                {actionSuccess && <p className="mt-2 text-xs text-accent">{actionSuccess}</p>}
              </form>
            )}

            {/* Tabs Bar */}
            <div className="flex gap-6 border-b border-line">
              <button
                onClick={() => setActiveTab('all')}
                className={`pb-3 text-sm font-medium transition border-b-2 ${
                  activeTab === 'all' ? 'border-accent text-fg' : 'border-transparent text-muted'
                }`}
              >
                All friends{' '}
                <span className="ml-1.5 rounded-full bg-field px-2 py-0.5 text-xs">
                  {friends.length}
                </span>
              </button>
              <button
                onClick={() => setActiveTab('requests')}
                className={`pb-3 text-sm font-medium transition border-b-2 ${
                  activeTab === 'requests'
                    ? 'border-accent text-fg'
                    : 'border-transparent text-muted'
                }`}
              >
                Requests{' '}
                <span className="ml-1.5 rounded-full bg-field px-2 py-0.5 text-xs">
                  {requests.length}
                </span>
              </button>
            </div>

            {/* Table Content */}
            <div className="flex-1">
              {loading ? (
                <p className="text-sm text-muted">Loading...</p>
              ) : activeTab === 'all' ? (
                <div className="overflow-hidden rounded-xl border border-line bg-card shadow-card">
                  <table className="w-full text-left text-sm">
                    <thead className="border-b border-line text-[11px] uppercase tracking-wider text-muted">
                      <tr>
                        <th className="px-6 py-4 font-medium">Name</th>
                        <th className="px-6 py-4 font-medium">Status</th>
                        <th className="px-6 py-4 text-right font-medium">Actions</th>
                      </tr>
                    </thead>
                    <tbody className="divide-y divide-line">
                      {friends.length === 0 ? (
                        <tr>
                          <td colSpan={3} className="px-6 py-8 text-center text-muted">
                            No friends added yet.
                          </td>
                        </tr>
                      ) : (
                        friends.map((friend) => (
                          <tr key={friend.id} className="transition hover:bg-field/50">
                            <td className="flex items-center gap-3 px-6 py-4">
                              <div className="flex size-9 items-center justify-center rounded-full bg-accent-soft text-xs font-bold text-accent">
                                {friend.username.substring(0, 2).toUpperCase()}
                              </div>
                              <div>
                                <p className="font-medium">{friend.username}</p>
                                <p className="text-xs text-muted">@{friend.username}</p>
                              </div>
                            </td>
                            <td className="px-6 py-4">
                              <span className="inline-block size-2 rounded-full bg-emerald-500 mr-2"></span>
                              <span className="text-xs text-muted">Online</span>
                            </td>
                            <td className="px-6 py-4 text-right">
                              <button className="rounded-lg border border-line bg-field px-3 py-1.5 text-xs font-medium transition hover:bg-card">
                                Message
                              </button>
                            </td>
                          </tr>
                        ))
                      )}
                    </tbody>
                  </table>
                </div>
              ) : (
                <div className="space-y-3">
                  {requests.length === 0 ? (
                    <p className="text-sm text-muted">No pending requests.</p>
                  ) : (
                    requests.map((req) => (
                      <div
                        key={req.id}
                        className="flex items-center justify-between rounded-xl border border-line bg-card p-4"
                      >
                        <div>
                          <p className="font-medium">{req.from_username}</p>
                          <p className="text-xs text-muted">
                            {req.is_incoming ? 'Wants to connect with you' : 'Outgoing request'}
                          </p>
                        </div>
                        {req.is_incoming && (
                          <div className="flex gap-2">
                            <button
                              onClick={() => void handleRespondRequest(req.id, 'accept')}
                              className="rounded-lg bg-accent px-3 py-1.5 text-xs font-medium text-on-accent"
                            >
                              Accept
                            </button>
                            <button
                              onClick={() => void handleRespondRequest(req.id, 'decline')}
                              className="rounded-lg border border-line bg-field px-3 py-1.5 text-xs font-medium"
                            >
                              Decline
                            </button>
                          </div>
                        )}
                      </div>
                    ))
                  )}
                </div>
              )}
            </div>
          </main>

          {/* Right Sidebar Panel (Docked on the right under the header) */}
          <aside className="w-80 flex-col gap-6 border-l border-line bg-card p-6 overflow-y-auto hidden lg:flex">
            {/* Friend Requests Widget */}
            <div className="rounded-xl border border-line bg-field p-4">
              <div className="flex items-center justify-between mb-2">
                <h3 className="text-sm font-semibold">Friend requests</h3>
                <span className="rounded-full bg-card px-2 py-0.5 text-xs">
                  {incomingRequests.length}
                </span>
              </div>
              <p className="text-xs text-muted mb-4">A couple of new connections.</p>

              {incomingRequests.length === 0 ? (
                <p className="text-xs text-muted">No pending requests right now.</p>
              ) : (
                <div className="space-y-4">
                  {incomingRequests.map((req) => (
                    <div key={req.id} className="border-b border-line pb-3 last:border-0 last:pb-0">
                      <div className="flex items-center gap-3 mb-3">
                        <div className="flex size-8 items-center justify-center rounded-full bg-accent-soft text-xs font-bold text-accent">
                          {req.from_username.substring(0, 2).toUpperCase()}
                        </div>
                        <div>
                          <p className="text-xs font-semibold">{req.from_username}</p>
                          <p className="text-[10px] text-muted">Wants to connect with you</p>
                        </div>
                      </div>
                      <div className="flex gap-2">
                        <button
                          onClick={() => void handleRespondRequest(req.id, 'accept')}
                          className="flex-1 rounded-lg bg-accent py-1.5 text-xs font-medium text-on-accent"
                        >
                          Accept
                        </button>
                        <button
                          onClick={() => void handleRespondRequest(req.id, 'decline')}
                          className="flex-1 rounded-lg border border-line bg-card py-1.5 text-xs font-medium text-fg"
                        >
                          Decline
                        </button>
                      </div>
                    </div>
                  ))}
                </div>
              )}
            </div>

            {/* Privacy Note Box */}
            <div className="rounded-xl border border-line bg-accent-soft/30 p-4 text-xs text-accent">
              <p className="font-semibold mb-1 flex items-center gap-1.5">
                <ShieldIcon className="size-5 text-accent" />
                <span>Connection, not collection.</span>{' '}
              </p>
              <p className="text-[11px] leading-relaxed opacity-90">
                We don’t upload your address book or suggest people from your data. Add friends on
                your own terms.
              </p>
            </div>
          </aside>
        </div>
      </div>
    </div>
  );
}
