interface ChatMessage {
  id: number;
  sender: 'Mario' | 'You';
  text: string;
  time: string;
}

const messages: ChatMessage[] = [
  {
    id: 1,
    sender: 'You',
    text: 'Eyo, how was the Smarthack hackathon?',
    time: '10:24 AM',
  },
  {
    id: 2,
    sender: 'Mario',
    text: 'I played Cuphead all night.',
    time: '10:26 AM',
  },
  {
    id: 3,
    sender: 'You',
    text: 'The whole night?',
    time: '10:27 AM',
  },
  {
    id: 4,
    sender: 'Mario',
    text: 'Yeah, I kept saying “one more try.”',
    time: '10:31 AM',
  },
];

export function meta() {
  return [
    { title: 'Conversation' },
    { name: 'description', content: 'Conversation with Mario' },
  ];
}

export default function Chat() {
  return (
    <main className="min-h-dvh bg-canvas px-3 py-4 text-fg sm:px-6 sm:py-8">
      <section className="mx-auto flex h-[calc(100dvh-2rem)] w-full max-w-5xl flex-col overflow-hidden rounded-xl border border-line bg-card shadow-card sm:h-[calc(100dvh-4rem)]">
        <header className="flex items-center gap-3 border-b border-line px-4 py-4 sm:px-7">
          <div
            className="flex size-11 shrink-0 items-center justify-center rounded-full bg-accent-soft text-sm font-semibold text-accent"
            aria-hidden="true"
          >
            M
          </div>
          <div className="min-w-0">
            <h1 className="truncate text-sm font-semibold">Mario</h1>
            <p className="mt-1 flex items-center gap-1.5 text-xs text-accent">
              <span className="size-1.5 rounded-full bg-accent" aria-hidden="true" />
              Online now
            </p>
          </div>
        </header>

        <div className="flex-1 space-y-6 overflow-y-auto px-4 py-6 sm:px-10 sm:py-8">
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
                      M
                    </div>
                  )}
                  <div
                    className={`max-w-[85%] sm:max-w-[70%] ${
                      isOutgoing ? 'text-right' : ''
                    }`}
                  >
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
        </div>

        <footer className="border-t border-line p-4 sm:px-7 sm:py-5">
          <div className="flex items-center gap-2 rounded-xl border border-line bg-field p-2">
            <button
              className="flex size-9 shrink-0 items-center justify-center rounded-lg text-muted"
              type="button"
              aria-label="Attachments are unavailable in this preview"
              disabled
            >
              <PaperclipIcon />
            </button>
            <textarea
              className="min-h-9 flex-1 resize-none bg-transparent px-1 py-2 text-sm text-fg outline-none placeholder:text-muted"
              aria-label="Message"
              placeholder="Write a message..."
              rows={1}
              readOnly
            />
            <button
              className="flex size-9 shrink-0 items-center justify-center rounded-lg bg-accent text-on-accent opacity-60"
              type="button"
              aria-label="Sending is unavailable in this preview"
              disabled
            >
              <SendIcon />
            </button>
          </div>
        </footer>
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
