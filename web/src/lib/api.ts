import { z } from 'zod'

/** Every API failure reaching the UI carries a request id the user can quote. */
export interface LoginFailureDetail {
  attemptsRemaining?: number | undefined
  lockedForSeconds?: number | undefined
  blocked?: boolean | undefined
}

export class ApiError extends Error {
  readonly status: number
  readonly requestId: string | undefined
  /** Present on sign-in failures: remaining attempts, lockout length, block state. */
  readonly detail: LoginFailureDetail

  constructor(message: string, status: number, requestId?: string, detail: LoginFailureDetail = {}) {
    super(message)
    this.name = 'ApiError'
    this.status = status
    this.requestId = requestId
    this.detail = detail
  }

  /** True when the session is missing or expired rather than merely insufficient. */
  get isUnauthenticated(): boolean {
    return this.status === 401
  }

  get isForbidden(): boolean {
    return this.status === 403
  }
}

const errorBodySchema = z.object({
  error: z.string(),
  requestId: z.string().optional(),
  attemptsRemaining: z.number().optional(),
  lockedForSeconds: z.number().optional(),
  blocked: z.boolean().optional(),
})

/** Reads the double-submit token the server set on a safe request. */
function csrfToken(): string {
  const match = document.cookie.match(/(?:^|;\s*)kubby_csrf=([^;]*)/)
  return match?.[1] ? decodeURIComponent(match[1]) : ''
}

interface RequestOptions {
  method?: string
  body?: unknown
  signal?: AbortSignal | undefined
}

async function request<T>(path: string, schema: z.ZodType<T>, options: RequestOptions = {}): Promise<T> {
  const method = options.method ?? 'GET'
  const headers: Record<string, string> = { Accept: 'application/json' }

  if (options.body !== undefined) {
    headers['Content-Type'] = 'application/json'
  }
  if (method !== 'GET' && method !== 'HEAD') {
    headers['X-CSRF-Token'] = csrfToken()
  }

  const response = await fetch(path, {
    method,
    headers,
    credentials: 'same-origin',
    ...(options.body !== undefined ? { body: JSON.stringify(options.body) } : {}),
    ...(options.signal ? { signal: options.signal } : {}),
  })

  const requestId = response.headers.get('X-Request-Id') ?? undefined

  if (!response.ok) {
    const parsed = errorBodySchema.safeParse(await response.json().catch(() => null))
    throw new ApiError(
      parsed.success ? parsed.data.error : `Request failed with status ${response.status}`,
      response.status,
      parsed.success ? (parsed.data.requestId ?? requestId) : requestId,
      parsed.success
        ? {
            attemptsRemaining: parsed.data.attemptsRemaining,
            lockedForSeconds: parsed.data.lockedForSeconds,
            blocked: parsed.data.blocked,
          }
        : {},
    )
  }

  // 204 carries no body; the caller's schema for these endpoints is z.undefined().
  if (response.status === 204) {
    return schema.parse(undefined)
  }

  const result = schema.safeParse(await response.json())
  if (!result.success) {
    throw new ApiError(`Unexpected response shape from ${path}`, response.status, requestId)
  }
  return result.data
}

// ---------------------------------------------------------------- schemas

export const emptySchema = z.undefined()

export const versionSchema = z.object({
  version: z.string(),
  commitSha: z.string(),
  buildDate: z.string(),
  goVersion: z.string(),
})

export const healthSchema = z.object({
  status: z.string(),
  checks: z.record(z.string(), z.string()).optional(),
  detail: z.string().optional(),
})

export const userSchema = z.object({
  id: z.string(),
  email: z.string(),
  displayName: z.string(),
  role: z.enum(['admin', 'user', 'readonly']),
  isActive: z.boolean(),
  mfaEnrolled: z.boolean(),
  createdAt: z.string(),
  lastLoginAt: z.string().optional(),
})

export const meSchema = z.object({
  user: userSchema,
  permissions: z.array(z.string()),
  mfaEnrolled: z.boolean(),
  readOnly: z.boolean(),
})

export const loginSchema = z.object({
  mfaRequired: z.boolean().optional(),
  mfaEnrolled: z.boolean().optional(),
  mfaEnrolmentRequired: z.boolean().optional(),
  user: userSchema.optional(),
})

export const setupStatusSchema = z.object({ setupRequired: z.boolean() })
export const enrollSchema = z.object({
  secret: z.string(),
  uri: z.string(),
  qrCodePng: z.string(),
})
export const recoveryCodesSchema = z.object({ recoveryCodes: z.array(z.string()) })
export const usersSchema = z.object({ users: z.array(userSchema) })

export const sessionSchema = z.object({
  id: z.string(),
  ipAddress: z.string().optional(),
  userAgent: z.string(),
  current: z.boolean(),
  createdAt: z.string(),
  lastSeenAt: z.string(),
  expiresAt: z.string(),
})
export const sessionsSchema = z.object({ sessions: z.array(sessionSchema) })

export const clusterSchema = z.object({
  id: z.string(),
  name: z.string(),
  environment: z.enum(['prod', 'preprod', 'test', 'dr']),
  environmentLabel: z.string(),
  displayEnvironment: z.string(),
  color: z.string(),
  authSource: z.string(),
  apiServerUrl: z.string(),
  insecureSkipTlsVerify: z.boolean(),
  credentialStatus: z.enum(['valid', 'invalid', 'unreachable', 'unknown']),
  statusDetail: z.string().optional(),
  k8sVersion: z.string().optional(),
  nodeCount: z.number().optional(),
  metricsAvailable: z.boolean(),
  readOnly: z.boolean(),
  impersonationEnabled: z.boolean(),
  qpsLimit: z.number(),
  lastValidatedAt: z.string().optional(),
  accessLevel: z.string().optional(),
  metricsUrl: z.string().optional(),
  metricsUsername: z.string().optional(),
  metricsInsecureSkipVerify: z.boolean().default(false),
  logsUrl: z.string().optional(),
  logsIndex: z.string().optional(),
  logsAuthScheme: z.string().optional(),
  logsUsername: z.string().optional(),
  logsInsecureSkipVerify: z.boolean().default(false),
})

/** One pod that keeps saying something is wrong, read out of its own logs. */
export const logFindingSchema = z.object({
  namespace: z.string(),
  pod: z.string(),
  container: z.string().optional(),
  rule: z.string(),
  class: z.enum(['auth', 'unreachable', 'timeout', 'generic']).catch('generic'),
  count: z.number(),
  summary: z.string().optional(),
  sample: z.string(),
  firstSeen: z.string(),
  lastSeen: z.string(),
  severity: z.enum(['warning', 'error']).catch('warning'),
  // How many pods a rolled-up finding covers. Absent on a pod's own finding.
  pods: z.number().optional(),
})

