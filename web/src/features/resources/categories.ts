import type { IconName } from '@/components/Icon'
import type { ResourceCategory } from '@/lib/api'

/** The order the navigation presents categories in, and how each is labelled. */
export const CATEGORY_ORDER: readonly ResourceCategory[] = [
  'workload',
  'config',
  'network',
  'storage',
  'access',
  'cluster',
  'custom',
]

export const CATEGORY_LABEL: Record<ResourceCategory, string> = {
  workload: 'Workloads',
  config: 'Config',
  network: 'Network',
  storage: 'Storage',
  access: 'Access Control',
  cluster: 'Cluster',
  custom: 'Custom Resources',
}

export const CATEGORY_ICON: Record<ResourceCategory, IconName> = {
  workload: 'workloads',
  config: 'settings',
  network: 'network',
  storage: 'storage',
  access: 'shield',
  cluster: 'clusters',
  custom: 'events',
}
