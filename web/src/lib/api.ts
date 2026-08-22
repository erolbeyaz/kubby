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

  audit: (params: { limit?: number } = {}, signal?: AbortSignal) => {
    const query = new URLSearchParams()
    if (params.limit) query.set('limit', String(params.limit))
    const suffix = query.size > 0 ? `?${query.toString()}` : ''
    return request(`/api/v1/audit${suffix}`, auditSchema, { signal })
  },
}