/**
 * What a cluster's logs are saying.
 *
 * `unknown` is the state that matters: a store nobody could reach must never render as
 * a cluster with nothing wrong in it.
 */
export const logFindingsSchema = z.object({
  state: z.enum(['off', 'ok', 'unknown']).catch('unknown'),
  detail: z.string().optional(),
  sweptAt: z.string().optional(),
  findings: z.array(logFindingSchema),
})

/** What a log-source connection test found. A failure is an answer, not an error. */
export const logsProbeSchema = z.object({
  reachable: z.boolean(),
  detail: z.string().optional(),
  probe: z
    .object({
      cluster: z.string().optional(),
      version: z.string().optional(),
      indices: z.number(),
      documents: z.number(),
      windowMinutes: z.number(),
      // The whole document, so the field names can be read off it.
      sample: z.record(z.string(), z.unknown()).optional(),
      sampleAt: z.string().optional(),
      messageField: z.string().optional(),
      // How the store indexes the field the rules match against. A keyword field cannot
      // be searched for a substring by a phrase query, and a length limit on one drops
      // long stack traces — neither shows up as an error, only as nothing being found.
      message: z
        .object({
          type: z.string().optional(),
          analyzed: z.boolean(),
          ignoreAbove: z.number().optional(),
        })
        .optional(),
    })
    .optional(),
})
export const clustersSchema = z.object({ clusters: z.array(clusterSchema) })

export const kubeContextSchema = z.object({
  name: z.string(),
  clusterName: z.string(),
  userName: z.string(),
  server: z.string(),
  namespace: z.string().optional(),
  authMethod: z.string(),
  insecureSkipTlsVerify: z.boolean(),
  hasCertificateAuthority: z.boolean(),
  blocked: z.boolean(),
  problem: z.string().optional(),
})

export const probeSchema = z.object({
  status: z.enum(['valid', 'invalid', 'unreachable']),
  detail: z.string().optional(),
  k8sVersion: z.string().optional(),
  nodeCount: z.number().optional(),
  metricsAvailable: z.boolean(),
  permissions: z.array(z.string()).optional(),
})

export const validateKubeconfigSchema = z.object({
  contexts: z.array(kubeContextSchema),
  currentContext: z.string(),
  probe: probeSchema.optional(),
})

export const clusterGrantSchema = z.object({
  userId: z.string(),
  email: z.string(),
  displayName: z.string(),
  accessLevel: z.enum(['read', 'write']),
})
export const clusterGrantsSchema = z.object({ grants: z.array(clusterGrantSchema) })

export const resourceTypeSchema = z.object({
  key: z.string(),
  kind: z.string(),
  group: z.string().optional(),
  namespaced: z.boolean(),
  category: z.enum(['workload', 'config', 'network', 'storage', 'access', 'cluster', 'custom']),
  cached: z.boolean(),
})
export const resourceTypesSchema = z.object({ types: z.array(resourceTypeSchema) })
export const namespacesSchema = z.object({ namespaces: z.array(z.string()) })

export const columnSchema = z.object({
  key: z.string(),
  label: z.string(),
  mono: z.boolean().optional(),
  link: z.enum(['namespace', 'owner', 'node']).optional(),
  status: z.boolean().optional(),
})

/** One container of a pod, as much of it as a list row can say without opening the pod. */
export const containerStateSchema = z.object({
  name: z.string(),
  state: z.enum(['running', 'waiting', 'terminated', '']).catch(''),
  ready: z.boolean().optional(),
  init: z.boolean().optional(),
  reason: z.string().optional(),
  message: z.string().optional(),
  exitCode: z.number().optional(),
  startedAt: z.string().optional(),
  finishedAt: z.string().optional(),
  restarts: z.number().optional(),
  containerId: z.string().optional(),
})

export const rowSchema = z.object({
  name: z.string(),
  namespace: z.string().optional(),
  age: z.string(),
  createdAt: z.string(),
  fields: z.record(z.string(), z.string()),
  severity: z.enum(['warning', 'error']).optional(),
  containerStates: z.array(containerStateSchema).optional(),
  logFinding: logFindingSchema.optional(),
})

export const resourceListSchema = z.object({
  columns: z.array(columnSchema),
  rows: z.array(rowSchema),
  total: z.number(),
  fromCache: z.boolean(),
  hideName: z.boolean().optional(),
  warming: z.boolean().optional(),
  continue: z.string().optional(),
})

const gaugeSchema = z.object({
  usage: z.number(),
  requests: z.number(),
  limits: z.number(),
  allocatable: z.number(),
  capacity: z.number(),
  unit: z.string(),
})

export const workloadOverviewSchema = z.object({
  counts: z.array(
    z.object({
      kind: z.string(),
      typeKey: z.string(),
      total: z.number(),
      ready: z.number(),
    }),
  ),
  events: resourceListSchema.nullable().optional(),
})

export type WorkloadOverview = z.infer<typeof workloadOverviewSchema>

export const relationSchema = z.object({
  direction: z.enum(['owner', 'owned', 'serves']),
  kind: z.string(),
  typeKey: z.string(),
  namespace: z.string().optional(),
  name: z.string(),
  detail: z.string().optional(),
  severity: z.string().optional(),
})

export type Relation = z.infer<typeof relationSchema>

const relationsSchema = z.object({ relations: z.array(relationSchema).nullable() })

// ---------------------------------------------------------------- deployment settings

export const kubbySettingsSchema = z.object({
  nodeShell: z.object({
    image: z.string(),
    namespace: z.string(),
    pullSecret: z.string().optional(),
    enabled: z.boolean(),
  }),
  podDebug: z.object({ image: z.string() }),
  metrics: z.object({
    enabled: z.boolean(),
    url: z.string(),
    username: z.string().optional(),
    // Whether a credential is stored is configuration; the credential is not.
    hasPassword: z.boolean(),
    insecureSkipVerify: z.boolean(),
    organization: z.string().optional(),
  }),
  auditSink: z.object({
    enabled: z.boolean(),
    kind: z.string(),
    url: z.string(),
    index: z.string().optional(),
    username: z.string().optional(),
    hasToken: z.boolean(),
    insecureSkipVerify: z.boolean(),
    scheme: z.string().optional(),
    dataStream: z.boolean().default(false),
  }),
  // What counts as a problem in a log line, and where in a document to look for it.
  logAnalysis: z.object({
    fields: z.object({
      timestamp: z.string().optional(),
      message: z.string().optional(),
      pod: z.string().optional(),
      namespace: z.string().optional(),
      container: z.string().optional(),
    }),
    rules: z.array(
      z.object({
        name: z.string(),
        class: z.string(),
        match: z.array(z.string()),
        capture: z.array(z.string()).optional(),
        disabled: z.boolean().optional(),
      }),
    ),
    windowMinutes: z.number().optional(),
    minCount: z.number().optional(),
  }),
})

