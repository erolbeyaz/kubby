import { useEffect, useState } from 'react'

/**
 * A clock that re-renders its caller once a second while it is needed.
 *
 * Ages under ten minutes are shown to the second, and a second-resolution number that
 * does not move is worse than no number: during an incident it is exactly the digit
 * being watched. The ticker stops as soon as nothing on screen is recent, so a list of
 * day-old objects costs nothing.
 */
export function useTicker(active: boolean): Date {
  const [now, setNow] = useState(() => new Date())

  useEffect(() => {
    if (!active) return

    const timer = setInterval(() => setNow(new Date()), 1_000)
    return () => clearInterval(timer)
  }, [active])

  return now
}
