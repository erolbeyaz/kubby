import { useCallback, useEffect, useMemo, useRef, useState, type ReactNode } from 'react'
import { useQuery } from '@tanstack/react-query'

import { api } from '@/lib/api'
import { openLogStream, type LogEvent } from '@/lib/log-stream'

import { ContainerPicker } from './ContainerPicker'
import { LogSearch, matchesFor, type SearchOptions } from './LogSearch'
import { IconButton, IconToggle } from './ToolbarButton'
import { parseLine, type LogLevel, type LogSpan } from './log-colour'

/**
 * How many lines the view keeps.
 *
 * A pod that logs a thousand lines a second would otherwise grow the page until the tab
 * dies. Older lines fall off the top, which is the end a reader following a live log is
 * not looking at.
 */
const BUFFER = 5000

/** The picker sizes to its content; the search field takes the room a query needs. */
const BOX_WIDTH = '11rem'
const SEARCH_WIDTH = '17rem'

interface LogPaneProps {
  clusterId: string
  namespace: string
  pod: string
}

interface Line {
  id: number
  container: string
  text: string
  level: LogLevel
  spans: LogSpan[]
}

export function LogPane({ clusterId, namespace, pod }: LogPaneProps) {
  // What the reader picked, which may be nothing. Kept apart from what the server then
  // resolved that to: folding the two together made the resolved name change the stream
  // key, so every log opened twice and aborted its own first connection.
  // A set, because "All containers" is a real answer: a pod's application and its
  // sidecar often only make sense read together, and flipping between them loses the
  // ordering that shows which caused which.
  const [chosen, setChosen] = useState<string[]>([])
  const [resolved, setResolved] = useState('')
  const [follow, setFollow] = useState(true)
  const [previous, setPrevious] = useState(false)
  const [timestamps, setTimestamps] = useState(false)
  const [showNames, setShowNames] = useState(false)
  const [wrap, setWrap] = useState(true)
  const [search, setSearch] = useState<SearchOptions>({
    term: '',
    matchCase: false,
    regex: false,
    onlyMatching: false,
  })

  const containers = useQuery({
    queryKey: ['containers', clusterId, namespace, pod],
    queryFn: ({ signal }) => api.podContainers(clusterId, namespace, pod, signal),
    staleTime: 30_000,
  })

  // Changing any of these is a different log, not the same one filtered. Keying the
  // stream on them gives it fresh state by remounting, which is what React offers
  // instead of clearing state from inside an effect.
  const all = containers.data?.containers.map((container) => container.name) ?? []
  const streaming = chosen.length > 0 ? chosen : all.length > 0 ? all : ['']
  const streamKey = `${clusterId}/${namespace}/${pod}/${streaming.join(',')}/${follow}/${previous}/${timestamps}`

  return (
    <LogStream
      key={streamKey}
      clusterId={clusterId}
      namespace={namespace}
      pod={pod}
      containers={streaming}
      openedContainer={chosen.length === 1 ? chosen[0]! : resolved}
      follow={follow}
      previous={previous}
      timestamps={timestamps}
      showNames={showNames || streaming.length > 1}
      wrap={wrap}
      search={search}
      onContainerResolved={setResolved}
      controls={
        <>
          <div style={{ width: BOX_WIDTH }}>
            <ContainerPicker
              containers={containers.data?.containers ?? []}
              selected={chosen}
              onChange={setChosen}
            />
          </div>

          <div style={{ width: SEARCH_WIDTH }}>
            <LogSearch value={search} onChange={setSearch} />
          </div>

        </>
      }
      controlsRight={
        <>
          <IconToggle
            label="Follow new lines"
            active={follow}
            onChange={setFollow}
            path="M8 4a5 5 0 100 10 5 5 0 000-10M8 6.4V9l2 1.3M6.4 2h3.2M8 2v2"
          />
          <IconToggle
            label="Show the previous instance"
            active={previous}
            onChange={setPrevious}
            path="M2.8 8.5a5.2 5.2 0 111.7 3.9M2.8 5v3.5h3.5M8 5.6v3l2.2 1.3"
          />
          <IconToggle
            label="Show timestamps"
            active={timestamps}
            onChange={setTimestamps}
            path="M8 2.8a5.2 5.2 0 100 10.4 5.2 5.2 0 000-10.4M8 5.4v2.8l2.2 1.3"
          />
          <IconToggle
            label="Show resource names"
            active={showNames}
            onChange={setShowNames}
            path="M2.6 7.2V3h4.2l6 6-4.2 4.2-6-6zM4.9 5.1h.01"
          />
          <IconToggle
            label="Wrap lines"
            active={wrap}
            onChange={setWrap}
            path="M2.5 4h11M2.5 8h8.5a2 2 0 110 4H8M9.6 10.6 8 12l1.6 1.4"
          />
        </>
      }
    />
  )
}

