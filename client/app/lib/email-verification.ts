import type { RegistrationVerificationState } from '../routes/home';
import { ApiError } from './api';

export const VERIFICATION_CODE_LENGTH = 8;
export const VERIFICATION_CODE_TTL_SECONDS = 10 * 60;
export const VERIFICATION_MAX_ATTEMPTS = 3;

const VERIFICATION_CODE_PATTERN = /^[A-Z0-9]$/;

export function normalizeVerificationCode(value: string): string {
  return value
    .toUpperCase()
    .split('')
    .filter((character) => VERIFICATION_CODE_PATTERN.test(character))
    .join('')
    .slice(0, VERIFICATION_CODE_LENGTH);
}

export function formatVerificationTime(totalSeconds: number): string {
  const seconds = Math.max(0, Math.floor(totalSeconds));
  const minutes = Math.floor(seconds / 60);
  return `${minutes}:${String(seconds % 60).padStart(2, '0')}`;
}

export function emptyVerificationCode(): string[] {
  return Array.from({ length: VERIFICATION_CODE_LENGTH }, () => '');
}

/**
 * Returns the next per-character code state. A multi-character value (paste or
 * autofill) or a call without an index replaces the whole code; a single
 * character (or deletion) updates only the field at `index`.
 */
export function applyVerificationInput(
  current: readonly string[],
  normalizedValue: string,
  index?: number
): string[] {
  if (index === undefined || normalizedValue.length > 1) {
    return [
      ...normalizedValue.split(''),
      ...Array.from({ length: VERIFICATION_CODE_LENGTH - normalizedValue.length }, () => ''),
    ];
  }
  const next = [...current];
  next[index] = normalizedValue;
  return next;
}

const REQUIRED_REGISTRATION_FIELDS = [
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

/** Validates the router location state handed over by the registration form. */
export function getRegistrationState(state: unknown): RegistrationVerificationState | null {
  if (typeof state !== 'object' || state === null) return null;
  const candidate = state as Partial<RegistrationVerificationState>;
  return REQUIRED_REGISTRATION_FIELDS.every((key) => candidate[key] !== undefined)
    ? (candidate as RegistrationVerificationState)
    : null;
}

export function toVerificationMessage(error: unknown): string {
  if (error instanceof ApiError) {
    if (error.status === 400) {
      return 'That code is not correct. Please check your email and try again.';
    }
    if (error.status === 410) {
      return 'This verification session has ended. Please start registration again.';
    }
  }
  return 'We could not verify your email right now. Please try again.';
}
