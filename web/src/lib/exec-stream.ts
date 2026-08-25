export type ShellEvent =
  | { type: 'open' }
  | { type: 'stdout'; data: string }
  | { type: 'end' }
  | { type: 'error'; message: string }

export interface ShellHandle {
  send: (data: string) => void
  resize: (cols: number, rows: number) => void
  /** Puts a file into the session's working directory, under a relative path. */
  upload: (name: string, data: ArrayBuffer) => void
  close: () => void
}

/**
 * Opens an interactive session over a WebSocket.
 *
 * The session runs on the server: the browser sends keystrokes and window sizes, and
 * receives output. Nothing needs to be installed on the reader's machine, which is the
 * point — a browser cannot exec into a container by itself (ADR-064).
 */
export function openShellStream(path: string, onEvent: (event: ShellEvent) => void): ShellHandle {
  const scheme = window.location.protocol === 'https:' ? 'wss' : 'ws'
  const socket = new WebSocket(`${scheme}://${window.location.host}${path}`)
  let closedByCaller = false

  socket.onmessage = (message) => {
    if (typeof message.data !== 'string') return
    try {
      const frame: unknown = JSON.parse(message.data)
      if (typeof frame !== 'object' || frame === null || !('type' in frame)) return

      const value = frame as { type: string; data?: string }
      switch (value.type) {
        case 'open':
          onEvent({ type: 'open' })
          break
        case 'stdout':
          onEvent({ type: 'stdout', data: value.data ?? '' })
          break
        case 'end':
          onEvent({ type: 'end' })
          break
        case 'error':
          onEvent({ type: 'error', message: value.data ?? 'The session failed.' })
          break
      }
    } catch {
      // A frame that is not ours is not worth ending the session over.
    }
  }

  socket.onerror = () => {
    if (!closedByCaller) onEvent({ type: 'error', message: 'The session could not be opened.' })
  }

  socket.onclose = (event) => {
    if (closedByCaller || event.wasClean) {
      if (!closedByCaller) onEvent({ type: 'end' })
      return
    }
    onEvent({ type: 'error', message: 'The session closed unexpectedly.' })
  }

  const post = (payload: object) => {
    if (socket.readyState === WebSocket.OPEN) socket.send(JSON.stringify(payload))
  }

  return {
    send: (data) => post({ type: 'stdin', data }),
    resize: (cols, rows) => post({ type: 'resize', cols, rows }),
    upload: (name, data) => post({ type: 'upload', name, data: toBase64(data) }),
    close: () => {
      closedByCaller = true
      if (socket.readyState === WebSocket.OPEN || socket.readyState === WebSocket.CONNECTING) {
        socket.close()
      }
    },
  }
}

/** The path a pod's shell lives at. */
export function podShellPath(clusterId: string, namespace: string, pod: string, container?: string): string {
  const query = container ? `?container=${encodeURIComponent(container)}` : ''
  return (
    `/api/v1/clusters/${clusterId}/pod/${encodeURIComponent(namespace)}/${encodeURIComponent(pod)}/shell` + query
  )
}

/**
 * The path that attaches a container with a shell to a pod that has none.
 *
 * A distroless image has no shell on purpose. This brings one alongside it — and cannot
 * be undone while the pod lives, so it is never taken automatically (ADR-013 #4).
 */
export function debugShellPath(clusterId: string, namespace: string, pod: string, container?: string): string {
  const query = container ? `?container=${encodeURIComponent(container)}` : ''
  return (
    `/api/v1/clusters/${clusterId}/pod/${encodeURIComponent(namespace)}/${encodeURIComponent(pod)}/debug` + query
  )
}

/** The path a node's shell lives at. Opening one starts a privileged pod (ADR-064). */
export function nodeShellPath(clusterId: string, node: string): string {
  return `/api/v1/clusters/${clusterId}/node/${encodeURIComponent(node)}/shell`
}

/**
 * Bytes as base64, because the frames are JSON.
 *
 * Chunked rather than spread into one call: `String.fromCharCode(...bytes)` on a file of
 * any size overflows the argument limit, which shows up as a crash on exactly the large
 * charts this is for.
 */
function toBase64(buffer: ArrayBuffer): string {
  const bytes = new Uint8Array(buffer)
  const chunk = 0x8000
  let binary = ''

  for (let i = 0; i < bytes.length; i += chunk) {
    binary += String.fromCharCode(...bytes.subarray(i, i + chunk))
  }
  return btoa(binary)
}
