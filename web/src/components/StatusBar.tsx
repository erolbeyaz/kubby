import monogram from '@/assets/monogram.png'
import signature from '@/assets/signature.png'
import type { VersionInfo } from '@/lib/api'

export type ConnectionState = 'connecting' | 'ready' | 'degraded' | 'offline'

interface StatusBarProps {
  connection: ConnectionState
  version: VersionInfo | undefined
  detail?: string | undefined
}

const CONNECTION_LABEL: Record<ConnectionState, string> = {
  connecting: 'Connecting',
  ready: 'Ready',
  degraded: 'Degraded',
  offline: 'Offline',
}

const CONNECTION_COLOR: Record<ConnectionState, string> = {
  connecting: 'var(--status-unknown)',
  ready: 'var(--status-ok)',
  degraded: 'var(--status-warn)',
  offline: 'var(--status-error)',
}

/** The always-present bottom strip: connection state, build identity, key hints. */
export function StatusBar({ connection, version, detail }: StatusBarProps) {
  return (
    <footer
      className="relative flex h-7 shrink-0 items-center gap-3 border-t px-3 font-mono text-[12px]"
      style={{
        backgroundColor: 'var(--bg-surface)',
        borderColor: 'var(--border-subtle)',
        color: 'var(--text-muted)',
      }}
    >
      <span className="flex items-center gap-1.5" title={detail ?? CONNECTION_LABEL[connection]}>
        <span
          aria-hidden="true"
          className="inline-block h-1.5 w-1.5 rounded-full"
          style={{ backgroundColor: CONNECTION_COLOR[connection] }}
        />
        <span style={{ color: 'var(--text-secondary)' }}>{CONNECTION_LABEL[connection]}</span>
      </span>

      {detail && <span className="truncate">{detail}</span>}

      {/* Centred on the strip itself rather than in the flow, so it stays put whatever
          the status text on either side happens to say. Only the signature takes the
          pointer; the rest of the strip is left alone. */}
      <span className="signature-slot absolute left-1/2 flex -translate-x-1/2 items-center">
        <img src={monogram} alt="" aria-hidden="true" className="monogram select-none" />
        <img src={signature} alt="powered by erolbeyaz" className="signature select-none" />
      </span>

      <span className="ml-auto flex items-center gap-3">
        <span className="flex items-center gap-1">
          <span className="kbd">Ctrl</span>
          <span className="kbd">K</span>
          <span>command palette</span>
        </span>
        {version && (
          <span title={`built ${version.buildDate} · ${version.goVersion}`}>
            {version.version}
            <span style={{ color: 'var(--border-strong)' }}> · </span>
            {version.commitSha}
          </span>
        )}
      </span>
    </footer>
  )
}
