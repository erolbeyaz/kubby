import { useState } from 'react'

import { Field, TextInput } from '@/components/Field'
import { api, type KubbySettings } from '@/lib/api'

import { SettingsSection } from './SettingsSection'
import { Switch } from './Switch'
import { useSaveSetting } from './useSaveSetting'

/**
 * Where a node shell's pod comes from.
 *
 * The image is a whole reference rather than a registry prefix: a node permitted to pull
 * only from its own registry needs the entire path rewritten, and one field covers a
 * proxy cache, a mirror and a self-built image without assuming the shape of any of them.
 */
export function NodeShellSection({ value }: { value: KubbySettings['nodeShell'] }) {
  const [form, setForm] = useState(value)
  const { busy, saved, error, save } = useSaveSetting()

  return (
    <SettingsSection
      title="Node shell"
      description="A node shell runs a privileged pod on the node and enters its namespaces. That is root on the machine, so it stays off until it is turned on."
      busy={busy}
      saved={saved}
      error={error}
      onSave={() => void save(() => api.saveNodeShell(form))}
    >
      <Switch
        label="Allow node shells"
        hint="Admins only, and never on a cluster locked read-only."
        checked={form.enabled}
        onChange={(enabled) => setForm({ ...form, enabled })}
      />

      <Field
        label="Image"
        hint="The whole reference the kubelet will pull. It must be reachable from the nodes, not from Kubby."
      >
        {(id) => (
          <TextInput
            id={id}
            value={form.image}
            placeholder="harbor.example.com/dockerhub/library/alpine:3.20"
            onChange={(event) => setForm({ ...form, image: event.target.value })}
          />
        )}
      </Field>

      <Field label="Namespace" hint="Where the temporary pod is created.">
        {(id) => (
          <TextInput
            id={id}
            value={form.namespace}
            onChange={(event) => setForm({ ...form, namespace: event.target.value })}
          />
        )}
      </Field>

      <Field
        label="Image pull secret"
        hint="Optional. The name of a secret that already exists in that namespace — Kubby stores no registry credentials of its own."
      >
        {(id) => (
          <TextInput
            id={id}
            value={form.pullSecret ?? ''}
            onChange={(event) => setForm({ ...form, pullSecret: event.target.value })}
          />
        )}
      </Field>
    </SettingsSection>
  )
}
