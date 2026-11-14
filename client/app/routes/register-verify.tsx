import { useEffect, useRef, useState } from 'react';
import type { FormEvent, ClipboardEvent, KeyboardEvent } from 'react';
import { Link, useLocation, useNavigate } from 'react-router';

import { AuthLayout, FormAlert, ShieldIcon, SubmitButton } from '../components/auth-layout';
import { ApiError, apiRequest } from '../lib/api';
import {
  formatVerificationTime,
  normalizeVerificationCode,
  VERIFICATION_CODE_LENGTH,
  VERIFICATION_MAX_ATTEMPTS,
  VERIFICATION_CODE_TTL_SECONDS,
} from '../lib/email-verification';
import type { RegistrationVerificationState } from './home';

export function meta() {
  return [
    { title: 'Verify your email' },
    { name: 'description', content: 'Verify your email address to create your account' },
  ];
}

export default function RegisterVerify() {
  const location = useLocation();
  const navigate = useNavigate();
  const registration = getRegistrationState(location.state);
  const [characters, setCharacters] = useState<string[]>(() =>
    Array.from({ length: VERIFICATION_CODE_LENGTH }, () => '')
  );
  const [remainingSeconds, setRemainingSeconds] = useState(VERIFICATION_CODE_TTL_SECONDS);
  const [attemptsRemaining, setAttemptsRemaining] = useState(VERIFICATION_MAX_ATTEMPTS);
  const [error, setError] = useState<string | null>(null);
  const [isSubmitting, setIsSubmitting] = useState(false);
  const inputs = useRef<Array<HTMLInputElement | null>>([]);

  useEffect(() => {
    const timer = window.setInterval(() => {
      setRemainingSeconds((remaining) => Math.max(0, remaining - 1));
    }, 1000);
    return () => window.clearInterval(timer);
  }, []);

  useEffect(() => {
    if (remainingSeconds === 0 && registration !== null) {
      void navigate('/', {
        replace: true,
        state: {
          registrationMessage:
            'Your verification code expired. Start registration again to receive a new code.',
        },
      });
    }
  }, [navigate, registration, remainingSeconds]);

  if (registration === null) {
    return (
      <AuthLayout
        icon={<ShieldIcon className="size-5" />}
        title="Verification expired"
        subtitle="Start registration again to receive a new code."
        footerNote="Your conversations stay end-to-end encrypted."
      >
        <Link
          className="flex w-full items-center justify-center rounded-lg bg-accent px-4 py-3 text-[13px] font-medium text-on-accent"
          to="/"
        >
          Return to registration
        </Link>
      </AuthLayout>
    );
  }
  const verifiedRegistration = registration;
  const code = characters.join('');

  const expired = remainingSeconds === 0;
  const locked = attemptsRemaining === 0;

  function updateCode(value: string, focusIndex?: number) {
    const normalized = normalizeVerificationCode(value);
    if (focusIndex === undefined || normalized.length > 1) {
      setCharacters([
        ...normalized.split(''),
        ...Array.from({ length: VERIFICATION_CODE_LENGTH - normalized.length }, () => ''),
      ]);
      return;
    }

    setCharacters((current) => {
      const next = [...current];
      next[focusIndex] = normalized;
      return next;
    });
    if (normalized !== '' && focusIndex < VERIFICATION_CODE_LENGTH - 1) {
      inputs.current[focusIndex + 1]?.focus();
    }
  }

  function handlePaste(event: ClipboardEvent<HTMLInputElement>) {
    event.preventDefault();
    const pastedCode = normalizeVerificationCode(event.clipboardData.getData('text'));
    updateCode(pastedCode);
    inputs.current[Math.min(pastedCode.length, VERIFICATION_CODE_LENGTH - 1)]?.focus();
  }

  function handleKeyDown(event: KeyboardEvent<HTMLInputElement>, index: number) {
    if (event.key === 'Backspace' && characters[index] === '' && index > 0) {
      inputs.current[index - 1]?.focus();
    }
    if (event.key === 'ArrowLeft' && index > 0) inputs.current[index - 1]?.focus();
    if (event.key === 'ArrowRight' && index < VERIFICATION_CODE_LENGTH - 1) {
      inputs.current[index + 1]?.focus();
    }
  }

  async function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setError(null);
    if (expired || locked) {
      setError(
        locked
          ? 'Verification attempts are exhausted. Start registration again.'
          : 'This verification code has expired. Start registration again.'
      );
      return;
    }
    if (code.length !== VERIFICATION_CODE_LENGTH) {
      setError('Enter all 8 characters from the verification email.');
      return;
    }

    setIsSubmitting(true);
    try {
      await apiRequest('/api/auth/register/verify', {
        body: { registration_id: verifiedRegistration.registrationId, code },
      });
      await apiRequest('/api/auth/register/finish', {
        body: {
          registration_id: verifiedRegistration.registrationId,
          opaque_record: verifiedRegistration.opaqueRecord,
          public_key: verifiedRegistration.publicKey,
          signing_public_key: verifiedRegistration.signingPublicKey,
          wrapped_x25519_private_key: verifiedRegistration.wrappedX25519PrivateKey,
          wrapped_ed25519_private_key: verifiedRegistration.wrappedEd25519PrivateKey,
          key_wrap_nonce: verifiedRegistration.keyWrapNonce,
          key_wrap_salt: verifiedRegistration.keyWrapSalt,
          key_wrap_params: verifiedRegistration.keyWrapParams,
        },
      });
      void navigate('/login', { state: { registeredUsername: verifiedRegistration.username } });
    } catch (verificationError) {
      if (verificationError instanceof ApiError && verificationError.status === 400) {
        setAttemptsRemaining((attempts) => Math.max(0, attempts - 1));
        setCharacters(Array.from({ length: VERIFICATION_CODE_LENGTH }, () => ''));
        inputs.current[0]?.focus();
      }
      if (verificationError instanceof ApiError && verificationError.status === 410) {
        setAttemptsRemaining(0);
      }
      setError(toVerificationMessage(verificationError));
    } finally {
      setIsSubmitting(false);
    }
  }

  return (
    <AuthLayout
      icon={<ShieldIcon className="size-5" />}
      title="Check your email"
      subtitle={`Enter the code sent to ${verifiedRegistration.email}.`}
      footerNote="Your conversations stay end-to-end encrypted."
    >
      <form className="space-y-5" noValidate onSubmit={(event) => void handleSubmit(event)}>
        <div>
          <div
            className="flex justify-center gap-1 sm:gap-2"
            role="group"
            aria-label="Email verification code"
          >
            {Array.from({ length: VERIFICATION_CODE_LENGTH }, (_, index) => (
              <input
                key={index}
                ref={(element) => {
                  inputs.current[index] = element;
                }}
                aria-label={`Verification character ${index + 1}`}
                autoComplete={index === 0 ? 'one-time-code' : 'off'}
                className="size-9 rounded-lg border border-line bg-field text-center font-mono text-lg text-fg outline-none transition focus:border-accent focus:ring-2 focus:ring-accent/25 sm:size-10"
                inputMode="text"
                maxLength={1}
                type="text"
                value={characters[index]}
                disabled={locked || expired}
                onChange={(event) => updateCode(event.target.value, index)}
                onKeyDown={(event) => handleKeyDown(event, index)}
                onPaste={handlePaste}
              />
            ))}
          </div>
          <p className="mt-3 text-center text-[11px] text-muted">
            {expired
              ? 'Code expired.'
              : locked
                ? 'You have used all verification attempts.'
                : `Code expires in ${formatVerificationTime(remainingSeconds)}.`}
          </p>
          {!locked && !expired && (
            <p className="mt-1 text-center text-[11px] text-muted">
              {attemptsRemaining} {attemptsRemaining === 1 ? 'attempt' : 'attempts'} remaining
            </p>
          )}
        </div>

        {error && (
          <div className="mt-3">
            <FormAlert>{error}</FormAlert>
          </div>
        )}
        {!locked && !expired && (
          <>
            <SubmitButton
              pending={isSubmitting}
              idleLabel="Verify email"
              pendingLabel="Verifying email..."
            />
          </>
        )}
        {(locked || expired) && (
          <div className="space-y-3">
            <p className="text-center text-[12px] text-muted">
              Start registration again to receive a new verification code.
            </p>
            <Link
              className="flex w-full items-center justify-center rounded-lg bg-accent px-4 py-3 text-[13px] font-medium text-on-accent"
              to="/"
            >
              Start registration again
            </Link>
          </div>
        )}
        {!locked && !expired && (
          <p className="text-center text-[12px] text-muted">
            Didn&apos;t receive it? Check your spam folder or{' '}
            <Link className="font-medium text-accent hover:underline" to="/">
              start again
            </Link>
            .
          </p>
        )}
      </form>
    </AuthLayout>
  );
}

function getRegistrationState(state: unknown): RegistrationVerificationState | null {
  if (typeof state !== 'object' || state === null) return null;
  const candidate = state as Partial<RegistrationVerificationState>;
  const required = [
    'registrationId',
    'username',
    'email',
    'opaqueRecord',
    'publicKey',
    'signingPublicKey',
    'wrappedX25519PrivateKey',
    'wrappedEd25519PrivateKey',
    'keyWrapNonce',
    'keyWrapSalt',
    'keyWrapParams',
  ] as const;
  return required.every((key) => candidate[key] !== undefined)
    ? (candidate as RegistrationVerificationState)
    : null;
}

function toVerificationMessage(error: unknown): string {
  if (error instanceof ApiError) {
    if (error.status === 400) {
      return 'That code is not correct. Please check your email and try again.';
    }
    if (error.status === 410) {
      return 'This verification session has ended. Please start registration again.';
    }
    return 'We could not verify your email right now. Please try again.';
  }
  return 'We could not verify your email right now. Please try again.';
}
