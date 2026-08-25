import { FitAddon } from '@xterm/addon-fit'
import { SearchAddon } from '@xterm/addon-search'
import { Terminal } from '@xterm/xterm'
import { useCallback, useEffect, useRef, useState } from 'react'

import { openShellStream, type ShellHandle } from '@/lib/exec-stream'

import { readDrop, MAX_FILES } from './dropped-files'
import { TerminalSearch, type SearchState } from './TerminalSearch'

import '@xterm/xterm/css/xterm.css'

interface TerminalPaneProps {
  /** The WebSocket path the session lives at. */
  path: string
  /** Shown while the session is being set up — a node shell schedules a pod first. */
  opening?: string
  /** What this session is attached to, in one line above the prompt. */
  context?: React.ReactNode
  /** Accept dropped files into the session's working directory. */
  acceptsFiles?: boolean
  /**
   * Offered when the container turns out to have no shell. Attaching a debug container
   * cannot be undone while the pod lives, so it is the reader's decision, never a
   * silent retry.
   */
  onDebug?: (() => void) | undefined
}

type State = 'opening' | 'live' | 'closed' | 'failed'

/**
 * An interactive session, rendered by xterm.
 *
 * The terminal owns its own DOM: React renders an empty box and xterm writes into it.
 * Re-rendering the output through React would cost a reflow per line, which a build log
 * scrolling past makes immediately obvious.
 */
