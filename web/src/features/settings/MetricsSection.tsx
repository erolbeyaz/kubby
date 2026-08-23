import { useState } from 'react'

import { Field, TextInput } from '@/components/Field'
import { api, type KubbySettings } from '@/lib/api'

import { SecretField } from './SecretField'
import { SettingsSection } from './SettingsSection'
import { Switch } from './Switch'
import { useSaveSetting } from './useSaveSetting'

/**
 * Where measurements over time come from.
 *
 * metrics-server reports what is happening now and keeps no history, so a chart over
 * time needs something that does. This is the connection; what is asked of it arrives
 * in phase 9.
 */
export function MetricsSection({ value }: { value: KubbySettings['metrics'] }) {
  const [form, setForm] = useState(value)
  const [password, setPassword] = useState('')
  const [clearPassword, setClearPassword] = useState(false)
  const { busy, saved, error, save } = useSaveSetting()

  return (
    <SettingsSection
      title="Metrics"
      description="Kubernetes keeps no history of its own measurements. Point Kubby at a Prometheus-compatible endpoint to see usage over time rather than only right now."
      busy={busy}
      saved={saved}
      error={error}
      onSave={() =>
        void save(() =>
          api.saveMetrics({
            ...form,
            ...(password ? { password } : {}),
            ...(clearPassword ? { clearPassword: true } : {}),
          }),
        )
      }
    >
      <Switch
        label="Read metrics from Prometheus"
        checked={form.enabled}
        onChange={(enabled) => setForm({ ...form, enabled })}
      />

      <Field label="Endpoint" hint="The base URL, reachable from Kubby itself.">
        {(id) => (
          <TextInput
            id={id}
            value={form.url}
            placeholder="https://prometheus.example.com"
            onChange={(event) => setForm({ ...form, url: event.target.value })}
          />
        )}
      </Field>

      <Field label="Username" hint="Optional. Basic authentication.">
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
        label="Password"
        stored={value.hasPassword}
        value={password}
        clear={clearPassword}
        onChange={setPassword}
        onClear={setClearPassword}
      />

      <Field
        label="Organisation header"
        hint="Optional. Sent as X-Scope-OrgID, which multi-tenant setups such as Mimir require."
      >
        {(id) => (
          <TextInput
            id={id}
            value={form.organization ?? ''}
            onChange={(event) => setForm({ ...form, organization: event.target.value })}
          />
        )}
      </Field>

      <Switch
        label="Skip TLS verification"
        hint="Only for an endpoint whose certificate Kubby cannot verify. It turns off the check that the endpoint is who it claims to be."
        checked={form.insecureSkipVerify}
        onChange={(insecureSkipVerify) => setForm({ ...form, insecureSkipVerify })}
      />
    </SettingsSection>
  )
}
