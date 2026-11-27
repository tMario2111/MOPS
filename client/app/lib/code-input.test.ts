import { describe, expect, it } from 'vitest';

import { applyCodeInput, emptyCodeInput, normalizeCodeInput } from './code-input';

describe('numeric code input', () => {
  it('keeps only digits and respects the code length', () => {
    expect(normalizeCodeInput('1a-2 345678', 6, 'numeric')).toBe('123456');
  });

  it('updates one box and preserves the other characters', () => {
    expect(applyCodeInput(['1', '2', '', '', '', ''], '3', 2, 6)).toEqual([
      '1',
      '2',
      '3',
      '',
      '',
      '',
    ]);
  });

  it('fills boxes from a pasted code', () => {
    expect(applyCodeInput(emptyCodeInput(6), '123456', undefined, 6)).toEqual([
      '1',
      '2',
      '3',
      '4',
      '5',
      '6',
    ]);
  });

  it('pads a partial paste with empty boxes', () => {
    expect(applyCodeInput(['9', '9', '9', '9', '9', '9'], '12', undefined, 6)).toEqual([
      '1',
      '2',
      '',
      '',
      '',
      '',
    ]);
  });

  it('creates an empty set of boxes for the requested length', () => {
    expect(emptyCodeInput(6)).toEqual(['', '', '', '', '', '']);
  });
});

describe('alphanumeric code input', () => {
  it('uppercases letters and filters characters outside the code alphabet', () => {
    expect(normalizeCodeInput('a-1b!c2d3e4', 8, 'alphanumeric')).toBe('A1BC2D3E');
  });
});
