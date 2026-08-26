import type { Environment } from '@/lib/api'

/**
 * Environment drives behaviour, the label is only how a team names it. Colours are
 * deliberately loud for production: mistaking which cluster you are on is the mistake
 * this tool has to make hardest.
 */
export const ENVIRONMENTS: readonly Environment[] = ['prod', 'preprod', 'test', 'dr']

export const ENVIRONMENT_COLOR: Record<Environment, string> = {
  prod: 'var(--env-prod)',
  preprod: 'var(--env-preprod)',
  test: 'var(--env-test)',
  dr: 'var(--env-dr)',
}

export const ENVIRONMENT_HINT: Record<Environment, string> = {
  prod: 'Destructive actions require typing the resource name',
  preprod: 'Pre-production',
  test: 'Test and development',
  dr: 'Disaster recovery site',
}

export function environmentColor(environment: Environment, override: string): string {
  return override || ENVIRONMENT_COLOR[environment]
}

/**
 * How a tier is titled where it heads a group rather than sits in a field.
 *
 * Upper case because a band is a heading, and because "PRODUCTION" is harder to skim
 * past than "prod" — which is the entire point of the grouping.
 */
export const ENVIRONMENT_TITLE: Record<Environment, string> = {
  prod: 'Production',
  preprod: 'Pre-production',
  test: 'Test',
  dr: 'Disaster recovery',
}

/**
 * Registry order: production first, always.
 *
 * Not alphabetical and not by when it was added. The first question on this screen is
 * "is anything in production broken", and a tier that sorts by name puts "dr" above it.
 */
export function byEnvironment(a: Environment, b: Environment): number {
  return ENVIRONMENTS.indexOf(a) - ENVIRONMENTS.indexOf(b)
}