export type KubbySettings = z.infer<typeof kubbySettingsSchema>

// ---------------------------------------------------------------- writing

export const diffLineSchema = z.object({
  kind: z.enum(['context', 'added', 'removed']),
  text: z.string(),
})

export type DiffLine = z.infer<typeof diffLineSchema>

export const gitOpsOwnerSchema = z.object({
  controller: z.string(),
  instance: z.string().optional(),
  selfHeal: z.boolean(),
})

export type GitOpsOwner = z.infer<typeof gitOpsOwnerSchema>

export const applyResultSchema = z.object({
  dryRun: z.boolean(),
  results: z.array(
    z.object({
      kind: z.string(),
      name: z.string(),
      namespace: z.string().optional(),
      diff: z.array(diffLineSchema).optional(),
      unchanged: z.boolean().optional(),
      owner: gitOpsOwnerSchema.optional(),
      error: z.string().optional(),
    }),
  ),
})

export type ApplyResult = z.infer<typeof applyResultSchema>

export const revisionSchema = z.object({
  number: z.number(),
  created: z.string(),
  images: z.array(z.string()),
  cause: z.string().optional(),
  current: z.boolean(),
})

export type Revision = z.infer<typeof revisionSchema>

const rolloutSchema = z.object({ revisions: z.array(revisionSchema) })

const drainPodSchema = z.object({
  namespace: z.string(),
  name: z.string(),
  owner: z.string().optional(),
  reason: z.string().optional(),
})

export const drainPlanSchema = z.object({
  node: z.string(),
  evict: z.array(drainPodSchema).nullable(),
  skip: z.array(drainPodSchema).nullable(),
})

export type DrainPlan = z.infer<typeof drainPlanSchema>

const drainResultSchema = z.object({
  results: z.array(
    z.object({
      namespace: z.string(),
      name: z.string(),
      evicted: z.boolean(),
      reason: z.string().optional(),
    }),
  ),
})

// ---------------------------------------------------------------- problem detection

export const findingSchema = z.object({
  category: z.string(),
  severity: z.string(),
  kind: z.string(),
  namespace: z.string().optional(),
  name: z.string(),
  reason: z.string(),
  detail: z.string(),
  container: z.string().optional(),
  count: z.number().optional(),
  lastSeen: z.string().optional(),
  typeKey: z.string().optional(),
})

export type Finding = z.infer<typeof findingSchema>

export const healthReportSchema = z.object({
  findings: z.array(findingSchema),
  failed: z.record(z.string(), z.string()).optional(),
  counts: z.record(z.string(), z.number()),
})

export type HealthReport = z.infer<typeof healthReportSchema>

export const clusterCardSchema = z.object({
  id: z.string(),
  name: z.string(),
  environment: z.string().optional(),
  colour: z.string().optional(),
  status: z.string(),
  unreachable: z.boolean(),
  error: z.string().optional(),
  counts: z.record(z.string(), z.number()),
  top: z.array(findingSchema).optional(),
  checkedAt: z.string(),
  stale: z.boolean(),
  // Absent when the cluster could not be reached — a card without it still says what is
  // wrong, which is what the fleet screen is for.
  capacity: z
    .object({
      nodes: z.number(),
      nodesReady: z.number(),
      cores: z.number(),
      memoryMiB: z.number(),
      pods: z.number(),
      k8sVersion: z.string().optional(),
    })
    .nullable()
    .optional(),
})

export type ClusterCard = z.infer<typeof clusterCardSchema>

const fleetSchema = z.object({ clusters: z.array(clusterCardSchema) })

export const containerSchema = z.object({
  name: z.string(),
  role: z.enum(['app', 'sidecar', 'init']),
})

export type PodContainer = z.infer<typeof containerSchema>

const containersSchema = z.object({ containers: z.array(containerSchema) })

// ---------------------------------------------------------------- cluster metrics

const pointSchema = z.object({ at: z.string(), value: z.number() })

// A number that might not exist. `known: false` means nobody is collecting the metric —
// which is a different answer from zero, and the panel says N/A rather than reassuring
// somebody with a number it never read.
const readingSchema = z.object({ value: z.number(), known: z.boolean() })

const clusterSummarySchema = z.object({
  status: z.enum(['Healthy', 'Degraded', 'Critical', 'Unknown']),
  reasons: z.array(z.string()).nullable().optional(),
  nodesReady: readingSchema,
  nodesTotal: readingSchema,
  nodesNotReady: readingSchema,
  nodesUnschedulable: readingSchema,
  nodesUnderPressure: readingSchema,
  podsReady: readingSchema,
  podsTotal: readingSchema,
  podsPending: readingSchema,
  // Older servers do not send it; the tile reads N/A rather than a reassuring zero.
  containersNotStarting: readingSchema.optional(),
  longestPendingSeconds: readingSchema,
  restarts1h: readingSchema,
  oomKilled: readingSchema,
  evicted: readingSchema,
  unavailableWorkloads: readingSchema,
  alertsCritical: readingSchema,
  alertsWarning: readingSchema,
  apiErrorRate: readingSchema,
  targetsDown: readingSchema,
  targetsTotal: readingSchema,
})

const problemSchema = z.object({
  kind: z.string(),
  namespace: z.string(),
  name: z.string(),
  container: z.string().optional(),
  reason: z.string(),
  detail: z.string().optional(),
  severity: z.string(),
  node: z.string().optional(),
  ageSeconds: z.number().optional(),
})

const podProblemSchema = z.object({
  namespace: z.string(),
  pod: z.string(),
  container: z.string().optional(),
  node: z.string().optional(),
  status: z.string(),
  reason: z.string().optional(),
  severity: z.string(),
  restarts: z.number(),
  ageSeconds: z.number(),
  cpuUsed: readingSchema,
  cpuRequest: readingSchema,
  cpuLimit: readingSchema,
  memoryUsed: readingSchema,
  memoryRequest: readingSchema,
  memoryLimit: readingSchema,
})

const storageProblemSchema = z.object({
  namespace: z.string(),
  name: z.string(),
  phase: z.string(),
  storageClass: z.string().optional(),
  capacityBytes: readingSchema,
  usedBytes: readingSchema,
  severity: z.string(),
})

