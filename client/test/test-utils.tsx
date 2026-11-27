import { act, type ReactNode } from 'react';
import { createRoot, type Root } from 'react-dom/client';

Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true });

const roots = new WeakMap<HTMLElement, Root>();

export function render(element: ReactNode) {
  const container = document.createElement('div');
  document.body.append(container);
  const root = createRoot(container);
  roots.set(container, root);
  act(() => root.render(element));

  return {
    container,
    unmount() {
      act(() => root.unmount());
      roots.delete(container);
      container.remove();
    },
  };
}

export async function settle() {
  await act(async () => {
    await Promise.resolve();
    await Promise.resolve();
    await Promise.resolve();
  });
}

export function setInputValue(element: HTMLInputElement, value: string) {
  act(() => {
    Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')?.set?.call(element, value);
    element.dispatchEvent(new Event('input', { bubbles: true }));
  });
}

export function click(element: HTMLElement) {
  act(() => element.click());
}

export async function submit(form: HTMLFormElement) {
  await act(async () => {
    form.requestSubmit();
    await Promise.resolve();
    await Promise.resolve();
    await Promise.resolve();
  });
}

export function cleanup(container: HTMLElement) {
  const root = roots.get(container);
  if (root) {
    act(() => root.unmount());
    roots.delete(container);
  }
  container.remove();
}