export function TerminalPane({ path, opening, context, acceptsFiles = false, onDebug }: TerminalPaneProps) {
  const host = useRef<HTMLDivElement>(null)
  const terminal = useRef<Terminal | null>(null)
  const search = useRef<SearchAddon | null>(null)
  const session = useRef<ShellHandle | null>(null)
  const [dropping, setDropping] = useState(false)

  const [state, setState] = useState<State>('opening')
  const [message, setMessage] = useState('')
  const [matches, setMatches] = useState<SearchState>({ current: 0, total: 0 })

  useEffect(() => {
    const container = host.current
    if (!container) return

    const term = new Terminal({
      // A concrete stack, not a custom property: xterm measures the font by writing into
      // a probe element, and a family it cannot resolve is measured as the fallback while
      // the glyphs are drawn in another — which is what put the rows on top of each other.
      fontFamily: monoStack(),
      fontSize: 13,
      // Above 1: at exactly the em box a descender has nowhere to go, and the tails of
      // g, y and p are cut off by the row beneath.
      lineHeight: 1.25,
      letterSpacing: 0,
      cursorBlink: true,
      cursorStyle: 'bar',
      convertEol: true,
      // The session is recorded from keystrokes rather than from what the container
      // prints, so a long scrollback costs only memory in this tab.
      scrollback: 10_000,
      theme: terminalTheme(),
    })

    const fit = new FitAddon()
    const finder = new SearchAddon()
    term.loadAddon(fit)
    term.loadAddon(finder)
    term.open(container)
    fit.fit()

    terminal.current = term
    search.current = finder

    const found = finder.onDidChangeResults((result) => {
      setMatches({ current: result.resultIndex + 1, total: result.resultCount })
    })

    let handle: ShellHandle | null = null
    let live = false

    handle = openShellStream(path, (event) => {
      switch (event.type) {
        case 'open':
          live = true
          setState('live')
          handle?.resize(term.cols, term.rows)
          break
        case 'stdout':
          term.write(event.data)
          break
        case 'end':
          setState('closed')
          term.write('\r\n\x1b[2m— session ended —\x1b[0m\r\n')
          break
        case 'error':
          setState('failed')
          setMessage(event.message)
          term.write(`\r\n\x1b[31m${event.message}\x1b[0m\r\n`)
          break
      }
    })

    session.current = handle
    const typed = term.onData((data) => handle?.send(data))

    const resize = new ResizeObserver(() => {
      try {
        fit.fit()
      } catch {
        // A pane collapsed to nothing has no size to fit to.
        return
      }
      if (live) handle?.resize(term.cols, term.rows)
    })
    resize.observe(container)

    term.focus()

    return () => {
      resize.disconnect()
      found.dispose()
      typed.dispose()
      handle?.close()
      term.dispose()
      terminal.current = null
      search.current = null
      session.current = null
    }
  }, [path])

  const find = useCallback((query: string, options: { caseSensitive: boolean; regex: boolean }) => {
    if (!query) {
      search.current?.clearDecorations()
      setMatches({ current: 0, total: 0 })
      return
    }
    search.current?.findNext(query, { ...options, incremental: true, decorations: DECORATIONS })
  }, [])

  const step = useCallback(
    (direction: 'next' | 'previous', query: string, options: { caseSensitive: boolean; regex: boolean }) => {
      if (!query) return
      const finder = search.current
      if (!finder) return

      const settings = { ...options, decorations: DECORATIONS }
      if (direction === 'next') finder.findNext(query, settings)
      else finder.findPrevious(query, settings)
    },
    [],
  )

  // Reading the drop first and sending after: the DataTransfer is emptied the moment the
  // handler yields, so the entries have to be taken from it synchronously.
  const onDrop = async (event: React.DragEvent) => {
    event.preventDefault()
    setDropping(false)
    if (!acceptsFiles) return

    const files = await readDrop(event.dataTransfer.items, event.dataTransfer.files)
    const handle = session.current
    if (!handle) return

    if (files.length >= MAX_FILES) {
      terminal.current?.write(`\r\n\x1b[31mtoo many files; the first ${MAX_FILES} were taken\x1b[0m\r\n`)
    }
    for (const dropped of files) {
      handle.upload(dropped.path, await dropped.file.arrayBuffer())
    }
  }

  return (
    <div
      className="relative flex h-full min-h-0 flex-col"
      style={{ backgroundColor: 'var(--bg-base)' }}
      {...(acceptsFiles
        ? {
            onDragOver: (event: React.DragEvent) => {
              event.preventDefault()
              setDropping(true)
            },
            onDragLeave: (event: React.DragEvent) => {
              // Only when the pointer has left the pane itself, or moving over the rows
              // inside it flickers the overlay on and off.
              if (!event.currentTarget.contains(event.relatedTarget as Node)) setDropping(false)
            },
            onDrop: (event: React.DragEvent) => void onDrop(event),
          }
        : {})}
    >
      <TerminalSearch matches={matches} onSearch={find} onStep={step} />

      {context && (
        <div
          className="shrink-0 border-b px-3 py-1.5"
          style={{
            borderColor: 'var(--border-subtle)',
            fontSize: 'var(--text-micro)',
            color: 'var(--text-secondary)',
          }}
        >
          {context}
        </div>
      )}

      {state === 'opening' && (
        <div
          className="flex shrink-0 items-center gap-2 border-b px-3 py-1.5"
          style={{ borderColor: 'var(--border-subtle)', fontSize: 'var(--text-micro)', color: 'var(--text-muted)' }}
        >
          <span
            aria-hidden="true"
            className="inline-block h-2.5 w-2.5 animate-spin rounded-full border border-current border-t-transparent"
          />
          {opening ?? 'Opening the session…'}
        </div>
      )}

      {state === 'failed' && message && (
        <div
          className="flex shrink-0 flex-wrap items-center gap-3 border-b px-3 py-1.5"
          style={{ borderColor: 'var(--border-subtle)', fontSize: 'var(--text-micro)' }}
        >
          <span style={{ color: 'var(--status-error)' }}>{message}</span>
          {onDebug && message.includes('no shell in container') && (
            <button type="button" onClick={onDebug} className="tool-button" style={{ color: 'var(--accent)' }}>
              Attach a debug container
            </button>
          )}
        </div>
      )}

      {/* The padding is on this wrapper rather than on the element xterm owns: xterm
          measures that element to decide how many rows fit, and padding inside it makes
          the last row land under the bottom edge. */}
      <div className="min-h-0 flex-1 overflow-hidden p-2">
        <div ref={host} className="h-full w-full" />
      </div>

      {dropping && (
        <div
          className="pointer-events-none absolute inset-0 z-10 flex items-center justify-center"
          style={{ backgroundColor: 'color-mix(in srgb, var(--bg-base) 78%, transparent)' }}
        >
          <div
            className="border-2 border-dashed px-6 py-4 text-center"
            style={{ borderColor: 'var(--accent)', borderRadius: 'var(--radius-panel)' }}
          >
            <p style={{ fontSize: 'var(--text-body)', color: 'var(--text-primary)' }}>
              Drop to add to this session
            </p>
            <p className="mt-1" style={{ fontSize: 'var(--text-micro)', color: 'var(--text-muted)' }}>
              A manifest, a values file, or a whole chart folder
            </p>
          </div>
        </div>
      )}
    </div>
  )
}

const DECORATIONS = {
  matchOverviewRuler: '#f2c94c',
  activeMatchColorOverviewRuler: '#f2994a',
  matchBackground: '#5a4a1e',
  activeMatchBackground: '#8a6d1f',
}

/**
 * The mono stack, resolved to real family names.
 *
 * xterm writes into a probe element to measure a character cell, and a family it cannot
 * resolve there is measured as one font while the rows are drawn in another.
 */
function monoStack(): string {
  const declared = getComputedStyle(document.documentElement).getPropertyValue('--font-mono')
  // The token is read raw, newlines and all, and xterm hands it straight to a style
  // property where a stray line break would truncate the list.
  const normalised = declared.replace(/\s+/g, ' ').trim()
  return normalised || 'Consolas, Menlo, monospace'
}

/** xterm needs concrete colours, so the theme tokens are read once at open. */
function terminalTheme() {
  const styles = getComputedStyle(document.documentElement)
  const token = (name: string, fallback: string) => styles.getPropertyValue(name).trim() || fallback

  return {
    background: token('--bg-base', '#131313'),
    foreground: token('--text-primary', '#e6edf3'),
    cursor: token('--accent', '#31c48d'),
    cursorAccent: token('--bg-base', '#131313'),
    selectionBackground: token('--bg-active', '#2d3441'),
  }
}
