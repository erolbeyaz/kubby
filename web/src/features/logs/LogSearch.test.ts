import { describe, expect, it } from 'vitest'

import { matchesFor, type SearchOptions } from './LogSearch'

const lines = [
  { id: 1, text: 'GET /health 200' },
  { id: 2, text: 'get /orders 500' },
  { id: 3, text: 'connection refused' },
]

const search = (patch: Partial<SearchOptions>): SearchOptions => ({
  term: '',
  matchCase: false,
  regex: false,
  onlyMatching: false,
  ...patch,
})

describe('matchesFor', () => {
  it('ignores case by default', () => {
    expect(matchesFor(lines, search({ term: 'GET' }))).toEqual([1, 2])
  })

  // A log search is often for a request id or a stack frame, where the wrong case is the
  // difference between one match and two thousand.
  it('respects case when asked', () => {
    expect(matchesFor(lines, search({ term: 'GET', matchCase: true }))).toEqual([1])
  })

  it('matches a regular expression', () => {
    expect(matchesFor(lines, search({ term: '\\d{3}$', regex: true }))).toEqual([1, 2])
  })

  it('respects case in a regular expression too', () => {
    expect(matchesFor(lines, search({ term: '^GET', regex: true, matchCase: true }))).toEqual([1])
  })

  // An invalid expression is a half-typed one. Shouting at every keystroke while someone
  // types "\d{" would make the field unusable.
  it('treats a half-typed expression as no match rather than an error', () => {
    expect(matchesFor(lines, search({ term: '\\d{', regex: true }))).toEqual([])
  })

  it('matches nothing for an empty term', () => {
    expect(matchesFor(lines, search({ term: '' }))).toEqual([])
  })
})
