import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'

import { Button } from '@/components/Button'
import { Callout } from '@/components/Callout'
import { EmptyState } from '@/components/EmptyState'
import { Field, Select, TextInput } from '@/components/Field'
import { ApiError, api, type Me, type Role, type User } from '@/lib/api'
import { formatAbsolute, formatAge } from '@/lib/time'

const ROLE_DESCRIPTION: Record<Role, string> = {
  admin: 'Manages clusters, users and settings',
  user: 'Reads and writes the clusters they are granted',
  readonly: 'Reads only',
}

interface UsersScreenProps {
  me: Me
}

export function UsersScreen({ me }: UsersScreenProps) {
  const [creating, setCreating] = useState(false)

  const users = useQuery({
    queryKey: ['users'],
    queryFn: ({ signal }) => api.users(signal),
  })

  const list = users.data?.users ?? []

  return (
    <div className="flex h-full flex-col">
      <header
        className="flex h-11 shrink-0 items-center justify-between border-b px-4"
        style={{ borderColor: 'var(--border-subtle)' }}
      >
        <h2 className="text-[13px] font-semibold" style={{ color: 'var(--text-primary)' }}>
          Users
          <span className="ml-2 font-mono text-[12px]" style={{ color: 'var(--text-muted)' }}>
            {list.length}
          </span>
        </h2>
        <Button variant="primary" onClick={() => setCreating((v) => !v)}>
          {creating ? 'Cancel' : 'Add user'}
        </Button>
      </header>

      {creating && (
        <div className="border-b p-4" style={{ borderColor: 'var(--border-subtle)' }}>
          <CreateUserForm onCreated={() => setCreating(false)} />
        </div>
      )}

      <div className="min-h-0 flex-1 overflow-auto">
        {users.isLoading && (
          <p className="p-4 text-[13px]" style={{ color: 'var(--text-muted)' }}>
            Loading users…
          </p>
        )}

        {users.isError && (
          <div className="p-4">
            <Callout tone="error" title="Could not load users">
              {users.error instanceof ApiError ? users.error.message : 'Unexpected error'}
            </Callout>
          </div>
        )}

        {!users.isLoading && list.length === 0 && (
          <EmptyState title="No users" description="Add a teammate to give them access." />
        )}

        {list.length > 0 && <UserTable users={list} currentUserId={me.user.id} />}
      </div>
    </div>
  )
}

