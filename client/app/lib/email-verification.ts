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
