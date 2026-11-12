import { afterEach, describe, expect, it, vi } from 'vitest';

const BLOCKLIST = ['PolniyPizdec0211', 'polniypizdec0211', 'qwertyuiop123456789'].join('\n');

async function importBlocklist() {
  vi.resetModules();
  return import('./password-blocklist');
}

function stubBlocklistFetch(body: string, status = 200) {
  const fetchMock = vi.fn().mockResolvedValue(new Response(body, { status }));
  vi.stubGlobal('fetch', fetchMock);
  return fetchMock;
}

afterEach(() => {
  vi.unstubAllGlobals();
  vi.resetModules();
});

describe('isPasswordBlocked', () => {
  it('blocks passwords from the list regardless of case', async () => {
    stubBlocklistFetch(BLOCKLIST);
    const { isPasswordBlocked } = await importBlocklist();

    await expect(isPasswordBlocked('polniypizdec0211')).resolves.toBe(true);
    await expect(isPasswordBlocked('POLNIYPIZDEC0211')).resolves.toBe(true);
  });

  it('allows passwords that are not in the list', async () => {
    stubBlocklistFetch(BLOCKLIST);
    const { isPasswordBlocked } = await importBlocklist();

    await expect(isPasswordBlocked('T7yQ!9zLm#2vR8kd')).resolves.toBe(false);
  });

  it('downloads and parses the list only once', async () => {
    const fetchMock = stubBlocklistFetch(BLOCKLIST);
    const { isPasswordBlocked } = await importBlocklist();

    await isPasswordBlocked('first');
    await isPasswordBlocked('second');

    expect(fetchMock).toHaveBeenCalledTimes(1);
  });

  it('retries after a failed download', async () => {
    const fetchMock = vi
      .fn()
      .mockRejectedValueOnce(new Error('network down'))
      .mockResolvedValueOnce(new Response(BLOCKLIST, { status: 200 }));
    vi.stubGlobal('fetch', fetchMock);
    const { isPasswordBlocked } = await importBlocklist();

    await expect(isPasswordBlocked('polniypizdec0211')).rejects.toThrow('network down');
    await expect(isPasswordBlocked('polniypizdec0211')).resolves.toBe(true);
    expect(fetchMock).toHaveBeenCalledTimes(2);
  });
});
