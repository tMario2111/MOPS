import { useEffect, useRef, useState } from 'react';
import type { FormEvent, KeyboardEvent } from 'react';

interface ChatMessage {
  id: number;
  sender: string;
  text: string;
  time: string;
}

interface Conversation {
  id: string;
  name: string;
  initials: string;
  online?: boolean;
  messages: ChatMessage[];
}

const initialConversations: Conversation[] = [
  {
    id: 'mario',
    name: 'Mario',
    initials: 'M',
    online: true,
    messages: [
      { id: 5, sender: 'You', text: 'Eyo, how was the Smarthack hackathon?', time: '10:24 AM' },
      { id: 6, sender: 'Mario', text: 'I played Cuphead all night.', time: '10:26 AM' },
      { id: 7, sender: 'You', text: 'The whole night?', time: '10:27 AM' },
      { id: 8, sender: 'Mario', text: 'Yeah, I kept saying “one more try.”', time: '10:31 AM' },
    ],
  },
  {
    id: 'ana',
    name: 'Ana Popescu',
    initials: 'AP',
    online: true,
    messages: [
      { id: 1, sender: 'Ana Popescu', text: 'Are we still meeting tomorrow?', time: '1:40 PM' },
      { id: 2, sender: 'You', text: 'See you tomorrow!', time: '1:42 PM' },
    ],
  },
  {
    id: 'alex',
    name: 'Alex Morgan',
    initials: 'AM',
    messages: [{ id: 3, sender: 'Alex Morgan', text: 'Sent you the notes.', time: 'Yesterday' }],
  },
  {
    id: 'sofia',
    name: 'Sofia Ionescu',
    initials: 'SI',
    messages: [{ id: 4, sender: 'Sofia Ionescu', text: 'Thanks for your help!', time: 'Monday' }],
  },
];

export function meta() {
  return [{ title: 'Chats' }, { name: 'description', content: 'Your conversations' }];
}

