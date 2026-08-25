import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { debugShellPath, nodeShellPath, openShellStream, podShellPath, type ShellEvent } from './exec-stream'

/** A socket the test drives, standing in for the one the browser would open. */
class FakeSocket {
  static last: FakeSocket | null = null
  static readonly OPEN = 1

  readyState = 1
  sent: string[] = []
  onmessage: ((event: { data: unknown }) => void) | null = null
  onerror: (() => void) | null = null
  onclose: ((event: { wasClean: boolean }) => void) | null = null
  closed = false

  constructor(public url: string) {
    FakeSocket.last = this
  }

  send(data: string) {
    this.sent.push(data)
  }

  close() {
    this.closed = true
  }

  deliver(payload: unknown) {
    this.onmessage?.({ data: JSON.stringify(payload) })
  }
}

beforeEach(() => {
  vi.stubGlobal('WebSocket', FakeSocket)
})

afterEach(() => {
  vi.unstubAllGlobals()
  FakeSocket.last = null
})

describe('openShellStream', () => {
  const collect = () => {
    const events: ShellEvent[] = []
    const handle = openShellStream('/api/v1/shell', (event) => events.push(event))
    return { events, handle, socket: FakeSocket.last! }
  }

  it('reports each kind of frame the server sends', () => {
    const { events, socket } = collect()

    socket.deliver({ type: 'open' })
    socket.deliver({ type: 'stdout', data: 'hello' })
    socket.deliver({ type: 'end' })

    expect(events).toEqual([{ type: 'open' }, { type: 'stdout', data: 'hello' }, { type: 'end' }])
  })

  it('carries the error the server names rather than a generic one', () => {
    const { events, socket } = collect()

    socket.deliver({ type: 'error', data: 'no shell in container "app"' })

    expect(events).toEqual([{ type: 'error', message: 'no shell in container "app"' }])
  })

  it('ignores a frame it cannot read instead of ending the session', () => {
    const { events, socket } = collect()

    socket.onmessage?.({ data: 'not json' })
    socket.deliver({ type: 'stdout', data: 'still here' })

    expect(events).toEqual([{ type: 'stdout', data: 'still here' }])
  })

  it('sends keystrokes and window sizes as tagged frames', () => {
    const { handle, socket } = collect()

    handle.send('ls\n')
    handle.resize(120, 40)

    expect(socket.sent.map((frame) => JSON.parse(frame) as unknown)).toEqual([
      { type: 'stdin', data: 'ls\n' },
      { type: 'resize', cols: 120, rows: 40 },
    ])
  })

  it('says nothing when the reader closed it themselves', () => {
    const { events, handle, socket } = collect()

    handle.close()
    socket.onclose?.({ wasClean: false })

    expect(events).toEqual([])
    expect(socket.closed).toBe(true)
  })

  // A dropped session that reports nothing looks like an idle prompt, which is worse
  // than an error: the reader keeps typing into a socket that is gone.
  it('reports a session that dropped on its own', () => {
    const { events, socket } = collect()

    socket.onclose?.({ wasClean: false })

    expect(events).toEqual([{ type: 'error', message: 'The session closed unexpectedly.' }])
  })
})

describe('session paths', () => {
  it('leaves the container out when the server should choose', () => {
    expect(podShellPath('c1', 'payments', 'api-0')).toBe(
      '/api/v1/clusters/c1/pod/payments/api-0/shell',
    )
  })

  it('escapes names rather than pasting them into the path', () => {
    expect(podShellPath('c1', 'a/b', 'x y', 'side car')).toBe(
      '/api/v1/clusters/c1/pod/a%2Fb/x%20y/shell?container=side%20car',
    )
  })

  it('keeps the debug route separate from the ordinary shell', () => {
    expect(debugShellPath('c1', 'payments', 'api-0')).toContain('/debug')
    expect(nodeShellPath('c1', 'node-1')).toBe('/api/v1/clusters/c1/node/node-1/shell')
  })
})
