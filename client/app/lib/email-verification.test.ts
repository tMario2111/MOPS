import { describe, expect, it } from 'vitest';

import { ApiError } from './api';
import {
  applyVerificationInput,
  emptyVerificationCode,
  formatVerificationTime,
  getRegistrationState,
  normalizeVerificationCode,
  toVerificationMessage,
  VERIFICATION_CODE_LENGTH,
} from './email-verification';

const validRegistrationState = {
  registrationId: '6f1c2f8e-0000-4000-8000-000000000000',
  username: 'test_user',
  email: 'test@example.com',
  opaqueRecord: 'record',
  publicKey: 'public',
  signingPublicKey: 'signing-public',
  wrappedX25519PrivateKey: 'wrapped-x25519',
  wrappedEd25519PrivateKey: 'wrapped-ed25519',
  keyWrapNonce: 'nonce',
  keyWrapSalt: 'salt',
  keyWrapParams: { algorithm: 'argon2id' },
};

describe('emptyVerificationCode', () => {
  it('creates one empty field per code character', () => {
    expect(emptyVerificationCode()).toEqual(Array(VERIFICATION_CODE_LENGTH).fill(''));
  });
});

describe('applyVerificationInput', () => {
  it('updates only the typed field for a single character', () => {
    const next = applyVerificationInput(emptyVerificationCode(), 'A', 2);
    expect(next).toEqual(['', '', 'A', '', '', '', '', '']);
  });

  it('clears a single field on deletion', () => {
    const current = ['A', 'B', 'C', '', '', '', '', ''];
    expect(applyVerificationInput(current, '', 1)).toEqual(['A', '', 'C', '', '', '', '', '']);
  });

  it('replaces the whole code when several characters arrive in one field', () => {
    const current = ['X', 'X', 'X', 'X', 'X', 'X', 'X', 'X'];
    expect(applyVerificationInput(current, 'AB12', 5)).toEqual([
      'A',
      'B',
      '1',
      '2',
      '',
      '',
      '',
      '',
    ]);
  });

  it('replaces the whole code when no field index is given (paste)', () => {
    expect(applyVerificationInput(emptyVerificationCode(), 'AB12CD34')).toEqual([
      'A',
      'B',
      '1',
      '2',
      'C',
      'D',
      '3',
      '4',
    ]);
  });

  it('does not mutate the current state', () => {
    const current = emptyVerificationCode();
    applyVerificationInput(current, 'A', 0);
    expect(current).toEqual(emptyVerificationCode());
  });
});

describe('getRegistrationState', () => {
  it('accepts a complete registration hand-off', () => {
    expect(getRegistrationState(validRegistrationState)).toBe(validRegistrationState);
  });

  it.each([null, undefined, 'state', 42])('rejects non-object state %p', (state) => {
    expect(getRegistrationState(state)).toBeNull();
  });

  it.each(Object.keys(validRegistrationState))('rejects state missing %s', (key) => {
    const incomplete: Record<string, unknown> = { ...validRegistrationState };
    delete incomplete[key];
    expect(getRegistrationState(incomplete)).toBeNull();
  });
});

describe('toVerificationMessage', () => {
  it('explains an incorrect code', () => {
    expect(toVerificationMessage(new ApiError('invalid verification code', 400))).toBe(
      'That code is not correct. Please check your email and try again.'
    );
  });

  it('explains an ended verification session', () => {
    expect(toVerificationMessage(new ApiError('gone', 410))).toBe(
      'This verification session has ended. Please start registration again.'
    );
  });

  it.each([new ApiError('server error', 500), new Error('network'), 'unknown'])(
    'falls back to a generic message for %p',
    (error) => {
      expect(toVerificationMessage(error)).toBe(
        'We could not verify your email right now. Please try again.'
      );
    }
  );
});

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