interface LogStreamProps extends LogPaneProps {
  containers: string[]
  /** What the stream actually opened, for the heading. */
  openedContainer: string
  showNames: boolean
  wrap: boolean
  follow: boolean
  previous: boolean
  timestamps: boolean
  search: SearchOptions
  controls: ReactNode
  /** The toggles, which sit between dividers after the search. */
  controlsRight: ReactNode
  onContainerResolved: (name: string) => void
}

function LogStream({
  clusterId,
  namespace,
  pod,
  containers,
  openedContainer,
  follow,
  previous,
  timestamps,
  showNames,
  wrap,
  search,
  controls,
  controlsRight,
  onContainerResolved,
}: LogStreamProps) {
  const [lines, setLines] = useState<Line[]>([])
  const [status, setStatus] = useState<'connecting' | 'streaming' | 'ended' | 'error'>('connecting')
  const [error, setError] = useState('')

  const [matchIndex, setMatchIndex] = useState(0)

  const nextId = useRef(0)
  const scroller = useRef<HTMLDivElement>(null)
  const pinned = useRef(true)
  const onEvent = useCallback(
    (event: LogEvent, from: string) => {
      switch (event.type) {
        case 'open':
          // The server decides which container an empty choice means (ADR-030), so the
          // picker is told what it actually opened rather than guessing alongside it.
          onContainerResolved(event.container)
          setStatus('streaming')
          return
        case 'line':
          setLines((current) => {
            const parsed = parseLine(event.line)
            const line = { id: nextId.current++, container: from, text: event.line, ...parsed }
            const next = [...current, line]
            return next.length > BUFFER ? next.slice(next.length - BUFFER) : next
          })
          return
        case 'end':
          setStatus('ended')
          return
        case 'error':
          setError(event.message)
          setStatus('error')
      }
    },
    [onContainerResolved],
  )

  // One socket per container. Merging on the client rather than asking the server for a
  // combined stream keeps each container's own ordering intact, which is what makes
  // "the sidecar logged this just before the app died" readable.
  const wanted = containers.join(',')
  useEffect(() => {
    const closers = wanted
      .split(',')
      .map((name) =>
        openLogStream(
          {
            clusterId,
            namespace,
            pod,
            ...(name ? { container: name } : {}),
            follow,
            previous,
            timestamps,
            tail: 1000,
          },
          (event) => onEvent(event, name),
        ),
      )
    return () => closers.forEach((close) => close())
  }, [clusterId, namespace, pod, wanted, follow, previous, timestamps, onEvent])

  // Following means staying at the bottom — unless the reader has scrolled up, in which
  // case yanking them back down is the view fighting them.
  useEffect(() => {
    if (!follow || !pinned.current || search.term) return
    const element = scroller.current
    if (element) element.scrollTop = element.scrollHeight
  }, [lines, follow, search.term])

  const onScroll = () => {
    const element = scroller.current
    if (!element) return
    pinned.current = element.scrollHeight - element.scrollTop - element.clientHeight < 40
  }

  // Following stops when the reader scrolls up, so there has to be a way back down that
  // is not "scroll to the end of ten thousand lines by hand".
  const jumpToPresent = () => {
    const element = scroller.current
    if (!element) return
    pinned.current = true
    element.scrollTop = element.scrollHeight
  }

  // Search highlights and steps between matches by default rather than hiding the rest:
  // filtering a log removes exactly the context that makes a match mean anything. The
  // funnel in the search box is there for when that is what the reader wants.
  const matches = useMemo(() => matchesFor(lines, search), [lines, search])
  const shown = useMemo(
    () => (search.onlyMatching && search.term ? lines.filter((line) => matches.includes(line.id)) : lines),
    [lines, matches, search.onlyMatching, search.term],
  )

  const plainText = useMemo(() => lines.map((line) => line.text).join('\n'), [lines])

  return (
    <div className="flex h-full flex-col" style={{ backgroundColor: 'var(--bg-base)' }}>
      <header
        className="flex h-9 shrink-0 items-center gap-2 border-b px-2"
        style={{ borderColor: 'var(--border-subtle)', backgroundColor: 'var(--bg-surface)' }}
      >
        {/* Groups, separated by rules: what to read (container, search, match stepper),
            how to read it (the toggles), what to do with it (download). Jump-to-present
            is pinned right, where the end of the stream is. */}
        {controls}

        <MatchStepper
          total={matches.length}
          index={matchIndex}
          onStep={setMatchIndex}
          active={search.term !== ''}
        />

        <Divider />

        {controlsRight}

        <Divider />

        <DownloadButton
          text={plainText}
          name={`${pod}${openedContainer ? `-${openedContainer}` : ''}.log`}
        />

        <span className="ml-auto flex items-center gap-2">
          <StatusDot status={status} />
          <span className="font-mono" style={{ fontSize: 'var(--text-micro)', color: 'var(--text-muted)' }}>
            {lines.length} lines
          </span>
          <IconButton
            label="Jump to present"
            onClick={jumpToPresent}
            path="M4 3.4 8 7l4-3.6M4 7.8 8 11.4l4-3.6M3.2 14h9.6"
          />
        </span>
      </header>

      {error && (
        <p className="px-3 py-1.5" style={{ fontSize: 'var(--text-micro)', color: 'var(--status-error)' }}>
          {error}
        </p>
      )}

      {/* Which log this is. A stream of text with no heading is the one thing a reader
          cannot check when they have three of them open. */}
      <p
        className="shrink-0 border-b px-3 py-1"
        style={{
          fontSize: 'var(--text-micro)',
          color: 'var(--text-muted)',
          borderColor: 'var(--border-subtle)',
          backgroundColor: 'var(--bg-surface)',
        }}
      >
        Namespace <span style={{ color: 'var(--accent)' }}>{namespace}</span>
        {' · Pod '}
        <span style={{ color: 'var(--accent)' }}>{pod}</span>
        {openedContainer && (
          <>
            {' · Container '}
            <span style={{ color: 'var(--accent)' }}>{openedContainer}</span>
          </>
        )}
        {previous && ' · previous instance'}
      </p>

      <div
        ref={scroller}
        onScroll={onScroll}
        className="min-h-0 flex-1 overflow-auto px-3 py-2 font-mono"
        style={{ fontSize: 'var(--text-micro)', lineHeight: 1.6, backgroundColor: 'var(--bg-terminal)' }}
      >
        {shown.map((line) => (
          <LogLine
            key={line.id}
            line={line}
            highlight={search.term}
            showName={showNames}
            wrap={wrap}
            current={matches[matchIndex] === line.id}
          />
        ))}

        {shown.length === 0 && (
          <p style={{ color: 'var(--text-muted)' }}>
            {status === 'connecting'
              ? 'Connecting…'
              : search.onlyMatching && search.term
                ? 'No line matches the search.'
                : previous
                ? 'The previous instance wrote nothing, or there was no previous instance.'
                : 'No output yet.'}
          </p>
        )}
      </div>
    </div>
  )
}

