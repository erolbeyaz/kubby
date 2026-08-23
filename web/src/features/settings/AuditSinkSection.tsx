import { useState } from 'react'

import { Field, Select, TextInput } from '@/components/Field'
import { api, type KubbySettings } from '@/lib/api'

import { SecretField } from './SecretField'
import { SettingsSection } from './SettingsSection'
import { Switch } from './Switch'
import { useSaveSetting } from './useSaveSetting'

const KINDS = [
  { value: 'elasticsearch', label: 'Elasticsearch' },
  { value: 'loki', label: 'Loki' },
  { value: 'syslog', label: 'Syslog (QRadar, ArcSight)' },
  { value: 'http', label: 'HTTP endpoint' },
]

/**
 * Where the audit trail is copied to.
 *
 * Kubby's own trail is never disabled; this is an additional destination, because a
 * record that lives only inside the tool being audited is not much of a record. Whoever
 * can turn Kubby's auditing into someone else's problem should be exactly the person who
 * cannot quietly turn it off — so changing this is itself audited.
 */
export function AuditSinkSection({ value }: { value: KubbySettings['auditSink'] }) {
  const [form, setForm] = useState({ ...value, kind: value.kind || 'elasticsearch' })
  const [token, setToken] = useState('')
  const [clearToken, setClearToken] = useState(false)
  const { busy, saved, error, save } = useSaveSetting()

  return (
    <SettingsSection
      title="Audit shipping"
      description="Kubby always records its own audit trail. This copies it somewhere else as well, so the record outlives the installation that wrote it."
      busy={busy}
      saved={saved}
      error={error}
      onSave={() =>
        void save(() =>
          api.saveAuditSink({
            ...form,
            ...(token ? { token } : {}),
            ...(clearToken ? { clearToken: true } : {}),
          }),
        )
      }
    >
      <Switch
        label="Ship the audit trail"
        checked={form.enabled}
        onChange={(enabled) => setForm({ ...form, enabled })}
      />

      <Field label="Destination">
        {(id) => (
          <Select
            id={id}
            value={form.kind}
            onChange={(event) => setForm({ ...form, kind: event.target.value })}
          >
            {KINDS.map((kind) => (
              <option key={kind.value} value={kind.value}>
                {kind.label}
              </option>
            ))}
          </Select>
        )}
      </Field>

      <Field label="Endpoint" hint="Reachable from Kubby itself.">
        {(id) => (
          <TextInput
            id={id}
            value={form.url}
            placeholder="https://elastic.example.com:9200"
            onChange={(event) => setForm({ ...form, url: event.target.value })}
          />
        )}
      </Field>

      <Field label="Index or stream" hint="Optional. Where the records are written on the far side.">
        {(id) => (
          <TextInput
            id={id}
            value={form.index ?? ''}
            placeholder="kubby-audit"
            onChange={(event) => setForm({ ...form, index: event.target.value })}
          />
        )}
      </Field>

      <Field label="Username" hint="Optional.">
        {(id) => (
          <TextInput
            id={id}
            value={form.username ?? ''}
            autoComplete="off"
            onChange={(event) => setForm({ ...form, username: event.target.value })}
          />
        )}
      </Field>

      <SecretField
        label="Password or API key"
        stored={value.hasToken}
        value={token}
        clear={clearToken}
        onChange={setToken}
        onClear={setClearToken}
      />

      <Switch
        label="Skip TLS verification"
        hint="Only for an endpoint whose certificate Kubby cannot verify."
        checked={form.insecureSkipVerify}
        onChange={(insecureSkipVerify) => setForm({ ...form, insecureSkipVerify })}
      />
    </SettingsSection>
  )
}
