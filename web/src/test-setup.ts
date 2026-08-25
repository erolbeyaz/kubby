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

// jsdom implements no EventSource. The streaming path is covered by its own unit tests
// and by an integration test against a real cluster; a component test only needs the
// constructor not to throw.
if (typeof globalThis.EventSource === 'undefined') {
  class StubEventSource {
    onmessage: ((event: MessageEvent) => void) | null = null
    onerror: ((event: Event) => void) | null = null
    close() {}
  }
  Object.defineProperty(globalThis, 'EventSource', { value: StubEventSource, writable: true })
}

// jsdom implements no scrollIntoView. The command palette keeps the highlighted row in
// view with it, which is a real browser behaviour worth having and nothing a test can
// assert on.
if (typeof Element !== 'undefined' && !Element.prototype.scrollIntoView) {
  Element.prototype.scrollIntoView = function scrollIntoView() {}
}
