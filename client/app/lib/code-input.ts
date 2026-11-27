export type CodeInputKind = 'alphanumeric' | 'numeric';

export function normalizeCodeInput(value: string, length: number, kind: CodeInputKind): string {
  const allowed = kind === 'numeric' ? /[0-9]/ : /[A-Z0-9]/;
  const normalized = kind === 'numeric' ? value : value.toUpperCase();
  return normalized
    .split('')
    .filter((character) => allowed.test(character))
    .join('')
    .slice(0, length);
}

export function emptyCodeInput(length: number): string[] {
  return Array.from({ length }, () => '');
}

export function applyCodeInput(
  current: readonly string[],
  normalizedValue: string,
  index: number | undefined,
  length: number
): string[] {
  if (index === undefined || normalizedValue.length > 1) {
    return [
      ...normalizedValue.split(''),
      ...Array.from({ length: length - normalizedValue.length }, () => ''),
    ];
  }
  const next = [...current];
  next[index] = normalizedValue;
  return next;
}