function UserTable({ users, currentUserId }: { users: User[]; currentUserId: string }) {
  const queryClient = useQueryClient()
  const [failure, setFailure] = useState<ApiError | null>(null)

  const update = useMutation({
    mutationFn: ({ id, body }: { id: string; body: { role?: Role; isActive?: boolean } }) =>
      api.updateUser(id, body),
    onSuccess: () => {
      setFailure(null)
      void queryClient.invalidateQueries({ queryKey: ['users'] })
    },
    onError: (error) => setFailure(error instanceof ApiError ? error : null),
  })

  return (
    <>
      {failure && (
        <div className="p-4 pb-0">
          <Callout tone="error" title="Change refused" requestId={failure.requestId}>
            {failure.message}
          </Callout>
        </div>
      )}

      <table className="w-full text-left text-[13px]">
        <thead>
          <tr style={{ color: 'var(--text-muted)' }}>
            <th className="px-4 py-2 font-medium">User</th>
            <th className="px-4 py-2 font-medium">Role</th>
            <th className="px-4 py-2 font-medium">2FA</th>
            <th className="px-4 py-2 font-medium">Last sign-in</th>
            <th className="px-4 py-2 font-medium">Status</th>
          </tr>
        </thead>
        <tbody>
          {users.map((user) => {
            const isSelf = user.id === currentUserId

            return (
              <tr
                key={user.id}
                className="border-t"
                style={{ borderColor: 'var(--border-subtle)', opacity: user.isActive ? 1 : 0.5 }}
              >
                <td className="px-4 py-2">
                  <div style={{ color: 'var(--text-primary)' }}>{user.displayName}</div>
                  <div className="font-mono text-[12px]" style={{ color: 'var(--text-muted)' }}>
                    {user.email}
                    {isSelf && <span style={{ color: 'var(--accent)' }}> · you</span>}
                  </div>
                </td>

                <td className="px-4 py-2">
                  <Select
                    aria-label={`Role for ${user.email}`}
                    value={user.role}
                    disabled={isSelf || update.isPending}
                    title={isSelf ? 'You cannot change your own role' : ROLE_DESCRIPTION[user.role]}
                    onChange={(e) =>
                      update.mutate({ id: user.id, body: { role: e.target.value as Role } })
                    }
                  >
                    {(['admin', 'user', 'readonly'] as const).map((role) => (
                      <option key={role} value={role}>
                        {role}
                      </option>
                    ))}
                  </Select>
                </td>

                <td className="px-4 py-2 font-mono text-[12px]">
                  <span style={{ color: user.mfaEnrolled ? 'var(--status-ok)' : 'var(--text-muted)' }}>
                    {user.mfaEnrolled ? 'enrolled' : 'not enrolled'}
                  </span>
                </td>

                <td
                  className="px-4 py-2 font-mono text-[12px]"
                  style={{ color: 'var(--text-secondary)' }}
                  title={user.lastLoginAt ? formatAbsolute(user.lastLoginAt) : undefined}
                >
                  {user.lastLoginAt ? `${formatAge(user.lastLoginAt)} ago` : 'never'}
                </td>

                <td className="px-4 py-2">
                  <Button
                    variant={user.isActive ? 'danger' : 'secondary'}
                    disabled={isSelf || update.isPending}
                    title={isSelf ? 'You cannot deactivate yourself' : undefined}
                    onClick={() =>
                      update.mutate({ id: user.id, body: { isActive: !user.isActive } })
                    }
                  >
                    {user.isActive ? 'Deactivate' : 'Reactivate'}
                  </Button>
                </td>
              </tr>
            )
          })}
        </tbody>
      </table>
    </>
  )
}

function CreateUserForm({ onCreated }: { onCreated: () => void }) {
  const queryClient = useQueryClient()
  const [email, setEmail] = useState('')
  const [displayName, setDisplayName] = useState('')
  const [password, setPassword] = useState('')
  const [role, setRole] = useState<Role>('user')

  const create = useMutation({
    mutationFn: () => api.createUser({ email, displayName, password, role }),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ['users'] })
      onCreated()
    },
  })

  const error = create.error instanceof ApiError ? create.error : null

  return (
    <form
      className="flex flex-col gap-3"
      onSubmit={(event) => {
        event.preventDefault()
        create.mutate()
      }}
    >
      <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-4">
        <Field label="Email">
          {(id) => (
            <TextInput id={id} type="email" required value={email} onChange={(e) => setEmail(e.target.value)} />
          )}
        </Field>
        <Field label="Display name">
          {(id) => (
            <TextInput id={id} required value={displayName} onChange={(e) => setDisplayName(e.target.value)} />
          )}
        </Field>
        <Field label="Initial password">
          {(id) => (
            <TextInput
              id={id}
              type="password"
              required
              autoComplete="new-password"
              value={password}
              onChange={(e) => setPassword(e.target.value)}
            />
          )}
        </Field>
        <Field label="Role" hint={ROLE_DESCRIPTION[role]}>
          {(id) => (
            <Select id={id} value={role} onChange={(e) => setRole(e.target.value as Role)}>
              {(['admin', 'user', 'readonly'] as const).map((value) => (
                <option key={value} value={value}>
                  {value}
                </option>
              ))}
            </Select>
          )}
        </Field>
      </div>

      {error && (
        <Callout tone="error" title="Could not create the user" requestId={error.requestId}>
          {error.message}
        </Callout>
      )}

      <div>
        <Button type="submit" variant="primary" loading={create.isPending}>
          Create user
        </Button>
      </div>
    </form>
  )
}
