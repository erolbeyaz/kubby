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

/**
 * How a kind is written in the navigation.
 *
 * Plural, and spaced the way people say it: "Daemon Sets", not "DaemonSets". The API's
 * spelling belongs in the API; a list of things is read as a list of things.
 */
const IRREGULAR: Record<string, string> = {
  Endpoints: 'Endpoints',
  NetworkPolicy: 'Network Policies',
  IngressClass: 'Ingress Classes',
  StorageClass: 'Storage Classes',
  PriorityClass: 'Priority Classes',
  RuntimeClass: 'Runtime Classes',
  Ingress: 'Ingresses',
  PodDisruptionBudget: 'Pod Disruption Budgets',
  HorizontalPodAutoscaler: 'Horizontal Pod Autoscalers',
  MutatingWebhookConfiguration: 'Mutating Webhook Configurations',
  ValidatingWebhookConfiguration: 'Validating Webhook Configurations',
  PersistentVolumeClaim: 'Persistent Volume Claims',
  PersistentVolume: 'Persistent Volumes',
  ReplicationController: 'Replication Controllers',
}

export function kindLabel(kind: string): string {
  const irregular = IRREGULAR[kind]
  if (irregular) return irregular

  const spaced = kind.replace(/([a-z])([A-Z])/g, '$1 $2')
  return spaced.endsWith('s') ? spaced : `${spaced}s`
}