export default function Chat() {
  const [conversations, setConversations] = useState(initialConversations);
  const [selectedConversationId, setSelectedConversationId] = useState('mario');
  const [mobileView, setMobileView] = useState<'list' | 'conversation'>('list');
  const [draft, setDraft] = useState('');
  const messagesEndRef = useRef<HTMLDivElement>(null);
  const selectedConversation =
    conversations.find((conversation) => conversation.id === selectedConversationId) ??
    conversations[0];
  const messages = selectedConversation.messages;

  useEffect(() => {
    messagesEndRef.current?.scrollIntoView({ behavior: 'smooth', block: 'end' });
  }, [messages]);

  function sendMessage(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const text = draft.trim();
    if (!text) {
      return;
    }

    setConversations((currentConversations) =>
      currentConversations.map((conversation) =>
        conversation.id === selectedConversationId
          ? {
              ...conversation,
              messages: [
                ...conversation.messages,
                {
                  id: Date.now(),
                  sender: 'You',
                  text,
                  time: new Date().toLocaleTimeString([], {
                    hour: 'numeric',
                    minute: '2-digit',
                  }),
                },
              ],
            }
          : conversation
      )
    );
    setDraft('');
  }

  function handleMessageKeyDown(event: KeyboardEvent<HTMLTextAreaElement>) {
    if (event.key === 'Enter' && !event.shiftKey) {
      event.preventDefault();
      event.currentTarget.form?.requestSubmit();
    }
  }

  return (
    <main className="min-h-dvh bg-canvas text-fg sm:px-6 sm:py-8">
      <section className="mx-auto flex h-dvh w-full max-w-6xl flex-col overflow-hidden bg-card sm:h-[calc(100dvh-4rem)] sm:rounded-xl sm:border sm:border-line sm:shadow-card lg:flex-row">
        <aside
          className={`${
            mobileView === 'list' ? 'flex' : 'hidden'
          } min-h-0 flex-1 flex-col border-b border-line lg:flex lg:w-72 lg:flex-none lg:border-r lg:border-b-0`}
        >
          <div className="flex items-center justify-between px-5 py-5">
            <h1 className="text-base font-semibold">Chats</h1>
            <span className="text-xs text-muted">{conversations.length}</span>
          </div>
          <nav className="min-h-0 flex-1" aria-label="Conversations">
            <ul className="flex h-full flex-col gap-1 overflow-y-auto px-2 pb-3">
              {conversations.map((conversation) => {
                const isSelected = conversation.id === selectedConversationId;
                const lastMessage = conversation.messages[conversation.messages.length - 1];
                const preview =
                  lastMessage.sender === 'You' ? `You: ${lastMessage.text}` : lastMessage.text;

                return (
                  <li key={conversation.id}>
                    <button
                      className={`flex w-full items-center gap-3 rounded-lg px-3 py-3 text-left transition hover:bg-field focus-visible:outline-2 focus-visible:outline-accent ${
                        isSelected ? 'bg-accent-soft hover:bg-accent-soft' : ''
                      }`}
                      type="button"
                      aria-pressed={isSelected}
                      onClick={() => {
                        setSelectedConversationId(conversation.id);
                        setMobileView('conversation');
                      }}
                    >
                      <span className="relative flex size-10 shrink-0 items-center justify-center rounded-full bg-field text-xs font-semibold text-accent">
                        {conversation.initials}
                        {conversation.online && (
                          <span
                            className="absolute right-0 bottom-0 size-2.5 rounded-full border-2 border-card bg-accent"
                            aria-label="Online"
                          />
                        )}
                      </span>
                      <span className="min-w-0 flex-1">
                        <span className="flex items-center justify-between gap-2">
                          <span className="truncate text-sm font-medium">{conversation.name}</span>
                          <time className="shrink-0 text-[10px] text-muted">
                            {lastMessage.time}
                          </time>
                        </span>
                        <span className="mt-1 block truncate text-xs text-muted">{preview}</span>
                      </span>
                    </button>
                  </li>
                );
              })}
            </ul>
          </nav>
        </aside>

        <div
          className={`${
            mobileView === 'conversation' ? 'flex' : 'hidden'
          } min-h-0 min-w-0 flex-1 flex-col lg:flex`}
        >
          <header className="flex items-center gap-3 border-b border-line px-4 py-3 sm:px-7 sm:py-4">
            <button
              className="flex size-9 shrink-0 items-center justify-center rounded-lg text-muted hover:bg-field focus-visible:outline-2 focus-visible:outline-accent lg:hidden"
              type="button"
              aria-label="Back to conversations"
              onClick={() => setMobileView('list')}
            >
              <BackIcon />
            </button>
            <div
              className="flex size-10 shrink-0 items-center justify-center rounded-full bg-accent-soft text-sm font-semibold text-accent"
              aria-hidden="true"
            >
              {selectedConversation.initials}
            </div>
            <div className="min-w-0">
              <h2 className="truncate text-sm font-semibold">{selectedConversation.name}</h2>
              {selectedConversation.online && (
                <p className="mt-1 flex items-center gap-1.5 text-xs text-accent">
                  <span className="size-1.5 rounded-full bg-accent" aria-hidden="true" />
                  Online now
                </p>
              )}
            </div>
          </header>

          <div className="min-h-0 flex-1 space-y-6 overflow-y-auto px-4 py-6 sm:px-10 sm:py-8">
            <div className="flex items-center gap-4 text-[11px] text-muted">
              <span className="h-px flex-1 bg-line" />
              Today
              <span className="h-px flex-1 bg-line" />
            </div>

            <ol className="space-y-5" aria-label="Conversation messages">
              {messages.map((message) => {
                const isOutgoing = message.sender === 'You';

                return (
                  <li
                    className={`flex items-end gap-2.5 ${isOutgoing ? 'justify-end' : ''}`}
                    key={message.id}
                  >
                    {!isOutgoing && (
                      <div
                        className="flex size-7 shrink-0 items-center justify-center rounded-full bg-accent-soft text-[10px] font-semibold text-accent"
                        aria-hidden="true"
                      >
                        {selectedConversation.initials}
                      </div>
                    )}
                    <div className={`max-w-[85%] sm:max-w-[70%] ${isOutgoing ? 'text-right' : ''}`}>
                      <p
                        className={`rounded-2xl px-4 py-3 text-left text-sm leading-6 ${
                          isOutgoing
                            ? 'rounded-br-sm bg-accent text-on-accent'
                            : 'rounded-bl-sm bg-field text-fg'
                        }`}
                      >
                        {message.text}
                      </p>
                      <time className="mt-1.5 block px-1 text-[10px] text-muted">
                        {message.time}
                      </time>
                    </div>
                  </li>
                );
              })}
            </ol>
            <div ref={messagesEndRef} />
          </div>

          <footer className="border-t border-line p-4 sm:px-7 sm:py-5">
            <form onSubmit={sendMessage}>
              <div className="flex items-center gap-2 rounded-xl border border-line bg-field p-2">
                <button
                  className="flex size-9 shrink-0 items-center justify-center rounded-lg text-muted"
                  type="button"
                  aria-label="Attachments are not available"
                  disabled
                >
                  <PaperclipIcon />
                </button>
                <textarea
                  className="min-h-9 flex-1 resize-none bg-transparent px-1 py-2 text-sm text-fg outline-none placeholder:text-muted"
                  aria-label="Message"
                  placeholder="Write a message..."
                  rows={1}
                  value={draft}
                  onChange={(event) => setDraft(event.currentTarget.value)}
                  onKeyDown={handleMessageKeyDown}
                />
                <button
                  className="flex size-9 shrink-0 items-center justify-center rounded-lg bg-accent text-on-accent transition hover:bg-accent-hover disabled:cursor-not-allowed disabled:opacity-50"
                  type="submit"
                  aria-label="Send message"
                  disabled={!draft.trim()}
                >
                  <SendIcon />
                </button>
              </div>
            </form>
          </footer>
        </div>
      </section>
    </main>
  );
}

function PaperclipIcon() {
  return (
    <svg
      className="size-[18px]"
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth="1.7"
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden="true"
    >
      <path d="m8 12.5 6.2-6.2a3.2 3.2 0 0 1 4.5 4.5l-8.5 8.5a5 5 0 0 1-7.1-7.1l8.4-8.4" />
    </svg>
  );
}

function BackIcon() {
  return (
    <svg
      className="size-5"
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth="1.7"
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden="true"
    >
      <path d="m15 18-6-6 6-6" />
      <path d="M9 12h12" />
    </svg>
  );
}

function SendIcon() {
  return (
    <svg
      className="size-[18px]"
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth="1.7"
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden="true"
    >
      <path d="m22 2-7 20-4-9-9-4Z" />
      <path d="M22 2 11 13" />
    </svg>
  );
}
