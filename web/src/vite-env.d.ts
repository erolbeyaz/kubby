/// <reference types="vite/client" />

// Vite's ?worker suffix has no types of its own; declaring it keeps the Monaco worker
// import checked rather than silently any.
declare module '*?worker' {
  const workerConstructor: new () => Worker
  export default workerConstructor
}

// Monaco's deep ESM paths carry no declarations of their own; the editor API is typed
// through its own .d.ts, and the language registrations are side effects.
declare module 'monaco-editor/editor/editor.api' {
  export * from 'monaco-editor'
}
declare module 'monaco-editor/languages/definitions/*/register'
declare module 'monaco-editor/editor/contrib/*'
