import { useState } from 'react'
import { useQueryClient } from '@tanstack/react-query'

import { ApiError } from '@/lib/api'

/**
 * The save half of a settings form.
 *
 * Every group saves the same way — try, report, refresh — so the shape lives here rather
 * than three times over, and "Saved." means the server said so rather than the form
 * having been submitted.
 */
export function useSaveSetting() {
  const queryClient = useQueryClient()
  const [busy, setBusy] = useState(false)
  const [saved, setSaved] = useState(false)
  const [error, setError] = useState('')

  const save = async (write: () => Promise<unknown>) => {
    setBusy(true)
    setSaved(false)
    setError('')
    try {
      await write()
      void queryClient.invalidateQueries({ queryKey: ['kubby-settings'] })
      setSaved(true)
    } catch (caught) {
      setError(caught instanceof ApiError ? caught.message : 'The settings could not be saved.')
    } finally {
      setBusy(false)
    }
  }

  return { busy, saved, error, save }
}
