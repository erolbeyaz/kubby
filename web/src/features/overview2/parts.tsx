import type { ReactNode } from 'react'

import { Icon, type IconName } from '@/components/Icon'

/**
 * The building blocks of Overview 2.
 *
 * The layout follows the design brief closely — 5-across KPI strip, rounded panels, pills,
 * bar rows — while every colour comes from Kubby's own tokens rather than the palette the
 * mock shipped with. Two screens of the same application that disagree about what "warning
 * orange" is teach the reader to check twice.
 *
 * Corner radius is one token so the whole screen can be flattened to Kubby's sharper house
 * style in one edit if this design is kept.
 */
export const RADIUS = '11px'

/** A grid of equal columns that collapses on narrow screens, as the brief specifies. */
export function Grid({ columns, children }: { columns: 2 | 3 | 4 | 5; children: ReactNode }) {
  const min = columns >= 5 ? '11rem' : columns === 4 ? '14rem' : columns === 3 ? '17rem' : '22rem'
  return (
    <div
      className="grid gap-2.5"
      style={{ gridTemplateColumns: `repeat(auto-fit, minmax(${min}, 1fr))` }}
    >
      {children}
    </div>
  )
}

export function Section({
  title,
  hint,
  right,
  children,
}: {
  title: string
  hint?: string | undefined
  right?: ReactNode | undefined
  children: ReactNode
}) {
  return (
    <section className="mt-[18px] flex flex-col gap-2.5">
      <div className="mx-0.5 flex items-baseline justify-between gap-3">
        <h2 className="flex items-baseline gap-2">
          <span style={{ fontSize: 'var(--text-body)', color: 'var(--text-primary)', fontWeight: 600 }}>
            {title}
          </span>
          {hint && (
            <span style={{ fontSize: 'var(--text-micro)', color: 'var(--text-muted)' }}>{hint}</span>
          )}
        </h2>
        {right}
      </div>
      {children}
    </section>
  )
}

export function Panel({
  title,
  meta,
  children,
  tone,
}: {
  title: string
  meta?: ReactNode | undefined
  children: ReactNode
  tone?: 'error' | 'warn' | undefined
}) {
  return (
    <article
      className="flex min-w-0 flex-col overflow-hidden border"
      style={{
        borderRadius: RADIUS,
        backgroundColor: 'var(--bg-surface)',
        borderColor: tone
          ? tone === 'error'
            ? 'var(--status-error)'
            : 'var(--status-warn)'
          : 'var(--border-subtle)',
      }}
    >
      <header className="flex items-center justify-between gap-2.5 px-[13px] pb-[9px] pt-3">
        <h3 style={{ fontSize: 'var(--text-micro)', color: 'var(--text-primary)', fontWeight: 600 }}>
          {title}
        </h3>
        {meta && (
          <span
            className="whitespace-nowrap"
            style={{ fontSize: 'var(--text-micro)', color: 'var(--text-muted)' }}
          >
            {meta}
          </span>
        )}
      </header>
      <div className="min-w-0 px-[13px] pb-[13px]">{children}</div>
    </article>
  )
}

/**
 * One headline number.
 *
 * `unknown` is a first-class state rather than a zero, and a tile Kubby could not read is
 * not clickable: a link from a number nobody measured leads to a list that proves nothing.
 */
export function Kpi({
  label,
  icon,
  value,
  detail,
  tone,
  unknown,
  onOpen,
}: {
  label: string
  icon: IconName
  value: ReactNode
  detail?: ReactNode | undefined
  tone?: string | undefined
  unknown?: boolean | undefined
  onOpen?: (() => void) | undefined
}) {
  const clickable = Boolean(onOpen) && !unknown

  const body = (
    <>
      <span
        className="flex items-center justify-between gap-2"
        style={{ fontSize: 'var(--text-micro)', color: 'var(--text-muted)' }}
      >
        <span className="truncate">{label}</span>
        <Icon name={icon} className="shrink-0 opacity-70" />
      </span>
      <span
        className="mt-2 font-mono tabular-nums"
        style={{
          fontSize: unknown ? '15px' : '21px',
          fontWeight: 500,
          letterSpacing: '-0.02em',
          color: unknown ? 'var(--text-muted)' : (tone ?? 'var(--text-primary)'),
        }}
        title={unknown ? 'This metric is not being collected' : undefined}
      >
        {unknown ? 'N/A' : value}
      </span>
      <span
        className="mt-[5px] truncate"
        style={{ fontSize: 'var(--text-micro)', color: 'var(--text-muted)' }}
      >
        {unknown ? 'not collected' : detail}
      </span>
    </>
  )

  const style = {
    borderRadius: RADIUS,
    backgroundColor: 'var(--bg-surface)',
    borderColor: 'var(--border-subtle)',
  }

  return clickable ? (
    <button
      type="button"
      onClick={onOpen}
      className="flex min-w-0 flex-col border p-3 text-left transition-colors hover:bg-[var(--bg-hover)]"
      style={style}
    >
      {body}
    </button>
  ) : (
    <div className="flex min-w-0 flex-col border p-3" style={style}>
      {body}
    </div>
  )
}

