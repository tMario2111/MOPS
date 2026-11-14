import { describe, expect, it } from 'vitest';

import {
  formatVerificationTime,
  normalizeVerificationCode,
  VERIFICATION_CODE_LENGTH,
} from './email-verification';

describe('normalizeVerificationCode', () => {
  it('uppercases and removes non-alphanumeric characters', () => {
    expect(normalizeVerificationCode('a-1b!c2d3e4')).toBe('A1BC2D3E');
  });

  it('limits codes to eight characters', () => {
    expect(normalizeVerificationCode('abcdefghi')).toHaveLength(VERIFICATION_CODE_LENGTH);
  });
});

describe('formatVerificationTime', () => {
  it('formats minutes and seconds', () => {
    expect(formatVerificationTime(599)).toBe('9:59');
    expect(formatVerificationTime(0)).toBe('0:00');
  });

  it('does not format negative time', () => {
    expect(formatVerificationTime(-1)).toBe('0:00');
  });
});
