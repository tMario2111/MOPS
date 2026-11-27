// @vitest-environment jsdom

import { act } from 'react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { ApiError } from '../lib/api';
import { cleanup, render, setInputValue, submit } from '../../test/test-utils';
import RegisterVerify, { meta } from './register-verify';

const mocks = vi.hoisted(() => {
  const locationState: { value: unknown } = { value: null };
  return { apiRequest: vi.fn(), navigate: vi.fn(), locationState };
});

vi.mock('../lib/api', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../lib/api')>();
  return { ...actual, apiRequest: mocks.apiRequest };
});

vi.mock('react-router', async () => {
  const React = await import('react');
  return {
    Link: (props: { to: string; children: import('react').ReactNode }) =>
      React.createElement('a', { href: props.to }, props.children),
    useLocation: () => ({ state: mocks.locationState.value }),
    useNavigate: () => mocks.navigate,
  };
});

const registration = {
  registrationId: 'registration-id',
  username: 'new_user',
  email: 'new@example.com',
  opaqueRecord: 'opaque-record',
  publicKey: 'public-key',
  signingPublicKey: 'signing-public-key',
  wrappedX25519PrivateKey: 'wrapped-x25519',
  wrappedEd25519PrivateKey: 'wrapped-ed25519',
  keyWrapNonce: 'nonce',
  keyWrapSalt: 'salt',
  keyWrapParams: 'params',
};

describe('RegisterVerify', () => {
  let container: HTMLElement | undefined;

  beforeEach(() => {
    mocks.apiRequest.mockReset();
    mocks.navigate.mockReset();
    mocks.locationState.value = registration;
  });

  afterEach(() => {
    if (container) cleanup(container);
    container = undefined;
    vi.useRealTimers();
  });

  function mount() {
    container = render(<RegisterVerify />).container;
    return container;
  }

  function codeBoxes(root: HTMLElement) {
    return root.querySelectorAll<HTMLInputElement>(
      '[aria-label^="Email verification code character"]'
    );
  }

  function enterCode(root: HTMLElement, code: string) {
    codeBoxes(root).forEach((box, index) => setInputValue(box, code[index] ?? ''));
  }

  function form(root: HTMLElement) {
    const element = root.querySelector('form');
    if (!element) throw new Error('Missing verification form');
    return element;
  }

  it('exposes page metadata', () => {
    expect(meta()[0]).toEqual({ title: 'Verify your email' });
  });

  it('shows an expired screen without registration state', () => {
    mocks.locationState.value = null;
    const root = mount();
    expect(root.textContent).toContain('Verification expired');
    expect(root.querySelector('a')?.getAttribute('href')).toBe('/');
  });

  it('renders eight alphanumeric code boxes and verifies then finishes registration', async () => {
    const root = mount();
    expect(root.textContent).toContain('Enter the code sent to new@example.com.');
    const boxes = codeBoxes(root);
    expect(boxes).toHaveLength(8);
    expect(boxes[0].getAttribute('inputmode')).toBe('text');

    enterCode(root, 'abcd1234');
    expect([...codeBoxes(root)].map((box) => box.value).join('')).toBe('ABCD1234');

    mocks.apiRequest.mockResolvedValueOnce(undefined).mockResolvedValueOnce(undefined);
    await submit(form(root));

    expect(mocks.apiRequest).toHaveBeenNthCalledWith(1, '/api/auth/register/verify', {
      body: { registration_id: 'registration-id', code: 'ABCD1234' },
    });
    expect(mocks.apiRequest).toHaveBeenNthCalledWith(
      2,
      '/api/auth/register/finish',
      expect.objectContaining({
        body: expect.objectContaining({
          registration_id: 'registration-id',
          opaque_record: 'opaque-record',
        }) as unknown,
      })
    );
    expect(mocks.navigate).toHaveBeenCalledWith('/login', {
      state: { registeredUsername: 'new_user' },
    });
  });

  it('requires all eight characters', async () => {
    const root = mount();
    enterCode(root, 'ABC');
    await submit(form(root));
    expect(root.textContent).toContain('Enter all 8 characters from the verification email.');
    expect(mocks.apiRequest).not.toHaveBeenCalled();
  });

  it('clears an incorrect code and counts down attempts', async () => {
    const root = mount();
    expect(root.textContent).toContain('3 attempts remaining');
    enterCode(root, 'ABCD1234');
    mocks.apiRequest.mockRejectedValueOnce(new ApiError('invalid', 400));
    await submit(form(root));

    expect(root.textContent).toContain(
      'That code is not correct. Please check your email and try again.'
    );
    expect(root.textContent).toContain('2 attempts remaining');
    expect([...codeBoxes(root)].every((box) => box.value === '')).toBe(true);
  });

  it('locks the form when the verification session ends', async () => {
    const root = mount();
    enterCode(root, 'ABCD1234');
    mocks.apiRequest.mockRejectedValueOnce(new ApiError('gone', 410));
    await submit(form(root));

    expect(root.textContent).toContain(
      'This verification session has ended. Please start registration again.'
    );
    expect(root.textContent).toContain('You have used all verification attempts.');
    expect(codeBoxes(root)[0].disabled).toBe(true);
    expect(root.textContent).toContain('Start registration again');
  });

  it('redirects to registration when the code expires', () => {
    vi.useFakeTimers();
    const root = mount();
    expect(root.textContent).toContain('Code expires in 10:00.');

    act(() => {
      vi.advanceTimersByTime(10 * 60 * 1000);
    });

    expect(root.textContent).toContain('Code expired.');
    expect(mocks.navigate).toHaveBeenCalledWith('/', {
      replace: true,
      state: {
        registrationMessage:
          'Your verification code expired. Start registration again to receive a new code.',
      },
    });
  });
});