const workloadRowSchema = z.object({
  kind: z.string(),
  namespace: z.string(),
  name: z.string(),
  ready: z.number(),
  desired: z.number(),
  updated: z.number(),
  available: z.number(),
  misscheduled: z.number(),
})

const alertSchema = z.object({
  name: z.string(),
  severity: z.string(),
  namespace: z.string().optional(),
  object: z.string().optional(),
  kind: z.string().optional(),
  summary: z.string().optional(),
})

const controlPlaneSchema = z.object({
  apiServers: readingSchema,
  apiLatencyP50: readingSchema,
  apiLatencyP95: readingSchema,
  apiLatencyP99: readingSchema,
  apiErrors4xx: readingSchema,
  apiErrors5xx: readingSchema,
  apiRequests: readingSchema,
  etcdMembers: readingSchema,
  etcdHasLeader: readingSchema,
  etcdLeaderChanges: readingSchema,
  etcdDbBytes: readingSchema,
  etcdFsyncP99: readingSchema,
  schedulerAttempts: readingSchema,
  schedulerUnschedulable: readingSchema,
  corednsUp: readingSchema,
  corednsErrorRate: readingSchema,
  corednsLatencyP99: readingSchema,
  scrapeTargets: readingSchema,
  scrapeFailures: readingSchema,
  ruleFailures: readingSchema,
  certExpiryDays: readingSchema,
  controllerQueueDepth: readingSchema,
  controllerRetries: readingSchema,
  ingressRequests: readingSchema,
  ingressErrorRate: readingSchema,
  ingressLatencyP99: readingSchema,
  quotaNearLimit: readingSchema,
  volumeCapacityBytes: readingSchema,
  volumesBound: readingSchema,
  volumeRequestedBytes: readingSchema,
  volumeUsedBytes: readingSchema,
})

const namespaceUsageSchema = z.object({
  namespace: z.string(),
  cpuCores: z.number(),
  cpuRequests: z.number(),
  memoryBytes: z.number(),
  memoryRequests: z.number(),
  pods: z.number(),
})

const extrasSchema = z.object({
  risks: z
    .array(
      z.object({
        namespace: z.string(),
        pod: z.string(),
        container: z.string(),
        kind: z.string(),
        value: z.number().optional(),
      }),
    )
    .nullable(),
  exitCodes: z
    .array(
      z.object({
        namespace: z.string(),
        pod: z.string(),
        container: z.string(),
        code: z.number(),
        reason: z.string().optional(),
      }),
    )
    .nullable(),
  scalers: z
    .array(
      z.object({
        namespace: z.string(),
        name: z.string(),
        current: z.number(),
        min: z.number(),
        max: z.number(),
        atCeiling: z.boolean(),
      }),
    )
    .nullable(),
  scalersKnown: z.boolean(),
  serviceGaps: z.array(z.object({ namespace: z.string(), name: z.string() })).nullable(),
  servicesKnown: z.boolean(),
  downTargets: z.array(z.object({ job: z.string(), instance: z.string() })).nullable(),
  containersReady: readingSchema,
  containersTotal: readingSchema,
  restarts15m: readingSchema,
  restarts24h: readingSchema,
  appErrorRate: readingSchema,
  lateCronJobs: readingSchema,
  stalledRollouts: z.array(problemSchema).nullable(),
})

const namedSeriesSchema = z.object({ name: z.string(), points: z.array(pointSchema).nullable() })

const trendsSchema = z.object({
  sparks: z.record(z.string(), z.array(pointSchema).nullable()).nullable().optional(),
  diskByNode: z.array(namedSeriesSchema).nullable().optional(),
  networkRx: z.array(namedSeriesSchema).nullable().optional(),
  networkTx: z.array(namedSeriesSchema).nullable().optional(),
  cpuByNodeOverTime: z.array(namedSeriesSchema).nullable().optional(),
  memoryByNodeOverTime: z.array(namedSeriesSchema).nullable().optional(),
  ioWaitByNode: z.array(namedSeriesSchema).nullable().optional(),
})

const containerIssueSchema = z.object({
  namespace: z.string(),
  pod: z.string(),
  container: z.string(),
  reason: z.string(),
})

