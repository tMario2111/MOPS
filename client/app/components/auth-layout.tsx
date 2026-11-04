import type { InputHTMLAttributes, ReactNode } from 'react';

interface IconProps {
  className?: string;
}

const iconProps = {
  viewBox: '0 0 24 24',
  fill: 'none',
  stroke: 'currentColor',
  strokeWidth: 1.8,
  strokeLinecap: 'round',
  strokeLinejoin: 'round',
  'aria-hidden': true,
} as const;

export function ShieldIcon({ className }: IconProps) {
  return (
    <svg className={className} {...iconProps}>
      <path d="M12 3 5 6v5c0 4.4 2.9 8.3 7 10 4.1-1.7 7-5.6 7-10V6l-7-3Z" />
      <path d="m9 12 2 2 4-4" />
    </svg>
  );
}

export function LockIcon({ className }: IconProps) {
  return (
    <svg className={className} {...iconProps}>
      <rect x="5" y="11" width="14" height="9" rx="2" />
      <path d="M8 11V8a4 4 0 0 1 8 0v3" />
    </svg>
  );
}

function ArrowIcon({ className }: IconProps) {
  return (
    <svg className={className} {...iconProps} strokeWidth={2}>
      <path d="M5 12h14M13 6l6 6-6 6" />
    </svg>
  );
}

/**
 * Page chrome shared by the login and registration screens. `dense` is the
 * tighter spacing the register design uses for its taller card.
 */
export function AuthLayout({
  icon,
  title,
  subtitle,
  footerNote,
  dense = false,
  children,
}: {
  icon: ReactNode;
  title: string;
  subtitle: string;
  footerNote: string;
  dense?: boolean;
  children: ReactNode;
}) {
  return (
    <div className="flex min-h-dvh flex-col bg-canvas text-fg">
      <header className="flex items-center justify-between px-6 pt-6 sm:px-10 sm:pt-8">
        <div className="flex items-center gap-4">
          <ShieldIcon className="size-5 text-accent" />
          <span className="text-2xl font-semibold tracking-tight">
            still<span className="text-accent">.</span>
          </span>
        </div>
        <span className="inline-flex items-center gap-1.5 rounded-full border border-line bg-card px-3 py-1.5 font-mono text-[9px] tracking-tight text-accent sm:text-[10px]">
          <LockIcon className="size-3" />
          End-to-end encrypted
        </span>
      </header>

      <main className="flex flex-1 items-center justify-center px-5 py-3 sm:px-4">
        <div
          className={`w-full max-w-[420px] rounded-xl border border-line bg-card shadow-card ${
            dense ? 'px-6 pt-5 pb-5 sm:px-8 sm:pt-7 sm:pb-6' : 'p-6 pt-7 sm:p-8'
          }`}
        >
          <div className="flex flex-col items-center text-center">
            <div
              className={`flex items-center justify-center rounded-[10px] bg-accent-soft text-accent ${
                dense ? 'size-9 sm:size-10' : 'size-12 sm:size-11'
              }`}
            >
              {icon}
            </div>
            <h1
              className={`${dense ? 'mt-3 sm:mt-4' : 'mt-6'} text-2xl font-medium tracking-tight`}
            >
              {title}
            </h1>
            <p className="mt-2 text-[13px] text-muted">{subtitle}</p>
          </div>

          <div className={dense ? 'mt-4 sm:mt-5' : 'mt-6'}>{children}</div>

          <div
            className={`${dense ? 'mt-3 sm:mt-4' : 'mt-6'} ${
              dense ? 'pt-3.5 sm:pt-5' : 'pt-5'
            } flex items-center justify-center gap-2 border-t border-line text-[11px] text-muted`}
          >
            <LockIcon className="size-3 text-accent" />
            {footerNote}
          </div>
        </div>
      </main>

      <footer className="flex items-center justify-between px-6 pt-4 pb-3.5 text-[10px] text-muted sm:px-10 sm:text-[11px]">
        <span className="inline-flex items-center gap-2">
          <ShieldIcon className="size-3.5 text-accent" />
          Privacy, by design.
        </span>
        <span className="flex items-center gap-2 sm:gap-4">
          <span>Help center</span>
          <span aria-hidden="true">·</span>
          <span>Privacy policy</span>
        </span>
      </footer>
    </div>
  );
}

const inputClass =
  'block w-full rounded-lg border border-line bg-field px-3.5 py-2.5 text-[13px] text-fg outline-none transition placeholder:text-muted focus:border-accent focus:ring-2 focus:ring-accent/25 aria-[invalid=true]:border-danger';

export function TextField({
  id,
  label,
  hint,
  error,
  trailingLock = false,
  ...inputProps
}: {
  id: string;
  label: string;
  hint?: string;
  error?: string;
  trailingLock?: boolean;
} & InputHTMLAttributes<HTMLInputElement>) {
  const describedBy = error ? `${id}-error` : hint ? `${id}-hint` : undefined;
  return (
    <div>
      <label className="block text-xs font-medium" htmlFor={id}>
        {label}
      </label>
      <div className="relative mt-2">
        <input
          {...inputProps}
          id={id}
          name={id}
          className={`${inputClass} ${trailingLock ? 'pr-10' : ''}`}
          aria-invalid={error !== undefined}
          aria-describedby={describedBy}
        />
        {trailingLock && (
          <LockIcon className="pointer-events-none absolute top-1/2 right-4 size-3.5 -translate-y-1/2 text-muted" />
        )}
      </div>
      {error ? (
        <p id={`${id}-error`} className="mt-1.5 text-[11px] text-danger" role="alert">
          {error}
        </p>
      ) : (
        hint && (
          <p id={`${id}-hint`} className="mt-1.5 text-[11px] text-muted">
            {hint}
          </p>
        )
      )}
    </div>
  );
}

export function SubmitButton({
  pending,
  idleLabel,
  pendingLabel,
}: {
  pending: boolean;
  idleLabel: string;
  pendingLabel: string;
}) {
  return (
    <button
      className="flex w-full items-center justify-center gap-2 rounded-lg bg-accent px-4 py-3 text-[13px] font-medium text-on-accent transition hover:bg-accent-hover focus-visible:ring-2 focus-visible:ring-accent/40 focus-visible:ring-offset-2 focus-visible:ring-offset-card focus-visible:outline-none disabled:cursor-not-allowed disabled:opacity-60"
      type="submit"
      disabled={pending}
    >
      {pending ? pendingLabel : idleLabel}
      {!pending && <ArrowIcon className="size-3.5" />}
    </button>
  );
}

export function FormAlert({ children }: { children: ReactNode }) {
  return (
    <p className="rounded-lg bg-danger-soft px-3 py-2 text-[13px] text-danger" role="alert">
      {children}
    </p>
  );
}

export function CheckboxField({
  id,
  children,
  error,
  ...inputProps
}: {
  id: string;
  children: ReactNode;
  error?: string;
} & Omit<InputHTMLAttributes<HTMLInputElement>, 'type'>) {
  return (
    <div>
      <label className="flex items-center gap-2.5 text-[11px] text-muted" htmlFor={id}>
        <input
          {...inputProps}
          id={id}
          name={id}
          type="checkbox"
          className="size-3.5 shrink-0 accent-(--accent)"
        />
        <span>{children}</span>
      </label>
      {error && (
        <p className="mt-1.5 text-[11px] text-danger" role="alert">
          {error}
        </p>
      )}
    </div>
  );
}