/** A status dot and a word, the way the brief marks node state. */
export function Pill({ tone, children }: { tone: 'good' | 'warn' | 'bad' | 'idle'; children: ReactNode }) {
  const colour =
    tone === 'good'
      ? 'var(--status-ok)'
      : tone === 'warn'
        ? 'var(--status-warn)'
        : tone === 'bad'
          ? 'var(--status-error)'
          : 'var(--text-muted)'

  return (
    <span
      className="inline-flex items-center gap-1.5 whitespace-nowrap rounded-full px-[7px] py-[3px]"
      style={{
        fontSize: '10px',
        color: colour,
        backgroundColor: `color-mix(in srgb, ${colour} 14%, transparent)`,
      }}
    >
      <span
        aria-hidden="true"
        className="h-1.5 w-1.5 rounded-full"
        style={{ backgroundColor: 'currentColor' }}
      />
      {children}
    </span>
  )
}

/** Name, bar, value — the row the brief uses everywhere a list is compared. */
export function BarRow({
  name,
  percent,
  value,
  tone = 'var(--accent)',
  onOpen,
}: {
  name: string
  percent: number
  value: string
  tone?: string | undefined
  onOpen?: (() => void) | undefined
}) {
  const width = Math.max(0, Math.min(100, percent))

  const body = (
    <>
      <span className="truncate" style={{ color: 'var(--text-secondary)' }} title={name}>
        {name}
      </span>
      <span
        className="h-[7px] overflow-hidden rounded-[5px]"
        style={{ backgroundColor: 'var(--bg-inset, rgba(255,255,255,0.07))' }}
      >
        <span className="block h-full rounded-[inherit]" style={{ width: `${width}%`, backgroundColor: tone }} />
      </span>
      <span
        className="min-w-9 text-right font-mono tabular-nums"
        style={{ color: 'var(--text-muted)' }}
      >
        {value}
      </span>
    </>
  )

  const className = 'grid items-center gap-[9px] py-0.5 text-left'
  const style = { gridTemplateColumns: 'minmax(90px, 1fr) 2fr auto', fontSize: 'var(--text-micro)' }

  return onOpen ? (
    <button type="button" onClick={onOpen} className={className} style={style}>
      {body}
    </button>
  ) : (
    <span className={className} style={style}>
      {body}
    </span>
  )
}

/** A labelled line in a panel: name and detail on the left, a figure on the right. */
export function HealthRow({
  name,
  detail,
  value,
  tone,
  onOpen,
}: {
  name: ReactNode
  detail?: ReactNode | undefined
  value: ReactNode
  tone?: string | undefined
  onOpen?: (() => void) | undefined
}) {
  const body = (
    <>
      <span className="min-w-0">
        <span className="block truncate" style={{ fontSize: 'var(--text-micro)', color: 'var(--text-primary)' }}>
          {name}
        </span>
        {detail && (
          <span className="mt-[3px] block truncate" style={{ fontSize: '10px', color: 'var(--text-muted)' }}>
            {detail}
          </span>
        )}
      </span>
      <span
        className="whitespace-nowrap text-right"
        style={{ fontSize: 'var(--text-micro)', fontWeight: 500, color: tone ?? 'var(--text-primary)' }}
      >
        {value}
      </span>
    </>
  )

  const className =
    'grid w-full items-center gap-3 border-b py-[9px] text-left last:border-b-0'
  const style = { gridTemplateColumns: '1fr auto', borderColor: 'var(--border-subtle)' }

  return onOpen ? (
    <button type="button" onClick={onOpen} className={`${className} hover:bg-[var(--bg-hover)]`} style={style}>
      {body}
    </button>
  ) : (
    <span className={className} style={style}>
      {body}
    </span>
  )
}

/** The one-line "nothing here" a panel shows instead of an empty box. */
export function Quiet({ children }: { children: ReactNode }) {
  return (
    <p className="py-1" style={{ fontSize: 'var(--text-micro)', color: 'var(--text-muted)' }}>
      {children}
    </p>
  )
}

/** A reading rendered as a figure or, when it was never collected, as N/A. */
export function Figure({
  reading,
  render,
  suffix = '',
}: {
  reading: { value: number; known: boolean }
  render?: ((value: number) => string) | undefined
  suffix?: string | undefined
}) {
  if (!reading.known) {
    return (
      <span style={{ color: 'var(--text-muted)' }} title="This metric is not being collected">
        N/A
      </span>
    )
  }
  return <>{(render ? render(reading.value) : String(Math.round(reading.value))) + suffix}</>
}
