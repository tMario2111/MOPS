import React, { useState, useRef, useEffect } from 'react';
import { useNavigate, useLocation } from 'react-router';
import { ApiError, apiRequest } from '../lib/api';
import { ShieldIcon } from './auth-layout';

interface SidebarProps {
    currentUser?: {
        username: string;
    } | null;
}

export const Sidebar: React.FC<SidebarProps> = ({ currentUser }) => {
    const navigate = useNavigate();
    const location = useLocation();
    const [showMenu, setShowMenu] = useState(false);
    const [error, setError] = useState<string | null>(null);
    const [isLoggingOut, setIsLoggingOut] = useState(false);
    const menuRef = useRef<HTMLDivElement>(null);

    const isActive = (path: string) => location.pathname === path;

    // Extract initials for the avatar (fallback to 'JL')
    const username = currentUser?.username || 'Jamie Lewis';
    const initials = username.substring(0, 2).toUpperCase();

    useEffect(() => {
        function handleClickOutside(event: MouseEvent) {
            if (menuRef.current && !menuRef.current.contains(event.target as Node)) {
                setShowMenu(false);
            }
        }
        document.addEventListener('mousedown', handleClickOutside);
        return () => document.removeEventListener('mousedown', handleClickOutside);
    }, []);

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
            setShowMenu(false);
        }
    }

    return (
        <aside className="flex w-64 flex-col justify-between border-r border-line bg-card p-6">
            <div className="space-y-8">
                <div className="flex items-center gap-3">
                    <ShieldIcon className="size-5 text-accent" />
                    <span className="text-xl font-semibold tracking-tight">
                        still<span className="text-accent">.</span>
                    </span>
                </div>

                <div className="space-y-1">
                    <p className="px-3 text-[10px] font-medium uppercase tracking-wider text-muted">Your Space</p>
                    <button disabled={true}
                        onClick={() => void navigate('/chats')}
                        className={`flex w-full items-center gap-3 rounded-lg px-3 py-2.5 text-sm font-medium transition ${isActive('/chats') ? 'bg-accent-soft text-accent' : 'text-muted hover:bg-field'
                            }`}
                    >
                        Chats <span className="ml-auto rounded-full bg-field px-2 py-0.5 text-xs">0</span>
                    </button>
                    <button
                        onClick={() => void navigate('/friends')}
                        className={`flex w-full items-center gap-3 rounded-lg px-3 py-2.5 text-sm font-medium transition ${isActive('/friends') ? 'bg-accent-soft text-accent' : 'text-muted hover:bg-field'
                            }`}
                    >
                        Friends
                    </button>
                    <button disabled={true}
                        onClick={() => void navigate('/settings')}
                        className={`flex w-full items-center gap-3 rounded-lg px-3 py-2.5 text-sm font-medium transition ${isActive('/settings') ? 'bg-accent-soft text-accent' : 'text-muted hover:bg-field'
                            }`}
                    >
                        Settings
                    </button>
                </div>
            </div>

            {/* User Footer profile */}
            <div className="border-t border-line pt-4 relative" ref={menuRef}>
                {/* Popup Menu */}
                {showMenu && (
                    <div className="absolute bottom-full left-0 mb-2 w-full rounded-lg border border-line bg-card py-1 shadow-card z-50">
                        <button
                            onClick={() => {
                                void handleLogout();
                            }}
                            disabled={isLoggingOut}
                            className="flex w-full items-center px-4 py-2 text-xs font-medium text-danger transition hover:bg-field disabled:opacity-50"
                        >
                            {isLoggingOut ? 'Logging out...' : 'Log out'}
                        </button>
                    </div>
                )}

                <div className="flex items-center justify-between">
                    <div className="flex items-center gap-3 overflow-hidden">
                        <div className="flex size-8 shrink-0 items-center justify-center rounded-full bg-accent-soft text-xs font-bold text-accent">
                            {initials}
                        </div>
                        <div className="text-xs truncate">
                            <p className="font-medium truncate">{username}</p>
                            <p className="text-muted truncate">Personal account</p>
                        </div>
                    </div>
                    <button
                        onClick={() => setShowMenu((prev) => !prev)}
                        className="flex size-8 shrink-0 items-center justify-center rounded-lg text-muted transition hover:bg-field hover:text-fg"
                        aria-label="Account options"
                    >
                        <span className="text-base tracking-widest font-bold">...</span>
                    </button>
                </div>
            </div>
        </aside>
    );
};