/**
 * Reading a pod spec for what a detail panel shows: images and where they come from,
 * the ports a container listens on, what it asked for and what it may use, and the
 * volumes it mounts.
 *
 * An image reference names a registry only when it has to, so `nginx:1.27` and
 * `registry.internal/team/api:1.4` look alike while coming from opposite ends of the
 * network. In an environment where nodes may only pull from a mirror, that is precisely
 * the question being asked of a pod, and the answer is not in the string as written.
 */

export interface ImageOrigin {
  /** The registry host the reference resolves to. */
  host: string
  /** True when the reference never named it — Docker Hub is the default, not a choice. */
  implicit: boolean
}

const DOCKER_HUB = 'docker.io'

/**
 * The registry a reference resolves to, by the same rule a container runtime uses: the
 * first path segment is a host only when it looks like one — it carries a dot or a port,
 * or it is localhost. Everything else belongs to Docker Hub.
 */
export function registryOf(image: string): ImageOrigin | null {
  const reference = image.trim()
  if (!reference) return null

  const slash = reference.indexOf('/')
  if (slash === -1) return { host: DOCKER_HUB, implicit: true }

  const head = reference.slice(0, slash)
  if (head === 'localhost' || head.includes('.') || head.includes(':')) {
    return { host: head, implicit: false }
  }
  return { host: DOCKER_HUB, implicit: true }
}

/**
 * Where a running container's image was actually pulled from, when that is worth saying.
 *
 * The kubelet records the resolved reference, which is the only place a digest and an
 * implicit registry ever appear. It earns its line when the spec left the registry
 * unsaid, or when the two disagree — a mirror rewriting the pull is invisible otherwise.
 * A bare `sha256:…` names no origin at all and is left out.
 */
export function pulledFrom(image: string, imageID: string): string | null {
  const resolved = imageID.trim()
  if (!resolved || !resolved.includes('/')) return null

  const wanted = registryOf(image)
  const actual = registryOf(resolved)
  if (!wanted || !actual) return null

  if (wanted.implicit || wanted.host !== actual.host) return resolved
  return null
}

export interface ContainerPort {
  port: number
  protocol: string
  /** A port's name is what a Service targets, so it is worth showing beside the number. */
  name: string
}

export interface ContainerMount {
  path: string
  volume: string
  readOnly: boolean
}

/** One container as the detail panel shows it, whichever kind it was reached through. */
export interface ContainerSpec {
  name: string
  image: string
  pullPolicy: string
  /** Init containers run and exit before the others; they are not what the pod runs. */
  init: boolean
  ports: ContainerPort[]
  requests: Record<string, string>
  limits: Record<string, string>
  mounts: ContainerMount[]
}

/** A volume the pod carries, named by what it actually is rather than by its key. */
export interface PodVolume {
  name: string
  type: string
  /** What backs it — the claim, the config map, the path on the host. */
  source: string
}

/**
 * The pod spec a kind creates or is.
 *
 * A workload's containers live in the template it will stamp out, not on the workload
 * itself, and a CronJob keeps that template two levels down.
 */
export function podSpecOf(kind: string, spec: Record<string, unknown>): Record<string, unknown> | null {
  if (kind === 'Pod') return spec

  if (kind === 'CronJob') {
    const jobTemplate = asRecord(spec.jobTemplate)
    const job = jobTemplate && asRecord(jobTemplate.spec)
    return job ? asRecord(asRecord(job.template)?.spec) : null
  }

  const template = asRecord(spec.template)
  return template ? asRecord(template.spec) : null
}

/** The containers of a pod spec, application containers first and init ones marked. */
export function containersOf(podSpec: Record<string, unknown> | null): ContainerSpec[] {
  if (!podSpec) return []

  const read = (key: string, init: boolean): ContainerSpec[] =>
    listOf(podSpec[key])
      .map((container) => {
        const resources = asRecord(container.resources) ?? {}
        return {
          name: str(container.name),
          image: str(container.image),
          pullPolicy: str(container.imagePullPolicy),
          init,
          ports: portsOf(container.ports),
          requests: quantities(resources.requests),
          limits: quantities(resources.limits),
          mounts: mountsOf(container.volumeMounts),
        }
      })
      .filter((container) => container.image !== '')

  return [...read('containers', false), ...read('initContainers', true)]
}

/**
 * The pod's volumes.
 *
 * A volume's kind is the one key beside its name, which is how the API expresses a
 * union; reading it that way keeps every volume type working, including ones added
 * after this was written.
 */
export function volumesOf(podSpec: Record<string, unknown> | null): PodVolume[] {
  if (!podSpec) return []

  return listOf(podSpec.volumes)
    .map((volume) => {
      const [key] = Object.keys(volume).filter((k) => k !== 'name')
      const body = key ? asRecord(volume[key]) : null

      return {
        name: str(volume.name),
        type: key ? key.charAt(0).toUpperCase() + key.slice(1) : '',
        source: body ? sourceOf(body) : '',
      }
    })
    .filter((volume) => volume.name !== '')
}

/** `cpu 10m · memory 32Mi`, or nothing at all when a container asked for nothing. */
export function formatQuantities(quantities: Record<string, string>): string {
  const order = ['cpu', 'memory']
  return Object.entries(quantities)
    .sort(([a], [b]) => rank(order, a) - rank(order, b) || a.localeCompare(b))
    .map(([name, value]) => `${name} ${value}`)
    .join(' · ')
}

function rank(order: string[], key: string): number {
  const at = order.indexOf(key)
  return at === -1 ? order.length : at
}

function sourceOf(body: Record<string, unknown>): string {
  for (const key of ['claimName', 'secretName', 'name', 'path', 'medium']) {
    const value = str(body[key])
    if (value) return value
  }
  return ''
}

function portsOf(value: unknown): ContainerPort[] {
  return listOf(value)
    .map((port) => ({
      port: typeof port.containerPort === 'number' ? port.containerPort : 0,
      protocol: str(port.protocol) || 'TCP',
      name: str(port.name),
    }))
    .filter((port) => port.port > 0)
}

function mountsOf(value: unknown): ContainerMount[] {
  return listOf(value)
    .map((mount) => ({
      path: str(mount.mountPath),
      volume: str(mount.name),
      readOnly: mount.readOnly === true,
    }))
    .filter((mount) => mount.path !== '')
}

function quantities(value: unknown): Record<string, string> {
  const record = asRecord(value)
  if (!record) return {}

  const out: Record<string, string> = {}
  for (const [key, quantity] of Object.entries(record)) {
    const text = typeof quantity === 'number' ? String(quantity) : str(quantity)
    if (text) out[key] = text
  }
  return out
}

function listOf(value: unknown): Record<string, unknown>[] {
  return (Array.isArray(value) ? value : [])
    .map(asRecord)
    .filter((item): item is Record<string, unknown> => item !== null)
}

function str(value: unknown): string {
  return typeof value === 'string' ? value : ''
}

function asRecord(value: unknown): Record<string, unknown> | null {
  return value !== null && typeof value === 'object' && !Array.isArray(value)
    ? (value as Record<string, unknown>)
    : null
}
