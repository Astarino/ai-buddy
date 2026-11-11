import '@testing-library/jest-dom/vitest';
import { afterEach, vi } from 'vitest';
import { cleanup } from '@testing-library/react';

afterEach(() => cleanup());
Object.defineProperty(HTMLDialogElement.prototype, 'showModal', {
  value: function (this: HTMLDialogElement) {
    this.open = true;
  },
  configurable: true,
});
Object.defineProperty(HTMLDialogElement.prototype, 'close', {
  value: function (this: HTMLDialogElement) {
    this.open = false;
  },
  configurable: true,
});
Object.defineProperty(Element.prototype, 'scrollIntoView', { value: vi.fn(), configurable: true });
Object.defineProperty(window, 'matchMedia', {
  value: () => ({ matches: false, addEventListener() {}, removeEventListener() {} }),
  configurable: true,
});
