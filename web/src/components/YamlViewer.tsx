import { useEffect, useRef } from 'react'
// The editor API and one language, not the whole package. Importing 'monaco-editor'
// pulls in every language definition and every language worker — including a 6.9 MB
// TypeScript worker that a read-only YAML view has no use for.
import * as monaco from 'monaco-editor/editor/editor.api'
import 'monaco-editor/languages/definitions/yaml/register'
import 'monaco-editor/editor/contrib/folding/browser/folding'
import 'monaco-editor/editor/contrib/find/browser/findController'

import '@/lib/monaco-environment'

const THEME_DARK = 'kubby-dark'
const THEME_LIGHT = 'kubby-light'

let themesDefined = false

/**
 * Reads a colour out of the stylesheet so the editor matches the rest of the interface
 * rather than shipping its own palette.
 */
function token(name: string, fallback: string): string {
  if (typeof window === 'undefined') return fallback
  const value = getComputedStyle(document.documentElement).getPropertyValue(name).trim()
  return value || fallback
}

function defineThemes() {
  if (themesDefined) return
  themesDefined = true

  // Language constructs are deliberately quiet and values are bright: in a manifest the
  // values are what is being read, and the keys are structure.
  const rules = (comment: string, key: string, value: string, number: string) => [
    { token: 'comment', foreground: comment },
    { token: 'type', foreground: key },
    { token: 'key', foreground: key },
    { token: 'string', foreground: value },
    { token: 'string.yaml', foreground: value },
    { token: 'number', foreground: number },
    { token: 'keyword', foreground: number },
    { token: 'delimiter', foreground: comment },
  ]

  monaco.editor.defineTheme(THEME_DARK, {
    base: 'vs-dark',
    inherit: true,
    rules: rules('6b6b6b', '7dd3fc', 'e8e8e8', 'f0a868'),
    colors: {
      'editor.background': token('--bg-base', '#131313'),
      'editor.foreground': token('--text-primary', '#e8e8e8'),
      'editorLineNumber.foreground': '#4a4a4a',
      'editorLineNumber.activeForeground': token('--accent', '#10b981'),
      'editorIndentGuide.background1': '#262626',
      'editorIndentGuide.activeBackground1': '#3d3d3d',
      'editor.lineHighlightBackground': '#1c1c1c',
      'editor.selectionBackground': '#10b98133',
      'editorGutter.background': token('--bg-base', '#131313'),
      'editorWidget.background': token('--bg-overlay', '#252525'),
      'editorWidget.border': token('--border-strong', '#3d3d3d'),
    },
  })

  monaco.editor.defineTheme(THEME_LIGHT, {
    base: 'vs',
    inherit: true,
    rules: rules('8a8a8a', '0369a1', '1a1a1a', 'b45309'),
    colors: {
      'editor.background': '#ffffff',
      'editor.foreground': '#1a1a1a',
      'editorLineNumber.foreground': '#b4b4b4',
      'editorIndentGuide.background1': '#e8e8e8',
    },
  })
}

interface YamlViewerProps {
  value: string
  /** Height is driven by the container; the editor fills whatever it is given. */
  ariaLabel?: string
}

/**
 * A read-only YAML view with line numbers, syntax colouring and indent guides.
 *
 * A Kubernetes manifest is deeply nested, and plain preformatted text makes following
 * that nesting a matter of counting spaces. The same editor is what phase 7 will use
 * for editing and diffing, so the reading and writing views stay consistent.
 */
export function YamlViewer({ value, ariaLabel }: YamlViewerProps) {
  const container = useRef<HTMLDivElement>(null)
  const editor = useRef<monaco.editor.IStandaloneCodeEditor | null>(null)

  useEffect(() => {
    const element = container.current
    if (!element) return

    defineThemes()

    const dark = !document.documentElement.matches('[data-theme="light"]')
    const instance = monaco.editor.create(element, {
      value,
      language: 'yaml',
      theme: dark ? THEME_DARK : THEME_LIGHT,
      readOnly: true,
      domReadOnly: true,
      automaticLayout: true,
      minimap: { enabled: false },
      lineNumbers: 'on',
      lineNumbersMinChars: 4,
      guides: { indentation: true, bracketPairs: false },
      renderLineHighlight: 'line',
      scrollBeyondLastLine: false,
      fontFamily: token('--font-mono', 'monospace'),
      fontSize: 12.5,
      lineHeight: 1.65,
      padding: { top: 10, bottom: 10 },
      folding: true,
      wordWrap: 'off',
      contextmenu: false,
      scrollbar: { verticalScrollbarSize: 10, horizontalScrollbarSize: 10 },
      overviewRulerLanes: 0,
      renderWhitespace: 'none',
      stickyScroll: { enabled: true },
    })

    editor.current = instance
    if (ariaLabel) instance.updateOptions({ ariaLabel })

    return () => {
      instance.getModel()?.dispose()
      instance.dispose()
      editor.current = null
    }
    // The editor is created once; content updates go through the effect below.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  useEffect(() => {
    const instance = editor.current
    if (instance && instance.getValue() !== value) {
      instance.setValue(value)
    }
  }, [value])

  return <div ref={container} className="h-full w-full" data-testid="yaml-viewer" />
}