const clusterHealthMetricsSchema = z.object({
  cpu: z.array(pointSchema).nullable().optional(),
  memory: z.array(pointSchema).nullable().optional(),
  disk: z.array(z.object({ node: z.string(), percent: z.number() })).nullable().optional(),
  restarts: z.array(pointSchema).nullable().optional(),
  failing: z
    .array(
      z.object({
        namespace: z.string(),
        pod: z.string(),
        container: z.string(),
        restarts: z.number(),
        reason: z.string().optional(),
      }),
    )
    .nullable()
    .optional(),
  degraded: z
    .array(
      z.object({
        namespace: z.string(),
        name: z.string(),
        kind: z.string(),
        missing: z.number(),
        forMinutes: z.number(),
      }),
    )
    .nullable()
    .optional(),
  nodeIssues: z
    .array(z.object({ node: z.string(), condition: z.string(), minutes: z.number() }))
    .nullable()
    .optional(),
  pods: z.object({
    running: z.number(),
    pending: z.number(),
    // Split out of pending by the server; older servers do not send it.
    notStarting: z.number().default(0),
    failed: z.number(),
    succeeded: z.number(),
    unknown: z.number(),
  }),
  nodes: z.object({ ready: z.number(), total: z.number() }),
  restarts24h: z.number(),
  cpuByNode: z.array(z.object({ node: z.string(), percent: z.number() })).nullable().optional(),
  memoryByNode: z.array(z.object({ node: z.string(), percent: z.number() })).nullable().optional(),
  topCpu: z.array(z.object({ name: z.string(), value: z.number() })).nullable().optional(),
  topMemory: z.array(z.object({ name: z.string(), value: z.number() })).nullable().optional(),
  reasons: z.array(z.object({ name: z.string(), value: z.number() })).nullable().optional(),
  waiting: z.array(z.object({ name: z.string(), value: z.number() })).nullable().optional(),
  summary: clusterSummarySchema.optional(),
  problems: z.array(problemSchema).nullable().optional(),
  podProblems: z.array(podProblemSchema).nullable().optional(),
  storageProblems: z.array(storageProblemSchema).nullable().optional(),
  workloads: z.array(workloadRowSchema).nullable().optional(),
  alerts: z.array(alertSchema).nullable().optional(),
  controlPlane: controlPlaneSchema.optional(),
  namespaceUsage: z.array(namespaceUsageSchema).nullable().optional(),
  spread: z
    .array(z.object({ namespace: z.string(), node: z.string(), pods: z.number() }))
    .nullable()
    .optional(),
  trends: trendsSchema.optional(),
  extras: extrasSchema.optional(),
  // One card per machine. Everything optional past the name: a cluster missing
  // node-exporter still gets cards with the Kubernetes half filled in, and a panel with
  // gaps beats no panel.
  nodeDetails: z
    .array(
      z.object({
        name: z.string(),
        role: z.string(),
        ready: z.boolean(),
        cpuPercent: z.number(),
        memoryPercent: z.number(),
        diskPercent: z.number(),
        cores: z.number(),
        memoryTotalBytes: z.number(),
        cpuCommittedPercent: z.number(),
        memoryCommittedPercent: z.number(),
        pods: z.number(),
        podCapacity: z.number(),
        networkRxBytes: z.number(),
        networkTxBytes: z.number(),
        loadPerCore: z.number(),
        uptimeSeconds: z.number(),
        memoryPressure: z.boolean(),
        diskPressure: z.boolean(),
        pidPressure: z.boolean(),
        networkUnavailable: z.boolean(),
        unschedulable: z.boolean(),
        swapPercent: z.number(),
        swapTotalBytes: z.number(),
        inodePercent: z.number(),
        diskReadBytes: z.number(),
        diskWriteBytes: z.number(),
        diskBusyPercent: z.number(),
        ioWaitPercent: z.number(),
        networkRxErrors: z.number(),
        networkTxErrors: z.number(),
        networkDrops: z.number(),
        clockSkewSeconds: z.number(),
        bootTimeUnix: z.number(),
        nodeExporterUp: z.boolean(),
        kubeletUp: z.boolean(),
        cpuLimitPercent: z.number(),
        memoryLimitPercent: z.number(),
        cpuAllocatable: z.number(),
        memoryAllocatable: z.number(),
        kubeletVersion: z.string().optional(),
        osImage: z.string().optional(),
        kernel: z.string().optional(),
        architecture: z.string().optional(),
      }),
    )
    .nullable()
    .optional(),
  capacity: z
    .object({
      nodes: z.number(),
      cores: z.number(),
      memoryBytes: z.number(),
      pods: z.number(),
      podCapacity: z.number(),
      cpuCommittedPercent: z.number(),
      memoryCommittedPercent: z.number(),
    })
    .optional(),
  // The containers behind the two rings, named well enough to open one.
  stuck: z.array(containerIssueSchema).nullable().optional(),
  died: z.array(containerIssueSchema).nullable().optional(),
  windowMinutes: z.number(),
  warnings: z.array(z.string()).nullable().optional(),
})

const clusterMetricsSchema = z.object({
  configured: z.boolean(),
  error: z.string().optional(),
  health: clusterHealthMetricsSchema.optional(),
  // Where the endpoint came from. 'auto' is one Kubby found inside the cluster, 'manual'
  // an address somebody typed, 'default' the deployment-wide fallback — which is the only
  // one that can be reporting a different cluster's numbers.
  source: z.enum(['manual', 'auto', 'default']).optional(),
  endpoint: z.string().optional(),
})

export type ClusterMetrics = z.infer<typeof clusterMetricsSchema>
export type ClusterHealthMetrics = z.infer<typeof clusterHealthMetricsSchema>
export type NodeDetail = NonNullable<ClusterHealthMetrics['nodeDetails']>[number]
export type ContainerIssue = z.infer<typeof containerIssueSchema>
export type ClusterSummary = z.infer<typeof clusterSummarySchema>
export type Reading = z.infer<typeof readingSchema>
export type Problem = z.infer<typeof problemSchema>
export type PodProblem = z.infer<typeof podProblemSchema>
export type StorageProblem = z.infer<typeof storageProblemSchema>
export type WorkloadRow = z.infer<typeof workloadRowSchema>
export type ClusterAlert = z.infer<typeof alertSchema>
export type ControlPlane = z.infer<typeof controlPlaneSchema>
export type NamespaceUsage = z.infer<typeof namespaceUsageSchema>
export type NamedSeries = z.infer<typeof namedSeriesSchema>
export type Extras = z.infer<typeof extrasSchema>
export type ContainerRisk = NonNullable<Extras['risks']>[number]
export type MetricPoint = z.infer<typeof pointSchema>

// ---------------------------------------------------------------- helm

const helmReleaseSchema = z.object({
  name: z.string(),
  namespace: z.string(),
  revision: z.number(),
  status: z.string(),
  chart: z.string().optional(),
  chartVersion: z.string().optional(),
  appVersion: z.string().optional(),
  updated: z.string().optional(),
  description: z.string().optional(),
})

const helmReleasesSchema = z.object({ releases: z.array(helmReleaseSchema).nullable().optional() })

const helmDetailSchema = helmReleaseSchema.extend({
  values: z.record(z.string(), z.unknown()).nullable().optional(),
  notes: z.string().optional(),
  history: z.array(helmReleaseSchema).nullable().optional(),
})

export type HelmRelease = z.infer<typeof helmReleaseSchema>
export type HelmReleaseDetail = z.infer<typeof helmDetailSchema>

// ---------------------------------------------------------------- global search

const searchHitSchema = z.object({
  clusterId: z.string(),
  clusterName: z.string(),
  environment: z.string(),
  typeKey: z.string(),
  kind: z.string(),
  namespace: z.string().optional(),
  name: z.string(),
  status: z.string().optional(),
  severity: z.string().optional(),
  age: z.string().optional(),
})

const searchResultSchema = z.object({
  hits: z.array(searchHitSchema).nullable().optional(),
  unreachable: z
    .array(z.object({ clusterId: z.string(), clusterName: z.string(), reason: z.string() }))
    .nullable()
    .optional(),
  truncated: z.boolean().default(false),
})

export type SearchHit = z.infer<typeof searchHitSchema>
export type SearchResult = z.infer<typeof searchResultSchema>

const portOptionSchema = z.object({
  name: z.string().optional(),
  port: z.number(),
  protocol: z.string(),
  container: z.string().optional(),
})
const portsSchema = z.object({ ports: z.array(portOptionSchema) })

const forwardSchema = z.object({
  id: z.string(),
  clusterId: z.string(),
  type: z.string(),
  namespace: z.string(),
  name: z.string(),
  pod: z.string(),
  port: z.number(),
  url: z.string(),
  startedAt: z.string(),
  // How the browser reaches it. A real local port gives the forwarded app its own
  // origin at its own root; the proxy is the fallback for a Kubby the browser can only
  // reach over HTTP, and it cannot serve every application.
  mode: z.enum(['port', 'proxy']).catch('proxy'),
  localPort: z.number().optional(),
  // Why a port could not be opened, when one could not.
  note: z.string().optional(),
})
const forwardsSchema = z.object({ forwards: z.array(forwardSchema) })

