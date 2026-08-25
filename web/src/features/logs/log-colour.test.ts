import { describe, expect, it } from 'vitest'

import { parseLine } from './log-colour'

const kinds = (line: string) => parseLine(line).spans.map((span) => span.kind)
const textOf = (line: string, kind: string) =>
  parseLine(line)
    .spans.filter((span) => span.kind === kind)
    .map((span) => span.text)

describe('parseLine', () => {
  // Structured logs are the common case in a cluster, and reading them as flat text is
  // most of why logs are hard to scan.
  it('splits a JSON log into keys and values', () => {
    const line = '{"time":"2026-08-23T10:00:00Z","level":"error","msg":"connection refused","attempt":3}'

    expect(parseLine(line).level).toBe('error')
    expect(textOf(line, 'key')).toEqual(['time', 'level', 'msg', 'attempt'])
    expect(textOf(line, 'timestamp')).toEqual(['2026-08-23T10:00:00Z'])
    expect(textOf(line, 'number')).toEqual(['3'])
  })

  it('recognises a level word in plain text', () => {
    expect(parseLine('2026-08-23T10:00:00Z WARN disk almost full').level).toBe('warn')
    expect(kinds('2026-08-23T10:00:00Z WARN disk almost full')).toContain('timestamp')
  })

  // One container may write JSON while its sidecar writes plain text, and both end up in
  // the same view, so the format is decided per line rather than for the stream.
  it('falls back to plain text for anything else', () => {
    const result = parseLine('starting up')

    expect(result.level).toBeNull()
    expect(result.spans).toEqual([{ text: 'starting up', kind: 'plain' }])
  })

  it('does not treat a broken JSON line as structured', () => {
    const result = parseLine('{"level":"error", oops}')

    expect(result.spans.every((span) => span.kind === 'plain' || span.kind === 'level')).toBe(true)
  })

  it('maps the level synonyms applications actually use', () => {
    expect(parseLine('{"severity":"FATAL","msg":"x"}').level).toBe('error')
    expect(parseLine('{"lvl":"trace","msg":"x"}').level).toBe('debug')
    expect(parseLine('{"level":"notice","msg":"x"}').level).toBe('info')
  })

  it('leaves an empty line alone', () => {
    expect(parseLine('').spans).toEqual([])
  })
})
