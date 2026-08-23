import { stringify } from 'yaml'

/**
 * Renders an object as YAML, or as JSON if it will not render.
 *
 * `lineWidth: 0` because a manifest wrapped at eighty columns is a manifest nobody can
 * diff: the wrap moves when a value changes length, and every wrapped line reads as
 * changed.
 */
export function toYaml(object: unknown): string {
  try {
    return stringify(object, { indent: 2, lineWidth: 0 })
  } catch {
    return JSON.stringify(object, null, 2)
  }
}