export type PortOption = z.infer<typeof portOptionSchema>
export type Forward = z.infer<typeof forwardSchema>

const terminationSchema = z.object({
  reason: z.string().optional(),
  exitCode: z.number(),
  signal: z.number().optional(),
  message: z.string().optional(),
  startedAt: z.string().optional(),
  finishedAt: z.string().optional(),
})

export const restartSummarySchema = z.object({
  app: z.number(),
  sidecar: z.number(),
  init: z.number(),
  details: z
    .array(
      z.object({
        name: z.string(),
        role: z.enum(['app', 'sidecar', 'init']),
        count: z.number(),
        last: terminationSchema.optional(),
      }),
    )
    .optional(),
})

export type RestartSummary = z.infer<typeof restartSummarySchema>

const describeSchema = z.object({ text: z.string() })

export const secretKeysSchema = z.object({
  keys: z.array(z.object({ key: z.string(), bytes: z.number() })),
})

export type SecretKey = z.infer<typeof secretKeysSchema>['keys'][number]

const revealSchema = z.object({ key: z.string(), value: z.string() })

export const overviewSchema = z.object({
  nodes: z.number(),
  nodesReady: z.number(),
  namespaces: z.number(),
  metricsAvailable: z.boolean(),
  k8sVersion: z.string(),
  cpu: gaugeSchema,
  memory: gaugeSchema,
  pods: gaugeSchema,
  problems: z
    .array(
      z.object({
        kind: z.string(),
        namespace: z.string().optional(),
        name: z.string(),
        reason: z.string(),
        severity: z.string(),
      }),
    )
    .nullable()
    .optional(),
})

export const auditEventSchema = z.object({
  id: z.number(),
  occurredAt: z.string(),
  actorEmail: z.string(),
  action: z.string(),
  result: z.enum(['success', 'denied', 'error']),
  resourceKind: z.string().optional(),
  resourceName: z.string().optional(),
  ipAddress: z.string().optional(),
  requestId: z.string().optional(),
  details: z.record(z.string(), z.unknown()).optional(),
})
export const auditSchema = z.object({ events: z.array(auditEventSchema) })

export type VersionInfo = z.infer<typeof versionSchema>
export type HealthInfo = z.infer<typeof healthSchema>
export type User = z.infer<typeof userSchema>
export type Me = z.infer<typeof meSchema>
export type SessionInfo = z.infer<typeof sessionSchema>
export type AuditEvent = z.infer<typeof auditEventSchema>
export type Role = User['role']
export type Cluster = z.infer<typeof clusterSchema>
export type KubeContext = z.infer<typeof kubeContextSchema>
export type Probe = z.infer<typeof probeSchema>
export type ValidateResult = z.infer<typeof validateKubeconfigSchema>
export type ClusterGrant = z.infer<typeof clusterGrantSchema>
export type Environment = Cluster['environment']
export type ResourceType = z.infer<typeof resourceTypeSchema>
export type ResourceCategory = ResourceType['category']
export type LogsProbe = z.infer<typeof logsProbeSchema>
export type LogFinding = z.infer<typeof logFindingSchema>
export type LogFindings = z.infer<typeof logFindingsSchema>
export type Column = z.infer<typeof columnSchema>
export type ContainerState = z.infer<typeof containerStateSchema>
export type ResourceRow = z.infer<typeof rowSchema>
export type ResourceList = z.infer<typeof resourceListSchema>
export type Overview = z.infer<typeof overviewSchema>
export type Gauge = z.infer<typeof gaugeSchema>
export type CredentialStatus = Cluster['credentialStatus']

// ---------------------------------------------------------------- endpoints

