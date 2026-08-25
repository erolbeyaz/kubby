/**
 * A toolbar control.
 *
 * Icons rather than words: this bar carries eight controls above a stream of text, and
 * eight labelled buttons would take more room than the log they act on. Every one keeps
 * a title and an accessible name, so nothing is only a picture.
 */
export function IconButton({
  label,
  path,
  onClick,
  active = false,
  pressed,
  disabled = false,
  accent = false,
}: {
  label: string
  path: string
  onClick: () => void
  active?: boolean
  pressed?: boolean
  disabled?: boolean
  /** The one button that commits, drawn as the accent so it is not one of a row. */
  accent?: boolean
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      title={label}
      aria-label={label}
      disabled={disabled}
      {...(pressed === undefined ? {} : { 'aria-pressed': pressed })}
      className={`tool-button${active ? ' tool-button--active' : ''}${accent ? ' tool-button--accent' : ''}`}
    >
      <svg
        width="17"
        height="17"
        viewBox="0 0 16 16"
        fill="none"
        stroke="currentColor"
        strokeWidth="1.4"
        strokeLinecap="round"
        strokeLinejoin="round"
        aria-hidden="true"
      >
        <path d={path} />
      </svg>
    </button>
  )
}

export function IconToggle({
  label,
  path,
  active,
  onChange,
}: {
  label: string
  path: string
  active: boolean
  onChange: (value: boolean) => void
}) {
  return (
    <IconButton label={label} path={path} active={active} pressed={active} onClick={() => onChange(!active)} />
  )
}
