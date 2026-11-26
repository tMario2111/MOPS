// @vitest-environment jsdom

import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { ApiError } from '../lib/api';
import { cleanup, click, render, setInputValue, settle, submit } from '../../test/test-utils';
import Login from './login';

const mocks = vi.hoisted(() => ({
  apiRequest: vi.fn(),
  navigate: vi.fn(),
  locationState: { value: null as { registeredUsername?: string } | null },
  startOpaqueLogin: vi.fn(),
  finishOpaqueLogin: vi.fn(),
  clearOpaqueSecrets: vi.fn(),
}));

vi.mock('../lib/api', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../lib/api')>();
  return { ...actual, apiRequest: mocks.apiRequest };
});

vi.mock('../lib/crypto', () => ({
  OpaqueProtocolError: class OpaqueProtocolError extends Error {},
  startOpaqueLogin: mocks.startOpaqueLogin,
  finishOpaqueLogin: mocks.finishOpaqueLogin,
  clearOpaqueSecrets: mocks.clearOpaqueSecrets,
}));

vi.mock('react-router', async () => {
  const React = await import('react');
  return {
    Link: (props: { to: string; children: import('react').ReactNode }) =>
      React.createElement('a', { href: props.to }, props.children),
    useLocation: () => ({ state: mocks.locationState.value }),
    useNavigate: () => mocks.navigate,
  };
});

describe('Login TOTP flow', () => {
  let container: HTMLElement | undefined;

  beforeEach(() => {
    mocks.apiRequest.mockReset();
    mocks.navigate.mockReset();
    mocks.startOpaqueLogin
      .mockReset()
      .mockResolvedValue({ request: 'opaque-request', session: {} });
    mocks.finishOpaqueLogin.mockReset().mockResolvedValue({ finish: 'opaque-finish' });
    mocks.clearOpaqueSecrets.mockReset();
    mocks.locationState.value = null;
  });

  afterEach(() => {
    if (container) cleanup(container);
    container = undefined;
  });

  function mountLogin() {
    const result = render(<Login />);
    container = result.container;
    return container;
  }

  function button(label: string): HTMLButtonElement {
    const match = [...(container?.querySelectorAll('button') ?? [])].find(
      (item) => item.textContent?.trim() === label
    );
    if (!(match instanceof HTMLButtonElement)) throw new Error(`Missing button: ${label}`);
    return match;
  }

  async function enterTOTPChallenge() {
    const root = mountLogin();
    const username = root.querySelector<HTMLInputElement>('#username');
    const password = root.querySelector<HTMLInputElement>('#password');
    const form = root.querySelector('form');
    if (!username || !password || !form) throw new Error('Missing login form');
    setInputValue(username, 'test_user');
    setInputValue(password, 'correct horse battery staple');

    mocks.apiRequest
      .mockResolvedValueOnce({ login_id: 'login-id', opaque_response: 'opaque-response' })
      .mockResolvedValueOnce({ requires_totp: true, challenge_id: 'challenge-id' });
    await submit(form);
    await settle();
    return root;
  }

  it('validates credentials and displays the registration handoff message', async () => {
    mocks.locationState.value = { registeredUsername: 'new_user' };
    const root = mountLogin();
    expect(root.textContent).toContain('Account created for new_user.');

    const form = root.querySelector('form');
    if (!form) throw new Error('Missing login form');
    const username = root.querySelector<HTMLInputElement>('#username');
    const password = root.querySelector<HTMLInputElement>('#password');
    if (!username || !password) throw new Error('Missing credential fields');
    setInputValue(username, 'bad');
    setInputValue(password, 'correct horse battery staple');
    await submit(form);
    expect(root.textContent).toContain('Enter your username and password.');

    setInputValue(username, 'test_user');
    setInputValue(password, 'short');
    await submit(form);
    expect(root.textContent).toContain('Password must be at least 12 characters.');
    expect(mocks.startOpaqueLogin).not.toHaveBeenCalled();
  });

  it('submits a six-digit numeric code and navigates after successful login', async () => {
    const root = await enterTOTPChallenge();
    expect(root.textContent).toContain('Two-step verification');
    const inputs = root.querySelectorAll<HTMLInputElement>(
      '[aria-label^="Authenticator code character"]'
    );
    expect(inputs).toHaveLength(6);
    expect(inputs[0].getAttribute('inputmode')).toBe('numeric');

    inputs.forEach((input, index) => setInputValue(input, String(index + 1)));
    const form = root.querySelector('form');
    if (!form) throw new Error('Missing TOTP form');
    mocks.apiRequest.mockResolvedValueOnce(undefined);
    await submit(form);

    expect(mocks.apiRequest).toHaveBeenLastCalledWith('/api/auth/login/totp', {
      body: { challenge_id: 'challenge-id', code: '123456' },
    });
    expect(mocks.navigate).toHaveBeenCalledWith('/friends');
    expect(mocks.clearOpaqueSecrets).toHaveBeenCalledWith({ finish: 'opaque-finish' });
  });

  it('allows recovery-code login and clears a rejected code', async () => {
    const root = await enterTOTPChallenge();
    click(button('Use a recovery code'));
    const recovery = root.querySelector<HTMLInputElement>('#totp-recovery-code');
    const form = root.querySelector('form');
    if (!recovery || !form) throw new Error('Missing recovery-code input');
    setInputValue(recovery, 'abcd-1234');
    expect(recovery.value).toBe('ABCD-1234');

    mocks.apiRequest.mockRejectedValueOnce(new ApiError('invalid', 401));
    await submit(form);
    expect(root.textContent).toContain('That code was not accepted. Check it and try again.');
    expect(recovery.value).toBe('');
  });

  it.each([
    [410, 'This sign-in attempt has expired. Please sign in again.', 'Welcome back'],
    [
      429,
      'Too many incorrect codes. For your security, sign in again in 15 minutes.',
      'Welcome back',
    ],
  ])('ends the challenge for HTTP %i', async (status, message, title) => {
    const root = await enterTOTPChallenge();
    const inputs = root.querySelectorAll<HTMLInputElement>(
      '[aria-label^="Authenticator code character"]'
    );
    inputs.forEach((input, index) => setInputValue(input, String(index + 1)));
    const form = root.querySelector('form');
    if (!form) throw new Error('Missing TOTP form');
    mocks.apiRequest.mockRejectedValueOnce(new ApiError('challenge ended', status));
    await submit(form);

    expect(root.textContent).toContain(message);
    expect(root.querySelector('h1')?.textContent).toBe(title);
  });

  it('allows restarting the login challenge and reports invalid credentials', async () => {
    const root = await enterTOTPChallenge();
    click(button('Restart sign in'));
    expect(root.querySelector('#username')).not.toBeNull();
    expect(root.textContent).not.toContain('Two-step verification');

    mocks.startOpaqueLogin.mockRejectedValueOnce(new Error('opaque failure'));
    const form = root.querySelector('form');
    const username = root.querySelector<HTMLInputElement>('#username');
    const password = root.querySelector<HTMLInputElement>('#password');
    if (!form || !username || !password) throw new Error('Missing login controls');
    setInputValue(username, 'test_user');
    setInputValue(password, 'correct horse battery staple');
    await submit(form);
    expect(root.textContent).toContain('opaque failure');
  });
});
