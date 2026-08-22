import { Callout } from '@/components/Callout'
import { LoginScreen } from '@/features/auth/LoginScreen'
import { SetupWizard } from '@/features/setup/SetupWizard'

import { Shell } from './Shell'
import { useSession } from './use-session'

/** Chooses the top-level screen: first-run wizard, sign-in, or the workspace. */
export function App() {
  const { state, me, refresh, signOut, signOutFailed } = useSession()

  switch (state) {
    case 'loading':
      return <Centered>Connecting…</Centered>

    case 'unreachable':
      return (
        <Centered>
          <Callout tone="error" title="Kubby is unreachable">
            The API did not respond. Check that the server is running, then reload.
          </Callout>
        </Centered>
      )

    case 'setup':
      return <SetupWizard onComplete={refresh} />

    case 'login':
      return <LoginScreen onAuthenticated={refresh} signOutFailed={signOutFailed} />

    case 'ready':
      return me ? <Shell me={me} onSignOut={signOut} /> : <Centered>Loading…</Centered>
  }
}

function Centered({ children }: { children: React.ReactNode }) {
  return (
    <div
      className="flex h-full items-center justify-center p-6 text-[13px]"
      style={{ backgroundColor: 'var(--bg-base)', color: 'var(--text-muted)' }}
    >
      <div className="w-full max-w-sm text-center">{children}</div>
    </div>
  )
}
