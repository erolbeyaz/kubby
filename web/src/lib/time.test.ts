import { describe, expect, it } from 'vitest'

import { formatAge, isLiveAge } from './time'

const at = (iso: string) => new Date(iso)
const NOW = at('2026-08-24T12:00:00Z')

describe('formatAge', () => {
  // Below ten minutes the seconds are the difference between "this just happened" and
  // "this happened a while ago", which is the reason the column is watched at all.
  it('shows seconds under ten minutes', () => {
    expect(formatAge('2026-08-24T11:56:42Z', NOW)).toBe('3m18s')
    expect(formatAge('2026-08-24T11:59:30Z', NOW)).toBe('30s')
  })

  // Above it they are a digit that changes every second and says nothing.
  it('drops seconds at ten minutes and beyond', () => {
    expect(formatAge('2026-08-24T11:49:42Z', NOW)).toBe('10m')
    expect(formatAge('2026-08-24T11:30:20Z', NOW)).toBe('29m')
  })

  it('keeps the coarser units unchanged', () => {
    expect(formatAge('2026-08-24T09:30:00Z', NOW)).toBe('2h30m')
    expect(formatAge('2026-08-20T12:00:00Z', NOW)).toBe('4d')
  })
})

describe('isLiveAge', () => {
  it('is true only while an age still shows seconds', () => {
    expect(isLiveAge('2026-08-24T11:56:00Z', NOW)).toBe(true)
    expect(isLiveAge('2026-08-24T11:49:00Z', NOW)).toBe(false)
  })

  it('is false for something unreadable rather than throwing', () => {
    expect(isLiveAge('not a date', NOW)).toBe(false)
  })
})
