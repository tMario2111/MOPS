// @vitest-environment jsdom

import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { ApiError } from '../lib/api';
import { cleanup, click, render, settle } from '../../test/test-utils';
import { Sidebar } from './sidebar';

const mocks = vi.hoisted(() => ({
  apiRequest: vi.fn(),
  navigate: vi.fn(),
  pathname: { value: '/friends' },
}));

vi.mock('../lib/api', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../lib/api')>();
  return { ...actual, apiRequest: mocks.apiRequest };
});

vi.mock('react-router', () => ({
  useLocation: () => ({ pathname: mocks.pathname.value }),
  useNavigate: () => mocks.navigate,
}));

describe('Sidebar', () => {
  let container: HTMLElement | undefined;

  beforeEach(() => {
    mocks.apiRequest.mockReset();
    mocks.navigate.mockReset();
    mocks.pathname.value = '/friends';
  });

  afterEach(() => {
    if (container) cleanup(container);
    container = undefined;
  });

  function button(label: string): HTMLButtonElement {
    const match = [...(container?.querySelectorAll('button') ?? [])].find(
      (item) => item.textContent?.trim() === label || item.getAttribute('aria-label') === label
    );
    if (!(match instanceof HTMLButtonElement)) throw new Error(`Missing button: ${label}`);
    return match;
  }

  it('opens account settings from the Settings entry', () => {
    container = render(<Sidebar currentUser={{ username: 'test_user' }} />).container;
    const settings = button('Settings');
    expect(settings.disabled).toBe(false);

    click(settings);
    expect(mocks.navigate).toHaveBeenCalledWith('/account');
  });

  it('highlights Settings while on the account page', () => {
    mocks.pathname.value = '/account';
    container = render(<Sidebar currentUser={{ username: 'test_user' }} />).container;
    expect(button('Settings').className).toContain('bg-accent-soft');
    expect(button('Friends').className).not.toContain('bg-accent-soft');
  });

  it('shows the user initials and logs out from the account menu', async () => {
    container = render(<Sidebar currentUser={{ username: 'test_user' }} />).container;
    expect(container.textContent).toContain('TE');

    click(button('Account options'));
    mocks.apiRequest.mockResolvedValueOnce(undefined);
    click(button('Log out'));
    await settle();
    expect(mocks.apiRequest).toHaveBeenCalledWith('/api/auth/logout');
    expect(mocks.navigate).toHaveBeenCalledWith('/login');
  });

  it('treats an expired session during logout as logged out', async () => {
    container = render(<Sidebar currentUser={null} />).container;
    click(button('Account options'));
    mocks.apiRequest.mockRejectedValueOnce(new ApiError('unauthorized', 401));
    click(button('Log out'));
    await settle();
    expect(mocks.navigate).toHaveBeenCalledWith('/login');
  });

  it('closes the menu when logout fails', async () => {
    container = render(<Sidebar currentUser={{ username: 'test_user' }} />).container;
    click(button('Account options'));
    mocks.apiRequest.mockRejectedValueOnce(new Error('Network unavailable'));
    click(button('Log out'));
    await settle();
    expect(mocks.navigate).not.toHaveBeenCalled();
    expect(container.textContent).not.toContain('Log out');
  });
});
