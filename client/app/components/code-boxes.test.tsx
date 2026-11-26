// @vitest-environment jsdom

import { afterEach, describe, expect, it, vi } from 'vitest';

import { CodeBoxes } from './code-boxes';
import { cleanup, click, render, setInputValue } from '../../test/test-utils';

describe('CodeBoxes', () => {
  let container: HTMLElement | undefined;

  afterEach(() => {
    if (container) cleanup(container);
    container = undefined;
  });

  it('renders the requested number of numeric, accessible boxes', () => {
    const result = render(
      <CodeBoxes
        ariaLabel="Authenticator code"
        characters={['', '', '', '', '', '']}
        kind="numeric"
        length={6}
        onChange={vi.fn()}
      />
    );
    container = result.container;

    expect(container.querySelectorAll('input')).toHaveLength(6);
    expect(container.querySelector('[aria-label="Authenticator code"]')).not.toBeNull();
    expect(container.querySelector('input')?.getAttribute('inputmode')).toBe('numeric');
    expect(container.querySelector('input')?.getAttribute('aria-label')).toBe(
      'Authenticator code character 1'
    );
  });

  it('filters a typed value and advances focus to the next box', () => {
    const onChange = vi.fn();
    const result = render(
      <CodeBoxes
        ariaLabel="Authenticator code"
        characters={['', '', '', '', '', '']}
        kind="numeric"
        length={6}
        onChange={onChange}
      />
    );
    container = result.container;
    const boxes = container.querySelectorAll('input');

    setInputValue(boxes[0], 'a7');

    expect(onChange).toHaveBeenCalledWith(['7', '', '', '', '', '']);
    expect(document.activeElement).toBe(boxes[1]);
  });

  it('distributes pasted text and focuses the next empty box', () => {
    const onChange = vi.fn();
    const result = render(
      <CodeBoxes
        ariaLabel="Email code"
        characters={['', '', '', '', '', '', '', '']}
        kind="alphanumeric"
        length={8}
        onChange={onChange}
      />
    );
    container = result.container;
    const boxes = container.querySelectorAll('input');
    const paste = new Event('paste', { bubbles: true, cancelable: true });
    Object.defineProperty(paste, 'clipboardData', {
      value: { getData: () => 'ab-12cd34!' },
    });

    boxes[0].dispatchEvent(paste);

    expect(paste.defaultPrevented).toBe(true);
    expect(onChange).toHaveBeenCalledWith(['A', 'B', '1', '2', 'C', 'D', '3', '4']);
    expect(document.activeElement).toBe(boxes[7]);
  });

  it('moves focus with arrow keys and backspace, and respects disabled state', () => {
    const result = render(
      <CodeBoxes
        ariaLabel="Authenticator code"
        characters={['1', '', '', '', '', '']}
        disabled
        kind="numeric"
        length={6}
        onChange={vi.fn()}
      />
    );
    container = result.container;
    const boxes = container.querySelectorAll('input');
    expect(boxes[0].disabled).toBe(true);

    result.unmount();
    container = undefined;
    const active = render(
      <CodeBoxes
        ariaLabel="Authenticator code"
        characters={['1', '', '', '', '', '']}
        kind="numeric"
        length={6}
        onChange={vi.fn()}
      />
    );
    container = active.container;
    const activeBoxes = container.querySelectorAll('input');

    activeBoxes[1].focus();
    activeBoxes[1].dispatchEvent(new KeyboardEvent('keydown', { key: 'Backspace', bubbles: true }));
    expect(document.activeElement).toBe(activeBoxes[0]);
    activeBoxes[0].dispatchEvent(
      new KeyboardEvent('keydown', { key: 'ArrowRight', bubbles: true })
    );
    expect(document.activeElement).toBe(activeBoxes[1]);
  });

  it('moves focus with the left arrow key', () => {
    const result = render(
      <CodeBoxes
        ariaLabel="Authenticator code"
        characters={['', '', '', '', '', '']}
        kind="numeric"
        length={6}
        onChange={vi.fn()}
      />
    );
    container = result.container;
    const boxes = container.querySelectorAll('input');

    click(boxes[2]);
    boxes[2].dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowLeft', bubbles: true }));

    expect(document.activeElement).toBe(boxes[1]);
  });
});
