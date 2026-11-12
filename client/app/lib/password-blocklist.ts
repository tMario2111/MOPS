// Vite's BASE_URL always ends with a slash, so this works for subpath deploys.
const BLOCKLIST_PATH = `${import.meta.env.BASE_URL}pwlist_an_len16.txt`;

let blocklistRequest: Promise<ReadonlySet<string>> | null = null;

/**
 * Loads the client-side blocklist of commonly used passwords. The list is a
 * static asset, so it is fetched in the browser and never reaches the server.
 * A successful load is cached for the lifetime of the page.
 */
export function loadPasswordBlocklist(): Promise<ReadonlySet<string>> {
  if (blocklistRequest === null) {
    blocklistRequest = fetchBlocklist().catch((error: unknown) => {
      // Do not cache a failed load; the next call retries.
      blocklistRequest = null;
      throw error;
    });
  }
  return blocklistRequest;
}

async function fetchBlocklist(): Promise<ReadonlySet<string>> {
  const response = await fetch(BLOCKLIST_PATH);
  if (!response.ok) {
    throw new Error(`Could not load the password blocklist (${response.status}).`);
  }

  const text = await response.text();
  const entries = text
    .split(/\r?\n/)
    .map((line) => line.trim().toLowerCase())
    .filter((line) => line.length > 0);
  return new Set(entries);
}

/**
 * Reports whether the password appears in the blocklist. Matching is
 * case-insensitive because the list is a set of commonly used passwords rather
 * than a list of exact credentials.
 */
export async function isPasswordBlocked(password: string): Promise<boolean> {
  const blocklist = await loadPasswordBlocklist();
  return blocklist.has(password.toLowerCase());
}
