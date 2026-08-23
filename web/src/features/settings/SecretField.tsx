import { Field, TextInput } from '@/components/Field'

interface SecretFieldProps {
  label: string
  /** Whether one is already stored. The value itself never reaches the browser. */
  stored: boolean
  value: string
  clear: boolean
  onChange: (value: string) => void
  onClear: (clear: boolean) => void
}

/**
 * A credential field for something already stored.
 *
 * Leaving it empty keeps what is there, and that has to be said rather than assumed: a
 * form that wipes a password because the field was not retyped is a form that loses
 * passwords, and whoever saved it has no way to know until something stops working.
 */
export function SecretField({ label, stored, value, clear, onChange, onClear }: SecretFieldProps) {
  return (
    <Field
      label={label}
      hint={
        stored
          ? 'One is stored. Leave this empty to keep it, or type a new one to replace it.'
          : 'Nothing is stored yet.'
      }
    >
      {(id) => (
        <div className="flex flex-col gap-1.5">
          <TextInput
            id={id}
            type="password"
            value={value}
            autoComplete="new-password"
            disabled={clear}
            placeholder={stored ? '••••••••' : ''}
            onChange={(event) => onChange(event.target.value)}
          />

          {stored && (
            <label className="flex cursor-pointer items-center gap-2">
              <input
                type="checkbox"
                checked={clear}
                onChange={(event) => {
                  onClear(event.target.checked)
                  if (event.target.checked) onChange('')
                }}
              />
              <span style={{ fontSize: 'var(--text-micro)', color: 'var(--text-muted)' }}>
                Remove the stored {label.toLowerCase()}
              </span>
            </label>
          )}
        </div>
      )}
    </Field>
  )
}