export const api = {
  version: (signal?: AbortSignal) => request('/version', versionSchema, { signal }),
  readiness: (signal?: AbortSignal) => request('/readyz', healthSchema, { signal }),

  setupStatus: (signal?: AbortSignal) =>
    request('/api/v1/setup/status', setupStatusSchema, { signal }),
  createFirstAdmin: (body: { email: string; displayName: string; password: string }) =>
    request('/api/v1/setup/admin', userSchema, { method: 'POST', body }),

  login: (body: { email: string; password: string }) =>
    request('/api/v1/auth/login', loginSchema, { method: 'POST', body }),
  verifyMfa: (body: { code?: string; recoveryCode?: string }) =>
    request('/api/v1/auth/mfa/verify', loginSchema, { method: 'POST', body }),
  logout: () => request('/api/v1/auth/logout', emptySchema, { method: 'POST' }),

  me: (signal?: AbortSignal) => request('/api/v1/me', meSchema, { signal }),
  changePassword: (body: { currentPassword: string; newPassword: string }) =>
    request('/api/v1/me/password', emptySchema, { method: 'POST', body }),
  sessions: (signal?: AbortSignal) => request('/api/v1/me/sessions', sessionsSchema, { signal }),
  revokeOtherSessions: () =>
    request('/api/v1/me/sessions', z.object({ revoked: z.number() }), { method: 'DELETE' }),
  enrollMfa: () => request('/api/v1/me/mfa/enroll', enrollSchema, { method: 'POST' }),
  confirmMfa: (body: { code: string }) =>
    request('/api/v1/me/mfa/confirm', recoveryCodesSchema, { method: 'POST', body }),

  users: (signal?: AbortSignal) => request('/api/v1/users', usersSchema, { signal }),
  createUser: (body: { email: string; displayName: string; password: string; role: Role }) =>
    request('/api/v1/users', userSchema, { method: 'POST', body }),
  updateUser: (id: string, body: { role?: Role; isActive?: boolean }) =>
    request(`/api/v1/users/${id}`, userSchema, { method: 'PATCH', body }),

  clusters: (signal?: AbortSignal) => request('/api/v1/clusters', clustersSchema, { signal }),
  cluster: (id: string, signal?: AbortSignal) =>
    request(`/api/v1/clusters/${id}`, clusterSchema, { signal }),
  validateKubeconfig: (body: { kubeconfig: string; contextName?: string }) =>
    request('/api/v1/clusters/validate', validateKubeconfigSchema, { method: 'POST', body }),
  createCluster: (body: {
    name: string
    environment: Environment
    environmentLabel?: string
    color?: string
    kubeconfig: string
    contextName?: string
  }) => request('/api/v1/clusters', clusterSchema, { method: 'POST', body }),
  updateCluster: (
    id: string,
    body: {
      name?: string
      environment?: Environment
      environmentLabel?: string
      color?: string
      readOnly?: boolean
      impersonationEnabled?: boolean
      qpsLimit?: number
      metricsUrl?: string
      metricsUsername?: string
      metricsInsecureSkipVerify?: boolean
      metricsPassword?: string
      clearMetricsPassword?: boolean
      logsUrl?: string
      logsIndex?: string
      logsAuthScheme?: string
      logsUsername?: string
      logsInsecureSkipVerify?: boolean
      logsSecret?: string
      clearLogsSecret?: boolean
    },
  ) => request(`/api/v1/clusters/${id}`, clusterSchema, { method: 'PATCH', body }),
  logFindings: (clusterId: string, signal?: AbortSignal) =>
    request(`/api/v1/clusters/${clusterId}/log-findings`, logFindingsSchema, { signal }),
  probeClusterLogs: (
    id: string,
    body: {
      logsUrl?: string
      logsIndex?: string
      logsAuthScheme?: string
      logsUsername?: string
      logsSecret?: string
      logsInsecureSkipVerify?: boolean
    },
  ) => request(`/api/v1/clusters/${id}/logs/probe`, logsProbeSchema, { method: 'POST', body }),
  replaceCredential: (id: string, body: { kubeconfig: string; contextName?: string }) =>
    request(`/api/v1/clusters/${id}/credentials`, clusterSchema, { method: 'PUT', body }),
  testCluster: (id: string) =>
    request(`/api/v1/clusters/${id}/test`, clusterSchema, { method: 'POST' }),
  deleteCluster: (id: string) =>
    request(`/api/v1/clusters/${id}`, emptySchema, { method: 'DELETE' }),
  clusterGrants: (id: string, signal?: AbortSignal) =>
    request(`/api/v1/clusters/${id}/grants`, clusterGrantsSchema, { signal }),
  setClusterGrant: (id: string, body: { userId: string; accessLevel: string }) =>
    request(`/api/v1/clusters/${id}/grants`, clusterGrantsSchema, { method: 'PUT', body }),

  resourceTypes: (clusterId: string, signal?: AbortSignal) =>
    request(`/api/v1/clusters/${clusterId}/resource-types`, resourceTypesSchema, { signal }),
  overview: (clusterId: string, signal?: AbortSignal) =>
    request(`/api/v1/clusters/${clusterId}/overview`, overviewSchema, { signal }),
  namespaces: (clusterId: string, signal?: AbortSignal) =>
    request(`/api/v1/clusters/${clusterId}/namespaces`, namespacesSchema, { signal }),

  resources: (
    clusterId: string,
    typeKey: string,
    params: { namespace?: string; search?: string; sort?: string; desc?: boolean } = {},
    signal?: AbortSignal,
  ) => {
    const query = new URLSearchParams()
    if (params.namespace) query.set('namespace', params.namespace)
    if (params.search) query.set('search', params.search)
    if (params.sort) query.set('sort', params.sort)
    if (params.desc) query.set('desc', 'true')

    const suffix = query.size > 0 ? `?${query.toString()}` : ''
    return request(`/api/v1/clusters/${clusterId}/resources/${typeKey}${suffix}`, resourceListSchema, { signal })
  },

  resourceObject: (
    clusterId: string,
    typeKey: string,
    params: { name: string; namespace?: string },
    signal?: AbortSignal,
  ) => {
    const query = new URLSearchParams({ name: params.name })
    if (params.namespace) query.set('namespace', params.namespace)

    return request(
      `/api/v1/clusters/${clusterId}/object/${typeKey}?${query.toString()}`,
      z.record(z.string(), z.unknown()),
      { signal },
    )
  },

  settings: (signal?: AbortSignal) => request('/api/v1/settings', kubbySettingsSchema, { signal }),

  saveNodeShell: (body: KubbySettings['nodeShell']) =>
    request('/api/v1/settings/node-shell', kubbySettingsSchema, { method: 'PUT', body }),

  savePodDebug: (body: KubbySettings['podDebug']) =>
    request('/api/v1/settings/pod-debug', kubbySettingsSchema, { method: 'PUT', body }),

  saveMetrics: (body: KubbySettings['metrics'] & { password?: string; clearPassword?: boolean }) =>
    request('/api/v1/settings/metrics', kubbySettingsSchema, { method: 'PUT', body }),

  saveLogAnalysis: (body: KubbySettings['logAnalysis']) =>
    request('/api/v1/settings/log-analysis', kubbySettingsSchema, { method: 'PUT', body }),

  saveAuditSink: (body: KubbySettings['auditSink'] & { token?: string; clearToken?: boolean }) =>
    request('/api/v1/settings/audit-sink', kubbySettingsSchema, { method: 'PUT', body }),

  workloadsOverview: (clusterId: string, namespaces: string[] = [], signal?: AbortSignal) => {
    const suffix = namespaces.length > 0 ? `?namespace=${encodeURIComponent(namespaces.join(','))}` : ''
    return request(
      `/api/v1/clusters/${clusterId}/workloads-overview${suffix}`,
      workloadOverviewSchema,
      { signal },
    )
  },

  health: (clusterId: string, namespaces: string[] = [], signal?: AbortSignal) => {
    const suffix = namespaces.length > 0 ? `?namespace=${encodeURIComponent(namespaces.join(','))}` : ''
    return request(`/api/v1/clusters/${clusterId}/health${suffix}`, healthReportSchema, { signal })
  },

  fleetHealth: (signal?: AbortSignal) => request('/api/v1/fleet/health', fleetSchema, { signal }),

  podContainers: (clusterId: string, namespace: string, name: string, signal?: AbortSignal) =>
    request(
      `/api/v1/clusters/${clusterId}/pod/${encodeURIComponent(namespace)}/${encodeURIComponent(name)}/containers`,
      containersSchema,
      { signal },
    ),

  helmReleases: (clusterId: string, namespace = '', signal?: AbortSignal) => {
    const suffix = namespace ? `?namespace=${encodeURIComponent(namespace)}` : ''
    return request(`/api/v1/clusters/${clusterId}/helm-releases${suffix}`, helmReleasesSchema, { signal })
  },

  helmRelease: (clusterId: string, namespace: string, name: string, signal?: AbortSignal) =>
    request(
      `/api/v1/clusters/${clusterId}/helm-releases/${encodeURIComponent(namespace)}/${encodeURIComponent(name)}`,
      helmDetailSchema,
      { signal },
    ),

  search: (query: string, signal?: AbortSignal) =>
    request(`/api/v1/search?q=${encodeURIComponent(query)}`, searchResultSchema, { signal }),

  clusterMetrics: (clusterId: string, window: string, signal?: AbortSignal) =>
    request(
      `/api/v1/clusters/${clusterId}/metrics?window=${encodeURIComponent(window)}`,
      clusterMetricsSchema,
      { signal },
    ),

  forwardablePorts: (
    clusterId: string,
    typeKey: string,
    namespace: string,
    name: string,
    signal?: AbortSignal,
  ) =>
    request(
      `/api/v1/clusters/${clusterId}/ports/${encodeURIComponent(namespace)}/${encodeURIComponent(name)}` +
        `?type=${encodeURIComponent(typeKey)}`,
      portsSchema,
      { signal },
    ),

  forwards: (clusterId: string, signal?: AbortSignal) =>
    request(`/api/v1/clusters/${clusterId}/forwards`, forwardsSchema, { signal }),

  startForward: (
    clusterId: string,
    body: {
      type: string
      namespace: string
      name: string
      port: number
      // Zero or absent means any free port, which is what "Random" sends.
      localPort?: number
      proxy?: boolean
    },
  ) => request(`/api/v1/clusters/${clusterId}/forwards`, forwardSchema, { method: 'POST', body }),

  stopForward: (forwardId: string) =>
    request(`/api/v1/forwards/${forwardId}`, emptySchema, { method: 'DELETE' }),

  podRestarts: (clusterId: string, namespace: string, name: string, signal?: AbortSignal) =>
    request(
      `/api/v1/clusters/${clusterId}/pod/${encodeURIComponent(namespace)}/${encodeURIComponent(name)}/restarts`,
      restartSummarySchema,
      { signal },
    ),

  relations: (
    clusterId: string,
    typeKey: string,
    params: { name: string; namespace?: string },
    signal?: AbortSignal,
  ) => {
    const query = new URLSearchParams({ name: params.name })
    if (params.namespace) query.set('namespace', params.namespace)

    return request(`/api/v1/clusters/${clusterId}/relations/${typeKey}?${query}`, relationsSchema, {
      signal,
    })
  },

  describe: (
    clusterId: string,
    typeKey: string,
    params: { name: string; namespace?: string },
    signal?: AbortSignal,
  ) => {
    const query = new URLSearchParams({ name: params.name })
    if (params.namespace) query.set('namespace', params.namespace)

    return request(`/api/v1/clusters/${clusterId}/describe/${typeKey}?${query}`, describeSchema, { signal })
  },

  secretKeys: (clusterId: string, namespace: string, name: string, signal?: AbortSignal) =>
    request(
      `/api/v1/clusters/${clusterId}/secret/${encodeURIComponent(namespace)}/${encodeURIComponent(name)}/keys`,
      secretKeysSchema,
      { signal },
    ),

  // One key at a time, deliberately: each disclosure is its own decision and leaves its
  // own audit record on the server.
  revealSecret: (clusterId: string, namespace: string, name: string, key: string, signal?: AbortSignal) =>
    request(
      `/api/v1/clusters/${clusterId}/secret/${encodeURIComponent(namespace)}/${encodeURIComponent(name)}/reveal?key=${encodeURIComponent(key)}`,
      revealSchema,
      { signal },
    ),

  apply: (clusterId: string, body: { manifest: string; dryRun: boolean }) =>
    request(`/api/v1/clusters/${clusterId}/apply`, applyResultSchema, { method: 'POST', body }),

  deleteObject: (
    clusterId: string,
    typeKey: string,
    params: { name: string; namespace?: string; propagation?: string },
  ) => {
    const query = new URLSearchParams({ name: params.name })
    if (params.namespace) query.set('namespace', params.namespace)
    if (params.propagation) query.set('propagation', params.propagation)

    return request(`/api/v1/clusters/${clusterId}/object/${typeKey}?${query}`, z.object({ deleted: z.boolean() }), {
      method: 'DELETE',
    })
  },

  scale: (clusterId: string, body: { typeKey: string; namespace: string; name: string; replicas: number }) =>
    request(`/api/v1/clusters/${clusterId}/scale`, z.object({ replicas: z.number() }), { method: 'POST', body }),

  restart: (clusterId: string, body: { typeKey: string; namespace: string; name: string }) =>
    request(`/api/v1/clusters/${clusterId}/restart`, z.object({ restarted: z.boolean() }), {
      method: 'POST',
      body,
    }),

  evict: (clusterId: string, body: { namespace: string; name: string }) =>
    request(`/api/v1/clusters/${clusterId}/evict`, z.object({ evicted: z.boolean() }), { method: 'POST', body }),

  suspendCronJob: (clusterId: string, body: { namespace: string; name: string; suspended: boolean }) =>
    request(`/api/v1/clusters/${clusterId}/cronjob/suspend`, z.object({ suspended: z.boolean() }), {
      method: 'POST',
      body,
    }),

  triggerCronJob: (clusterId: string, body: { namespace: string; name: string }) =>
    request(`/api/v1/clusters/${clusterId}/cronjob/trigger`, z.object({ job: z.string() }), {
      method: 'POST',
      body,
    }),

  cordonNode: (clusterId: string, body: { name: string; unschedulable: boolean }) =>
    request(`/api/v1/clusters/${clusterId}/node/cordon`, z.object({ unschedulable: z.boolean() }), {
      method: 'POST',
      body,
    }),

  rolloutHistory: (clusterId: string, namespace: string, name: string, signal?: AbortSignal) =>
    request(
      `/api/v1/clusters/${clusterId}/rollout/${encodeURIComponent(namespace)}/${encodeURIComponent(name)}`,
      rolloutSchema,
      { signal },
    ),

  drainPlan: (clusterId: string, node: string, signal?: AbortSignal) =>
    request(`/api/v1/clusters/${clusterId}/drain-plan/${encodeURIComponent(node)}`, drainPlanSchema, {
      signal,
    }),

  drainNode: (clusterId: string, body: { name: string }) =>
    request(`/api/v1/clusters/${clusterId}/node/drain`, drainResultSchema, { method: 'POST', body }),

  rollback: (clusterId: string, body: { namespace: string; name: string; revision: number }) =>
    request(`/api/v1/clusters/${clusterId}/rollback`, z.object({ revision: z.number() }), {
      method: 'POST',
      body,
    }),

  audit: (params: { limit?: number } = {}, signal?: AbortSignal) => {
    const query = new URLSearchParams()
    if (params.limit) query.set('limit', String(params.limit))
    const suffix = query.size > 0 ? `?${query.toString()}` : ''
    return request(`/api/v1/audit${suffix}`, auditSchema, { signal })
  },
}
