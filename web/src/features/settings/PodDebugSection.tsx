import { useState } from 'react'

import { Field, TextInput } from '@/components/Field'
import { api, type KubbySettings } from '@/lib/api'

import { SettingsSection } from './SettingsSection'
import { useSaveSetting } from './useSaveSetting'

/**
 * The image brought alongside a container that has no shell of its own.
 *
 * A distroless image is distroless deliberately, and the answer is not to rebuild it but
 * to attach a container that does have a shell. Like the node shell's, this is a whole
 * reference so a node that may only pull from one registry is covered by one field.
 */
export function PodDebugSection({ value }: { value: KubbySettings['podDebug'] }) {
  const [form, setForm] = useState(value)
  const { busy, saved, error, save } = useSaveSetting()

  return (
    <SettingsSection
      title="Debug container"
      description="Opening a shell in a pod with no shell offers to attach one of these instead. It shares the pod's processes, network and files — and cannot be removed while the pod lives."
      busy={busy}
      saved={saved}
      error={error}
      onSave={() => void save(() => api.savePodDebug(form))}
    >
      <Field
        label="Image"
        hint="The whole reference the kubelet will pull. It must be reachable from the nodes, not from Kubby."
      >
        {(id) => (
          <TextInput
            id={id}
            value={form.image}
            placeholder="harbor.example.com/dockerhub/library/busybox:1.36"
            onChange={(event) => setForm({ ...form, image: event.target.value })}
          />
        )}
      </Field>
    </SettingsSection>
  )
}