function LogLine({
  line,
  highlight,
  showName,
  wrap,
  current,
}: {
  line: Line
  highlight: string
  showName: boolean
  wrap: boolean
  current: boolean
}) {
  const element = useRef<HTMLDivElement>(null)

  // Stepping to a match has to bring it into view; a counter that changes while the
  // screen does not is a control that appears broken.
  useEffect(() => {
    if (current) element.current?.scrollIntoView({ block: 'center' })
  }, [current])

  return (
    <div
      ref={element}
      className={wrap ? 'whitespace-pre-wrap break-all' : 'whitespace-pre'}
      style={{
        borderLeft: `2px solid ${levelColour(line.level) ?? 'transparent'}`,
        paddingLeft: 6,
        backgroundColor: current ? 'var(--bg-hover)' : undefined,
      }}
    >
      {showName && line.container && (
        <span style={{ color: 'var(--accent)' }}>[{line.container}] </span>
      )}
      {line.spans.map((span, index) => (
        <span key={index} style={{ color: spanColour(span.kind, line.level) }}>
          {highlight ? mark(span.text, highlight) : span.text}
        </span>
      ))}
    </div>
  )
}

/** Marks every occurrence of the search term inside one span. */
function mark(text: string, needle: string) {
  const lower = text.toLowerCase()
  const target = needle.toLowerCase()

  const parts = []
  let at = 0
  for (let found = lower.indexOf(target); found !== -1; found = lower.indexOf(target, at)) {
    if (found > at) parts.push(text.slice(at, found))
    parts.push(
      <mark
        key={found}
        style={{ backgroundColor: 'var(--accent-muted)', color: 'var(--accent)', padding: 0 }}
      >
        {text.slice(found, found + needle.length)}
      </mark>,
    )
    at = found + needle.length
  }
  if (at === 0) return text
  if (at < text.length) parts.push(text.slice(at))
  return parts
}

