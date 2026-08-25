import { TerminalPane } from './TerminalPane'

interface ClusterTerminalPaneProps {
  clusterId: string
  clusterName: string
}

/**
 * A terminal with kubectl already pointed at the selected cluster.
 *
 * It runs where Kubby runs, not on the reader's machine: a browser cannot start a local
 * process, which is the whole reason this exists rather than a shortcut that opens their
 * own terminal (ADR-012).
 */
export function ClusterTerminalPane({ clusterId, clusterName }: ClusterTerminalPaneProps) {
  return (
    <TerminalPane
      path={`/api/v1/clusters/${clusterId}/terminal`}
      opening="Opening a terminal…"
      // A manifest or chart on the reader's machine is the one they mean to apply, so it
      // is dragged straight into the session the command runs in.
      acceptsFiles
      context={
        <>
          Kubernetes cluster{' '}
          <span className="font-mono" style={{ color: 'var(--accent)' }}>
            {clusterName}
          </span>{' '}
          is in context.
        </>
      }
    />
  )
}
