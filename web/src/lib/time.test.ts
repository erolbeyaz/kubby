import { describe, expect, it } from 'vitest'

import { formatAbsolute, formatAge } from './time'

// ADR-026: the server speaks UTC, the team reads Europe/Istanbul. A silent three hour
// skew on a restart timestamp causes misdiagnosis, so the boundary is pinned here too.
describe('formatAbsolute', () => {
  it('renders a UTC instant in Europe/Istanbul with its offset', () => {
    const rendered = formatAbsolute('2026-08-22T10:15:30Z')

    expect(rendered).toContain('22/08/2026')
    expect(rendered).toContain('13:15:30')
    expect(rendered).toMatch(/GMT\+3/)
  })

  it('returns the input unchanged when it is not a valid timestamp', () => {
    expect(formatAbsolute('not-a-date')).toBe('not-a-date')
  })
})

describe('formatAge', () => {
  const now = new Date('2026-08-22T12:00:00Z')

  it.each([
    ['2026-08-22T11:59:15Z', '45s'],
    ['2026-08-22T11:58:30Z', '1m30s'],
    ['2026-08-22T11:55:00Z', '5m'],
    ['2026-08-22T10:30:00Z', '1h30m'],
    ['2026-08-22T07:00:00Z', '5h'],
    ['2026-08-20T10:00:00Z', '2d2h'],
    ['2026-07-23T12:00:00Z', '30d'],
  ])('renders %s as %s', (iso, expected) => {
    expect(formatAge(iso, now)).toBe(expected)
  })

  it('does not render a negative age when the clock is skewed', () => {
    expect(formatAge('2026-08-22T12:00:30Z', now)).toBe('0s')
  })

  it('returns a placeholder for an unparseable timestamp', () => {
    expect(formatAge('nonsense', now)).toBe('—')
  })
})
