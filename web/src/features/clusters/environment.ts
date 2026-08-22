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
