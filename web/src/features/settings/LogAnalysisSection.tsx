import { useState } from 'react'

import { Field, Select, TextInput } from '@/components/Field'
import { api, type KubbySettings } from '@/lib/api'

import { SettingsSection } from './SettingsSection'
import { useSaveSetting } from './useSaveSetting'

type LogAnalysis = KubbySettings['logAnalysis']
type LogRule = LogAnalysis['rules'][number]

const CLASSES = ['auth', 'unreachable', 'timeout', 'generic'] as const

/**
 * What counts as a problem in a log line.
 *
 * Editable because the list that ships cannot know what a given stack produces. The
 * named rules exist to turn a match into a sentence worth reading — which database,
 * which user, which address — while the generic one at the end is what finds the
 * problems nobody wrote a rule for. Deleting that last rule is the one edit that
 * quietly narrows this feature to what somebody already anticipated.
 */
export function LogAnalysisSection({ value }: { value: LogAnalysis }) {
  const [form, setForm] = useState<LogAnalysis>(value)
  const { busy, saved, error, save } = useSaveSetting()

  const patch = (at: number, change: Partial<LogRule>) =>
    setForm({
      ...form,
      rules: form.rules.map((rule, index) => (index === at ? { ...rule, ...change } : rule)),
    })

  const enabled = form.rules.filter((rule) => !rule.disabled).length

  return (
    <SettingsSection
      title="Log analysis"
      description="A pod can be Running and Ready while its log says it cannot reach its database. These are the phrases Kubby looks for, and how insistent a pod has to be before the list says anything."
      busy={busy}
      saved={saved}
      error={error}
      onSave={() => void save(() => api.saveLogAnalysis(form))}
    >
      <div className="grid gap-4 sm:grid-cols-2">
        <Field
          label="Window"
          hint="How far back each sweep looks, in minutes."
        >
          {(id) => (
            <TextInput
              id={id}
              type="number"
              value={String(form.windowMinutes ?? 15)}
              onChange={(event) =>
                setForm({ ...form, windowMinutes: Number(event.target.value) || 0 })
              }
            />
          )}
        </Field>
        <Field
          label="Minimum lines"
          // A retry that succeeded on the second attempt logged one failure. Marking
          // that is how a list fills with marks nobody reads.
          hint="How many matching lines in the window before a pod is worth marking."
        >
          {(id) => (
            <TextInput
              id={id}
              type="number"
              value={String(form.minCount ?? 3)}
              onChange={(event) => setForm({ ...form, minCount: Number(event.target.value) || 0 })}
            />
          )}
        </Field>
      </div>

      <details>
        <summary
          className="cursor-pointer select-none"
          style={{ fontSize: 'var(--text-micro)', color: 'var(--text-secondary)' }}
        >
          Document fields — where in a log document to look
        </summary>
        <p className="mt-1 mb-2" style={{ fontSize: 'var(--text-micro)', color: 'var(--text-muted)' }}>
          The defaults match Fluent Bit's Kubernetes filter. An ECS pipeline writes{' '}
          <span className="font-mono">message</span> and{' '}
          <span className="font-mono">kubernetes.pod.name</span> instead. The connection test on
          a cluster's log source shows one whole document, which is where to read these off.
        </p>
        <div className="grid gap-3 sm:grid-cols-2">
          {(
            [
              ['message', 'Message', 'log'],
              ['pod', 'Pod name', 'kubernetes.pod_name'],
              ['namespace', 'Namespace', 'kubernetes.namespace_name'],
              ['container', 'Container', 'kubernetes.container_name'],
              ['timestamp', 'Timestamp', '@timestamp'],
            ] as const
          ).map(([key, label, placeholder]) => (
            <Field key={key} label={label}>
              {(id) => (
                <TextInput
                  id={id}
                  value={form.fields[key] ?? ''}
                  placeholder={placeholder}
                  onChange={(event) =>
                    setForm({ ...form, fields: { ...form.fields, [key]: event.target.value } })
                  }
                />
              )}
            </Field>
          ))}
        </div>
      </details>

      <div className="flex flex-col gap-2">
        <p style={{ fontSize: 'var(--text-micro)', color: 'var(--text-muted)' }}>
          {enabled} of {form.rules.length} rules enabled. A rule is matched against the message
          field; any one of its phrases is a hit, and the first rule that matches names the
          finding — so the specific ones come before the general one.
        </p>

        {form.rules.map((rule, index) => (
          <div
            key={`${rule.name}-${index}`}
            className="flex flex-col gap-2 border p-2"
            style={{
              borderRadius: 'var(--radius-sharp)',
              borderColor: 'var(--border-subtle)',
              opacity: rule.disabled ? 0.55 : 1,
            }}
          >
            <div className="flex flex-wrap items-center gap-2">
              <TextInput
                aria-label={`Rule ${index + 1} name`}
                value={rule.name}
                onChange={(event) => patch(index, { name: event.target.value })}
              />
              <Select
                aria-label={`Rule ${index + 1} class`}
                value={rule.class}
                onChange={(event) => patch(index, { class: event.target.value })}
              >
                {CLASSES.map((name) => (
                  <option key={name} value={name}>
                    {name}
                  </option>
                ))}
              </Select>
              <label
                className="ml-auto flex items-center gap-1.5"
                style={{ fontSize: 'var(--text-micro)', color: 'var(--text-secondary)' }}
              >
                <input
                  type="checkbox"
                  checked={!rule.disabled}
                  onChange={(event) => patch(index, { disabled: !event.target.checked })}
                />
                enabled
              </label>
            </div>

            <TextInput
              aria-label={`Rule ${index + 1} phrases`}
              value={rule.match.join(' | ')}
              placeholder="phrase | another phrase"
              onChange={(event) =>
                patch(index, { match: event.target.value.split('|').map((part) => part.trim()) })
              }
            />
          </div>
        ))}

        <div className="flex gap-2">
          <button
            type="button"
            onClick={() =>
              setForm({
                ...form,
                rules: [...form.rules, { name: '', class: 'generic', match: [] }],
              })
            }
            className="border px-2 py-1 transition-colors hover:bg-[var(--bg-hover)]"
            style={{
              borderRadius: 'var(--radius-sharp)',
              borderColor: 'var(--border-default)',
              fontSize: 'var(--text-micro)',
              color: 'var(--text-secondary)',
            }}
          >
            Add a rule
          </button>
          <button
            type="button"
            onClick={() => setForm(value)}
            className="border px-2 py-1 transition-colors hover:bg-[var(--bg-hover)]"
            style={{
              borderRadius: 'var(--radius-sharp)',
              borderColor: 'var(--border-default)',
              fontSize: 'var(--text-micro)',
              color: 'var(--text-muted)',
            }}
          >
            Discard changes
          </button>
        </div>
      </div>
    </SettingsSection>
  )
}