/** How many lines match, and a way to step through them. */
function MatchStepper({
  total,
  index,
  onStep,
  active,
}: {
  total: number
  index: number
  onStep: (next: number) => void
  active: boolean
}) {
  const step = (delta: number) => {
    if (total === 0) return
    onStep((index + delta + total) % total)
  }

  return (
    <span className="flex shrink-0 items-center gap-0.5">
      <span
        className="w-12 text-right font-mono"
        style={{
          fontSize: 'var(--text-micro)',
          color: active && total > 0 ? 'var(--accent)' : 'var(--text-muted)',
        }}
      >
        {total === 0 ? '0/0' : `${index + 1}/${total}`}
      </span>
      <StepButton label="Previous match" onClick={() => step(-1)} disabled={!active || total === 0} up />
      <StepButton label="Next match" onClick={() => step(1)} disabled={!active || total === 0} />
    </span>
  )
}

function StepButton({
  label,
  onClick,
  disabled,
  up = false,
}: {
  label: string
  onClick: () => void
  disabled: boolean
  up?: boolean
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      disabled={disabled}
      aria-label={label}
      title={label}
      className="tool-chip"
    >
      <svg width="12" height="12" viewBox="0 0 10 10" aria-hidden="true" style={{ transform: up ? 'rotate(180deg)' : undefined }}>
        <path d="M2 3.5 L5 6.5 L8 3.5" fill="none" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" />
      </svg>
    </button>
  )
}

function levelColour(level: LogLevel): string | null {
  switch (level) {
    case 'error':
      return 'var(--status-error)'
    case 'warn':
      return 'var(--status-warn)'
    default:
      return null
  }
}

function spanColour(kind: LogSpan['kind'], level: LogLevel): string {
  switch (kind) {
    case 'timestamp':
      return 'var(--text-muted)'
    case 'level':
      return levelColour(level) ?? 'var(--accent)'
    case 'key':
      return 'var(--accent)'
    case 'number':
      return 'var(--status-warn)'
    case 'string':
      return 'var(--text-primary)'
    default:
      return 'var(--text-secondary)'
  }
}

/** A rule between groups of controls, so the toolbar reads as sections. */
function Divider() {
  return <span className="mx-1 h-4 w-px shrink-0" style={{ backgroundColor: 'var(--border-default)' }} />
}

function StatusDot({ status }: { status: 'connecting' | 'streaming' | 'ended' | 'error' }) {
  const colour =
    status === 'streaming'
      ? 'var(--accent)'
      : status === 'error'
        ? 'var(--status-error)'
        : 'var(--text-muted)'

  return <span aria-label={status} title={status} className="h-1.5 w-1.5 rounded-full" style={{ backgroundColor: colour }} />
}

function DownloadButton({ text, name }: { text: string; name: string }) {
  const save = () => {
    const url = URL.createObjectURL(new Blob([text], { type: 'text/plain' }))
    const link = document.createElement('a')
    link.href = url
    link.download = name
    link.click()
    URL.revokeObjectURL(url)
  }

  return (
    <IconButton
      label="Download"
      onClick={save}
      path="M8 2.2v6.4M5.4 6.2 8 8.8l2.6-2.6M2.8 10.4v1.8a1.2 1.2 0 001.2 1.2h8a1.2 1.2 0 001.2-1.2v-1.8"
    />
  )
}
