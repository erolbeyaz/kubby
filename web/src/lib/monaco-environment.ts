import EditorWorker from 'monaco-editor/editor/editor.worker?worker'

/**
 * Monaco resolves its workers through a global hook.
 *
 * Only the core editor worker is registered. Language-service workers exist for
 * validation and completion, which a read-only YAML view does not perform — and the
 * TypeScript one alone is several megabytes.
 */
self.MonacoEnvironment = {
  getWorker() {
    return new EditorWorker()
  },
}
