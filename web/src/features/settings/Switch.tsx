/** A labelled on/off control, with room for the sentence that says what it costs. */
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
  return (
    <label className="flex cursor-pointer items-start gap-2.5">
      <input
        type="checkbox"
        checked={checked}
        onChange={(event) => onChange(event.target.checked)}
        className="mt-0.5"
      />
      <span className="flex flex-col">
        <span style={{ fontSize: 'var(--text-secondary-size)', color: 'var(--text-primary)' }}>
          {label}
        </span>
        {hint && (
          <span style={{ fontSize: 'var(--text-micro)', color: 'var(--text-muted)' }}>{hint}</span>
        )}
      </span>
    </label>
  )
}
