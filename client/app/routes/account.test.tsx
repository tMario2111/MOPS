// @vitest-environment jsdom

import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { ApiError } from '../lib/api';
import { cleanup, click, render, setInputValue, settle, submit } from '../../test/test-utils';
import Account from './account';

const { apiRequestMock, navigateMock } = vi.hoisted(() => ({
  apiRequestMock: vi.fn(),
  navigateMock: vi.fn(),
}));

vi.mock('../lib/api', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../lib/api')>();
  return { ...actual, apiRequest: apiRequestMock };
});

vi.mock('react-router', () => ({ useNavigate: () => navigateMock }));

const user = {
  id: 'user-id',
  username: 'test_user',
  email: 'test@example.com',
  totp_enabled: false,
};

const setup = {
  secret: 'JBSWY3DPEHPK3PXP',
  otpauth_url: 'otpauth://totp/Still:test_user?secret=JBSWY3DPEHPK3PXP',
  recovery_codes: ['AAAA-BBBB', 'CCCC-DDDD'],
};

describe('Account TOTP settings', () => {
  let container: HTMLElement | undefined;

  beforeEach(() => {
    apiRequestMock.mockReset();
    navigateMock.mockReset();
  });

  afterEach(() => {
    if (container) cleanup(container);
    container = undefined;
  });

  async function renderAccount(initialUser = user) {
    apiRequestMock.mockResolvedValueOnce(initialUser);
    const result = render(<Account />);
    container = result.container;
    await settle();
    return container;
  }

  function button(label: string): HTMLButtonElement {
    const match = [...(container?.querySelectorAll('button') ?? [])].find(
      (item) => item.textContent?.trim() === label
    );
    if (!(match instanceof HTMLButtonElement)) throw new Error(`Missing button: ${label}`);
    return match;
  }

  it('starts enrollment, requires saved recovery codes, then enables TOTP', async () => {
    await renderAccount();
    apiRequestMock.mockResolvedValueOnce(setup);
    click(button('Set up authenticator'));
    await settle();

    expect(container?.textContent).toContain(setup.secret);
    expect(container?.textContent).toContain('AAAA-BBBB');
    expect(button('Confirm and enable').disabled).toBe(true);

    const code = container?.querySelector<HTMLInputElement>('#totp-enable-code');
    if (!code) throw new Error('Missing enrollment code field');
    setInputValue(code, '12x34567');
    expect(code.value).toBe('123456');
    const form = code.closest('form');
    if (!form) throw new Error('Missing enrollment form');

    await submit(form);
    expect(container?.textContent).toContain('Save your recovery codes before continuing.');
    expect(apiRequestMock).toHaveBeenCalledTimes(2);

    const savedCheckbox = [
      ...(container?.querySelectorAll<HTMLInputElement>('input[type="checkbox"]') ?? []),
    ][0];
    if (!savedCheckbox) throw new Error('Missing recovery-code confirmation checkbox');
    click(savedCheckbox);
    expect(button('Confirm and enable').disabled).toBe(false);
    apiRequestMock.mockResolvedValueOnce(undefined);
    await submit(form);

    expect(apiRequestMock).toHaveBeenLastCalledWith('/api/auth/totp/enable', {
      body: { code: '123456' },
    });
    expect(container?.textContent).toContain('Two-factor authentication is enabled.');
    expect(container?.textContent).toContain('An authenticator code is required when you sign in.');
  });

  it('shows an incorrect setup code and decrements remaining attempts', async () => {
    await renderAccount();
    apiRequestMock.mockResolvedValueOnce(setup);
    click(button('Set up authenticator'));
    await settle();

    const checkbox = container?.querySelector<HTMLInputElement>('input[type="checkbox"]');
    const code = container?.querySelector<HTMLInputElement>('#totp-enable-code');
    const form = code?.closest('form');
    if (!checkbox || !code || !form) throw new Error('Missing setup form controls');
    click(checkbox);
    setInputValue(code, '123456');
    apiRequestMock.mockRejectedValueOnce(new ApiError('invalid', 400));
    await submit(form);

    expect(container?.textContent).toContain('4 attempts remaining');
    expect(container?.textContent).toContain(
      'That authenticator code was not accepted. Check your app and try again.'
    );
  });

  it('discards an expired enrollment and lets the user restart', async () => {
    await renderAccount();
    apiRequestMock.mockResolvedValueOnce(setup);
    click(button('Set up authenticator'));
    await settle();
    const code = container?.querySelector<HTMLInputElement>('#totp-enable-code');
    const form = code?.closest('form');
    if (!code || !form) throw new Error('Missing setup form');
    const checkbox = container?.querySelector<HTMLInputElement>('input[type="checkbox"]');
    if (!checkbox) throw new Error('Missing recovery-code confirmation checkbox');
    click(checkbox);
    setInputValue(code, '123456');
    apiRequestMock.mockRejectedValueOnce(new ApiError('expired', 410));
    await submit(form);

    expect(container?.textContent).toContain(
      'Setup expired or too many codes were entered. Start setup again.'
    );
    expect(container?.textContent).not.toContain(setup.secret);
    expect(button('Set up authenticator')).toBeDefined();
  });

  it('disables TOTP with a valid code and reports an invalid code', async () => {
    await renderAccount({ ...user, totp_enabled: true });
    const code = container?.querySelector<HTMLInputElement>('#totp-disable-code');
    const form = code?.closest('form');
    if (!code || !form) throw new Error('Missing disable form');
    setInputValue(code, 'aBc123');
    expect(code.value).toBe('ABC123');

    apiRequestMock.mockRejectedValueOnce(new ApiError('invalid', 401));
    await submit(form);
    expect(container?.textContent).toContain(
      'That authenticator or recovery code was not accepted.'
    );

    apiRequestMock.mockResolvedValueOnce(undefined);
    await submit(form);
    expect(container?.textContent).toContain('Two-factor authentication is disabled.');
    expect(container?.textContent).toContain('Add an authenticator code as an extra sign-in step.');
  });

  it('redirects an expired session and handles logout success and failure', async () => {
    apiRequestMock.mockRejectedValueOnce(new ApiError('unauthorized', 401));
    const expired = render(<Account />);
    container = expired.container;
    await settle();
    expect(navigateMock).toHaveBeenCalledWith('/login', { replace: true });
    cleanup(container);
    container = undefined;

    const loadedAccount = await renderAccount();
    apiRequestMock.mockRejectedValueOnce(new Error('Network unavailable'));
    click(button('Log out'));
    await settle();
    expect(loadedAccount.textContent).toContain('Network unavailable');
    expect(button('Log out').disabled).toBe(false);

    apiRequestMock.mockResolvedValueOnce(undefined);
    click(button('Log out'));
    await settle();
    expect(navigateMock).toHaveBeenCalledWith('/login');
  });
});
