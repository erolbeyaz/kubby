import '@testing-library/jest-dom/vitest'

// jsdom does not implement ResizeObserver, which virtualised lists rely on.
if (typeof globalThis.ResizeObserver === 'undefined') {
  globalThis.ResizeObserver = class {
    observe() {}
    unobserve() {}
    disconnect() {}
  }
}

// jsdom implements neither half of the deprecated clipboard API: Monaco probes the
// first, and the copy button falls back to the second when the async clipboard is
// refused. Both are declared writable so a test can decide what they answer.
if (typeof document !== 'undefined' && !('queryCommandSupported' in document)) {
  Object.defineProperty(document, 'queryCommandSupported', { value: () => false, writable: true })
}
if (typeof document !== 'undefined' && !('execCommand' in document)) {
  Object.defineProperty(document, 'execCommand', { value: () => false, writable: true })
}
