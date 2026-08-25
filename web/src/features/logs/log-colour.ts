/** One piece of a log line, already decided what it is. */
export interface LogSpan {
  text: string
  kind: 'timestamp' | 'level' | 'key' | 'string' | 'number' | 'plain'
}

export type LogLevel = 'error' | 'warn' | 'info' | 'debug' | null

const LEVELS: Record<string, LogLevel> = {
  fatal: 'error',
  error: 'error',
  err: 'error',
  warning: 'warn',
  warn: 'warn',
  info: 'info',
  information: 'info',
  notice: 'info',
  debug: 'debug',
  trace: 'debug',
}

const RFC3339 = /^\S*\d{4}-\d{2}-\d{2}[T ]\d{2}:\d{2}:\d{2}(\.\d+)?(Z|[+-]\d{2}:?\d{2})?/
const LEVEL_WORD = /\b(FATAL|ERROR|ERR|WARNING|WARN|INFO|INFORMATION|NOTICE|DEBUG|TRACE)\b/i

/**
 * Reads a log line well enough to colour it.
 *
 * Applications log in whatever shape they please, so the format is recognised per line
 * rather than assumed for the stream: one container may write JSON while its sidecar
 * writes plain text, and both end up in the same view.
 */
export function parseLine(line: string): { level: LogLevel; spans: LogSpan[] } {
  const json = asJson(line)
  if (json) return json

  const spans: LogSpan[] = []
  let rest = line

  const timestamp = RFC3339.exec(rest)
  if (timestamp) {
    spans.push({ text: timestamp[0], kind: 'timestamp' })
    rest = rest.slice(timestamp[0].length)
  }

  const level = LEVEL_WORD.exec(rest)
  if (!level) {
    if (rest) spans.push({ text: rest, kind: 'plain' })
    return { level: null, spans }
  }

  const at = level.index
  if (at > 0) spans.push({ text: rest.slice(0, at), kind: 'plain' })
  spans.push({ text: level[0], kind: 'level' })
  const tail = rest.slice(at + level[0].length)
  if (tail) spans.push({ text: tail, kind: 'plain' })

  return { level: LEVELS[level[0].toLowerCase()] ?? null, spans }
}

/**
 * Structured logs are the common case in a cluster, and reading them as flat text is
 * most of why logs are hard to scan. Keys and values are separated so the eye can.
 */
function asJson(line: string): { level: LogLevel; spans: LogSpan[] } | null {
  const trimmed = line.trim()
  if (!trimmed.startsWith('{') || !trimmed.endsWith('}')) return null

  let parsed: unknown
  try {
    parsed = JSON.parse(trimmed)
  } catch {
    return null
  }
  if (typeof parsed !== 'object' || parsed === null || Array.isArray(parsed)) return null

  const record = parsed as Record<string, unknown>
  const spans: LogSpan[] = []
  let level: LogLevel = null
  let first = true

  for (const [key, value] of Object.entries(record)) {
    if (!first) spans.push({ text: '  ', kind: 'plain' })
    first = false

    spans.push({ text: key, kind: 'key' })
    spans.push({ text: '=', kind: 'plain' })

    const text = typeof value === 'string' ? value : JSON.stringify(value)
    if (isLevelKey(key)) {
      level = LEVELS[String(value).toLowerCase()] ?? null
      spans.push({ text, kind: 'level' })
      continue
    }
    if (isTimeKey(key)) {
      spans.push({ text, kind: 'timestamp' })
      continue
    }
    spans.push({
      text,
      kind: typeof value === 'number' || typeof value === 'boolean' ? 'number' : 'string',
    })
  }
  return { level, spans }
}

function isLevelKey(key: string): boolean {
  const lower = key.toLowerCase()
  return lower === 'level' || lower === 'severity' || lower === 'lvl' || lower === 'loglevel'
}

function isTimeKey(key: string): boolean {
  const lower = key.toLowerCase()
  return lower === 'time' || lower === 'timestamp' || lower === 'ts' || lower === '@timestamp'
}
