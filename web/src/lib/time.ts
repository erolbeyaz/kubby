/**
 * The server speaks UTC only (ADR-026). Conversion happens here, once, so a restart
 * timestamp is never read three hours off.
 */
export const DISPLAY_TIMEZONE = 'Europe/Istanbul'

const absoluteFormatter = new Intl.DateTimeFormat('en-GB', {
  timeZone: DISPLAY_TIMEZONE,
  year: 'numeric',
  month: '2-digit',
  day: '2-digit',
  hour: '2-digit',
  minute: '2-digit',
  second: '2-digit',
  hour12: false,
  timeZoneName: 'shortOffset',
})

/** Renders an absolute timestamp, always with its offset so it cannot be misread. */
export function formatAbsolute(iso: string): string {
  const date = new Date(iso)
  if (Number.isNaN(date.getTime())) return iso
  return absoluteFormatter.format(date)
}

/** Renders a duration the way kubectl renders its AGE column. */
export function formatAge(from: string | Date, now: Date = new Date()): string {
  const start = from instanceof Date ? from : new Date(from)
  if (Number.isNaN(start.getTime())) return '—'

  const seconds = Math.max(0, Math.floor((now.getTime() - start.getTime()) / 1000))
  if (seconds < 60) return `${seconds}s`

  const minutes = Math.floor(seconds / 60)
  if (minutes < 60) {
    const rest = seconds % 60
    return rest > 0 ? `${minutes}m${rest}s` : `${minutes}m`
  }

  const hours = Math.floor(minutes / 60)
  if (hours < 24) {
    const rest = minutes % 60
    return rest > 0 ? `${hours}h${rest}m` : `${hours}h`
  }

  const days = Math.floor(hours / 24)
  if (days < 365) {
    const rest = hours % 24
    return rest > 0 && days < 10 ? `${days}d${rest}h` : `${days}d`
  }

  const years = Math.floor(days / 365)
  const restDays = days % 365
  return restDays > 0 && years < 10 ? `${years}y${restDays}d` : `${years}y`
}
