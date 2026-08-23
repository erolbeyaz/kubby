const KEY_LINE = /^(\s*)(-\s+)?([\w.\-/[\]]+)(:)(\s*)(.*)$/
const LIST_ITEM = /^(\s*)(-\s+)(.*)$/

/**
 * The YAML shown while the editor's chunk is still arriving.
 *
 * It deliberately mirrors the editor's metrics and colouring rather than being plain
 * text: swapping unstyled text for a highlighted editor reads as the view breaking and
 * repairing itself, and the two frames were far enough apart to be noticed.
 */
export function YamlFallback({ value }: { value: string }) {
  const lines = value.split('\n')
  const gutter = String(lines.length).length

  return (
    <div
      className="h-full w-full overflow-auto font-mono"
      style={{
        backgroundColor: 'var(--bg-base)',
        fontSize: '12.5px',
        lineHeight: 1.65,
        paddingTop: 10,
        paddingBottom: 10,
      }}
    >
      {lines.map((line, index) => (
        <div key={index} className="flex whitespace-pre">
          <span
            className="shrink-0 select-none pr-4 text-right"
            style={{ width: `${gutter + 3}ch`, color: '#4a4a4a' }}
          >
            {index + 1}
          </span>
          <span className="pr-4">{colour(line)}</span>
        </div>
      ))}
    </div>
  )
}

/** The same split the editor's theme makes: structure is quiet, values are bright. */
function colour(line: string) {
  if (line.trimStart().startsWith('#')) return <span style={{ color: '#6b6b6b' }}>{line}</span>

  const keyed = KEY_LINE.exec(line)
  if (keyed) {
    const [, indent, dash, key, colon, space, rest] = keyed
    return (
      <>
        {indent}
        {dash && <span style={{ color: '#6b6b6b' }}>{dash}</span>}
        <span style={{ color: '#7dd3fc' }}>{key}</span>
        <span style={{ color: '#6b6b6b' }}>{colon}</span>
        {space}
        <span style={{ color: scalarColour(rest ?? '') }}>{rest}</span>
      </>
    )
  }

  const item = LIST_ITEM.exec(line)
  if (item) {
    const [, indent, dash, rest] = item
    return (
      <>
        {indent}
        <span style={{ color: '#6b6b6b' }}>{dash}</span>
        <span style={{ color: scalarColour(rest ?? '') }}>{rest}</span>
      </>
    )
  }

  return <span style={{ color: '#e8e8e8' }}>{line}</span>
}

function scalarColour(value: string): string {
  const trimmed = value.trim()
  if (trimmed === '') return '#e8e8e8'
  if (/^(true|false|null|~)$/.test(trimmed)) return '#f0a868'
  if (/^-?\d+(\.\d+)?$/.test(trimmed)) return '#f0a868'
  return '#e8e8e8'
}
