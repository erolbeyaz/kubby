import { useId } from 'react'

/**
 * A labelled on/off control, with room for the sentence that says what it costs.
 *
 * The hint sits outside the label rather than inside it. Within the label it became part
 * of the control's accessible name, so a switch announced itself as its title followed by
 * a whole sentence of consequences — and could not be found by its own title at all.
 * Outside, linked by aria-describedby, it is read as what it is: a description.
 */
export function Switch({
  label,
  hint,
  checked,
  onChange,
}: {
  label: string
  hint?: string
  checked: boolean
  onChange: (value: boolean) => void
}) {
  const hintId = useId()

  return (
    <div className="flex flex-col">
      <label className="flex cursor-pointer items-start gap-2.5">
        <input
          type="checkbox"
          checked={checked}
          onChange={(event) => onChange(event.target.checked)}
          className="mt-0.5"
          {...(hint ? { 'aria-describedby': hintId } : {})}
        />
        <span style={{ fontSize: 'var(--text-secondary-size)', color: 'var(--text-primary)' }}>
          {label}
        </span>
      </label>

      {hint && (
        <span
          id={hintId}
          // Indented to sit under the label rather than under the box, which is where the
          // sentence belongs when it explains the label.
          className="pl-[1.55rem]"
          style={{ fontSize: 'var(--text-micro)', color: 'var(--text-muted)' }}
        >
          {hint}
        </span>
      )}
    </div>
  )
}
