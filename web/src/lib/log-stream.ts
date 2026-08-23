export interface LogOptions {
  clusterId: string
  namespace: string
  pod: string
  container?: string
  follow?: boolean
  previous?: boolean
  timestamps?: boolean
  tail?: number
  since?: number
}

export type LogEvent =
  | { type: 'open'; container: string }
  | { type: 'line'; line: string }
  | { type: 'end' }
  | { type: 'error'; message: string }

/**
 * Opens a pod's log over a WebSocket.
 *
 * A log view is a conversation rather than a download: the reader switches container,
 * asks for the instance that died, or stops following. Each of those would otherwise be
 * a fresh request and a fresh place in the log.
 */
export function openLogStream(options: LogOptions, onEvent: (event: LogEvent) => void): () => void {
  const query = new URLSearchParams()
  if (options.container) query.set('container', options.container)
  if (options.follow) query.set('follow', 'true')
  if (options.previous) query.set('previous', 'true')
  if (options.timestamps) query.set('timestamps', 'true')
  if (options.tail) query.set('tail', String(options.tail))
  if (options.since) query.set('since', String(options.since))

  const scheme = window.location.protocol === 'https:' ? 'wss' : 'ws'
  const path =
    `/api/v1/clusters/${options.clusterId}` +
    `/pod/${encodeURIComponent(options.namespace)}/${encodeURIComponent(options.pod)}/logs`

  const socket = new WebSocket(`${scheme}://${window.location.host}${path}?${query}`)
  let closedByCaller = false

  socket.onmessage = (message) => {
    const event = parse(message.data)
    if (event) onEvent(event)
  }

  socket.onerror = () => {
    if (!closedByCaller) onEvent({ type: 'error', message: 'The log connection failed.' })
  }

  socket.onclose = (event) => {
    if (closedByCaller || event.wasClean) return
    // A dropped follow is the normal case after a pod restarts or a proxy times out, and
    // saying so is better than the view quietly stopping and looking idle.
    onEvent({ type: 'error', message: 'The log connection closed unexpectedly.' })
  }

  return () => {
    closedByCaller = true
    if (socket.readyState === WebSocket.OPEN || socket.readyState === WebSocket.CONNECTING) {
      socket.close()
    }
  }
}

function parse(data: unknown): LogEvent | null {
  if (typeof data !== 'string') return null

  try {
    const value: unknown = JSON.parse(data)
    if (typeof value !== 'object' || value === null || !('type' in value)) return null
    return value as LogEvent
  } catch {
    return null
  }
}
