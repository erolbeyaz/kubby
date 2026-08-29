import { describe, expect, it } from 'vitest'

import { containersOf, formatQuantities, podSpecOf, pulledFrom, registryOf, volumesOf } from './containers'

describe('registryOf', () => {
  // The case the panel exists for: nothing in the string says where this comes from.
  it('resolves a bare name to Docker Hub and says the reference never named it', () => {
    expect(registryOf('nginx:1.27')).toEqual({ host: 'docker.io', implicit: true })
    expect(registryOf('grafana/grafana:10.1.2')).toEqual({ host: 'docker.io', implicit: true })
  })

  it('reads a host out of the first segment when it looks like one', () => {
    expect(registryOf('registry.internal.example/team/api:1.4')).toEqual({
      host: 'registry.internal.example',
      implicit: false,
    })
    expect(registryOf('localhost:5000/kubby:0.9.1')).toEqual({ host: 'localhost:5000', implicit: false })
    expect(registryOf('ghcr.io/acme/web@sha256:abc')).toEqual({ host: 'ghcr.io', implicit: false })
  })

  it('has nothing to say about an empty reference', () => {
    expect(registryOf('')).toBeNull()
    expect(registryOf('   ')).toBeNull()
  })
})

describe('pulledFrom', () => {
  it('spells out the origin the reference left implicit', () => {
    expect(pulledFrom('grafana/grafana:10.1.2', 'docker.io/grafana/grafana@sha256:4f2a')).toBe(
      'docker.io/grafana/grafana@sha256:4f2a',
    )
  })

  // A mirror rewriting the pull is invisible in the spec; this is the only place it shows.
  it('reports a pull that came from somewhere other than the reference asked for', () => {
    expect(pulledFrom('ghcr.io/acme/web:2.1', 'mirror.internal/acme/web@sha256:99')).toBe(
      'mirror.internal/acme/web@sha256:99',
    )
  })

  it('stays quiet when the reference already named the registry it was pulled from', () => {
    expect(pulledFrom('ghcr.io/acme/web:2.1', 'ghcr.io/acme/web@sha256:99')).toBeNull()
  })

  it('ignores an id that names no origin', () => {
    expect(pulledFrom('nginx:1.27', 'sha256:2b4c')).toBeNull()
    expect(pulledFrom('nginx:1.27', '')).toBeNull()
  })
})

describe('podSpecOf', () => {
  const podSpec = { containers: [{ name: 'app', image: 'nginx:1.27' }] }

  it('finds the template a workload will stamp out', () => {
    expect(podSpecOf('Deployment', { template: { spec: podSpec } })).toEqual(podSpec)
    expect(podSpecOf('StatefulSet', { template: { spec: podSpec } })).toEqual(podSpec)
  })

  it('reaches the template a CronJob keeps two levels down', () => {
    expect(podSpecOf('CronJob', { jobTemplate: { spec: { template: { spec: podSpec } } } })).toEqual(podSpec)
  })

  it('takes a pod as its own spec', () => {
    expect(podSpecOf('Pod', podSpec)).toEqual(podSpec)
  })

  it('returns nothing for a kind that creates no pods', () => {
    expect(podSpecOf('Service', { type: 'ClusterIP' })).toBeNull()
  })
})

describe('containersOf', () => {
  // Application containers first, and an init container is never mistaken for one:
  // the first container in a spec is not necessarily what the pod is there to run.
  it('lists application containers before init ones and marks them apart', () => {
    const containers = containersOf({
      initContainers: [{ name: 'wait', image: 'busybox:1.36' }],
      containers: [
        { name: 'app', image: 'api:1.4', imagePullPolicy: 'IfNotPresent' },
        { name: 'istio-proxy', image: 'istio/proxyv2:1.22' },
      ],
    })

    expect(containers.map((container) => [container.name, container.init])).toEqual([
      ['app', false],
      ['istio-proxy', false],
      ['wait', true],
    ])
    expect(containers[0]?.pullPolicy).toBe('IfNotPresent')
  })

  it('reads the ports, requests, limits and mounts a container declares', () => {
    const [container] = containersOf({
      containers: [
        {
          name: 'app',
          image: 'api:1.4',
          ports: [{ containerPort: 8080, name: 'http' }, { containerPort: 9090, protocol: 'UDP' }],
          resources: { requests: { cpu: '10m', memory: '32Mi' }, limits: { memory: '192Mi' } },
          volumeMounts: [{ name: 'config', mountPath: '/etc/app', readOnly: true }],
        },
      ],
    })

    // A port with no protocol is TCP; saying nothing would read as unknown.
    expect(container?.ports).toEqual([
      { port: 8080, protocol: 'TCP', name: 'http' },
      { port: 9090, protocol: 'UDP', name: '' },
    ])
    expect(container?.requests).toEqual({ cpu: '10m', memory: '32Mi' })
    expect(container?.limits).toEqual({ memory: '192Mi' })
    expect(container?.mounts).toEqual([{ path: '/etc/app', volume: 'config', readOnly: true }])
  })

  it('drops entries that carry no image and copes with a missing spec', () => {
    expect(containersOf({ containers: [{ name: 'broken' }, 'nonsense'] })).toEqual([])
    expect(containersOf(null)).toEqual([])
  })
})

describe('volumesOf', () => {
  // The kind of a volume is the one key beside its name — that is how the API spells a
  // union, and reading it that way keeps volume types nobody has added yet working.
  it('names each volume by what actually backs it', () => {
    expect(
      volumesOf({
        volumes: [
          { name: 'config', configMap: { name: 'prometheus-server' } },
          { name: 'storage', persistentVolumeClaim: { claimName: 'data-0' } },
          { name: 'scratch', emptyDir: {} },
          { name: 'certs', secret: { secretName: 'tls' } },
        ],
      }),
    ).toEqual([
      { name: 'config', type: 'ConfigMap', source: 'prometheus-server' },
      { name: 'storage', type: 'PersistentVolumeClaim', source: 'data-0' },
      { name: 'scratch', type: 'EmptyDir', source: '' },
      { name: 'certs', type: 'Secret', source: 'tls' },
    ])
  })

  it('has nothing to say about a spec with no volumes', () => {
    expect(volumesOf({ containers: [] })).toEqual([])
    expect(volumesOf(null)).toEqual([])
  })
})

describe('formatQuantities', () => {
  it('reads cpu before memory, whatever order the object was in', () => {
    expect(formatQuantities({ memory: '32Mi', cpu: '10m' })).toBe('cpu 10m · memory 32Mi')
  })

  it('keeps extended resources rather than dropping what it does not know', () => {
    expect(formatQuantities({ 'nvidia.com/gpu': '1', cpu: '2' })).toBe('cpu 2 · nvidia.com/gpu 1')
  })

  it('says nothing when a container asked for nothing', () => {
    expect(formatQuantities({})).toBe('')
  })
})
