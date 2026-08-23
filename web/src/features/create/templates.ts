/** One starting manifest. */
export interface Template {
  kind: string
  /** The group heading it appears under; empty means the main list. */
  group?: string
  /** Namespaced kinds get a namespace line; cluster-scoped ones must not have one. */
  namespaced: boolean
  manifest: string
}

const GATEWAY = 'GATEWAY-API'

/**
 * The starting manifests offered when creating a resource.
 *
 * Hand-written rather than generated for the kinds people actually create: a skeleton
 * produced from the OpenAPI schema is complete and unreadable, and the point of a
 * template is to be edited, not decoded. Anything not listed here — a CRD, a kind added
 * after this file — falls back to a schema-derived skeleton (ADR-054).
 */
export const TEMPLATES: Template[] = [
  {
    kind: 'ClusterRole',
    namespaced: false,
    manifest: `apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: example-cluster-role
rules:
  - apiGroups: [""]
    resources: ["pods"]
    verbs: ["get", "list", "watch"]
`,
  },
  {
    kind: 'ClusterRoleBinding',
    namespaced: false,
    manifest: `apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: example-cluster-role-binding
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: example-cluster-role
subjects:
  - kind: ServiceAccount
    name: default
    namespace: default
`,
  },
  {
    kind: 'ConfigMap',
    namespaced: true,
    manifest: `apiVersion: v1
kind: ConfigMap
metadata:
  name: example-config
  namespace: {{namespace}}
data:
  key: value
`,
  },
  {
    kind: 'CronJob',
    namespaced: true,
    manifest: `apiVersion: batch/v1
kind: CronJob
metadata:
  name: example-cronjob
  namespace: {{namespace}}
spec:
  schedule: "0 * * * *"
  jobTemplate:
    spec:
      template:
        spec:
          restartPolicy: OnFailure
          containers:
            - name: worker
              image: busybox:1.36
              command: ["sh", "-c", "date"]
`,
  },
  {
    kind: 'DaemonSet',
    namespaced: true,
    manifest: `apiVersion: apps/v1
kind: DaemonSet
metadata:
  name: example-daemonset
  namespace: {{namespace}}
spec:
  selector:
    matchLabels:
      app: example
  template:
    metadata:
      labels:
        app: example
    spec:
      containers:
        - name: agent
          image: busybox:1.36
          command: ["sh", "-c", "sleep infinity"]
`,
  },
  {
    kind: 'Deployment',
    namespaced: true,
    manifest: `apiVersion: apps/v1
kind: Deployment
metadata:
  name: nginx-deployment
  namespace: {{namespace}}
  labels:
    app: nginx
spec:
  replicas: 3
  selector:
    matchLabels:
      app: nginx
  template:
    metadata:
      labels:
        app: nginx
    spec:
      containers:
        - name: nginx
          image: nginx:1.14.2
          ports:
            - containerPort: 80
`,
  },
  {
    kind: 'HorizontalPodAutoscaler',
    namespaced: true,
    manifest: `apiVersion: autoscaling/v2
kind: HorizontalPodAutoscaler
metadata:
  name: example-hpa
  namespace: {{namespace}}
spec:
  scaleTargetRef:
    apiVersion: apps/v1
    kind: Deployment
    name: example-deployment
  minReplicas: 1
  maxReplicas: 10
  metrics:
    - type: Resource
      resource:
        name: cpu
        target:
          type: Utilization
          averageUtilization: 80
`,
  },
  {
    kind: 'Ingress',
    namespaced: true,
    manifest: `apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: example-ingress
  namespace: {{namespace}}
spec:
  rules:
    - host: example.com
      http:
        paths:
          - path: /
            pathType: Prefix
            backend:
              service:
                name: example-service
                port:
                  number: 80
`,
  },
  {
    kind: 'Job',
    namespaced: true,
    manifest: `apiVersion: batch/v1
kind: Job
metadata:
  name: example-job
  namespace: {{namespace}}
spec:
  template:
    spec:
      restartPolicy: Never
      containers:
        - name: worker
          image: busybox:1.36
          command: ["sh", "-c", "echo done"]
`,
  },
  {
    kind: 'LimitRange',
    namespaced: true,
    manifest: `apiVersion: v1
kind: LimitRange
metadata:
  name: example-limits
  namespace: {{namespace}}
spec:
  limits:
    - type: Container
      default:
        cpu: 500m
        memory: 512Mi
      defaultRequest:
        cpu: 100m
        memory: 128Mi
`,
  },
  {
    kind: 'Namespace',
    namespaced: false,
    manifest: `apiVersion: v1
kind: Namespace
metadata:
  name: example-namespace
`,
  },
  {
    kind: 'NetworkPolicy',
    namespaced: true,
    manifest: `apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: example-network-policy
  namespace: {{namespace}}
spec:
  podSelector:
    matchLabels:
      app: example
  policyTypes:
    - Ingress
  ingress:
    - from:
        - podSelector:
            matchLabels:
              app: caller
`,
  },
  {
    kind: 'PersistentVolumeClaim',
    namespaced: true,
    manifest: `apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: example-claim
  namespace: {{namespace}}
spec:
  accessModes: ["ReadWriteOnce"]
  resources:
    requests:
      storage: 1Gi
`,
  },
  {
    kind: 'Pod',
    namespaced: true,
    manifest: `apiVersion: v1
kind: Pod
metadata:
  name: static-web
  namespace: {{namespace}}
  labels:
    role: myrole
spec:
  containers:
    - name: web
      image: nginx
      ports:
        - name: web
          containerPort: 80
          protocol: TCP
`,
  },
  {
    kind: 'PodDisruptionBudget',
    namespaced: true,
    manifest: `apiVersion: policy/v1
kind: PodDisruptionBudget
metadata:
  name: example-pdb
  namespace: {{namespace}}
spec:
  minAvailable: 1
  selector:
    matchLabels:
      app: example
`,
  },
  {
    kind: 'ResourceQuota',
    namespaced: true,
    manifest: `apiVersion: v1
kind: ResourceQuota
metadata:
  name: example-quota
  namespace: {{namespace}}
spec:
  hard:
    requests.cpu: "4"
    requests.memory: 8Gi
    pods: "20"
`,
  },
  {
    kind: 'Role',
    namespaced: true,
    manifest: `apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  name: example-role
  namespace: {{namespace}}
rules:
  - apiGroups: [""]
    resources: ["pods"]
    verbs: ["get", "list", "watch"]
`,
  },
  {
    kind: 'RoleBinding',
    namespaced: true,
    manifest: `apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: example-role-binding
  namespace: {{namespace}}
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: Role
  name: example-role
subjects:
  - kind: ServiceAccount
    name: default
`,
  },
  {
    kind: 'Secret',
    namespaced: true,
    manifest: `apiVersion: v1
kind: Secret
metadata:
  name: example-secret
  namespace: {{namespace}}
type: Opaque
stringData:
  key: replace-me
`,
  },
  {
    kind: 'Service',
    namespaced: true,
    manifest: `apiVersion: v1
kind: Service
metadata:
  name: example-service
  namespace: {{namespace}}
spec:
  selector:
    app: example
  ports:
    - protocol: TCP
      port: 80
      targetPort: 8080
`,
  },
  {
    kind: 'ServiceAccount',
    namespaced: true,
    manifest: `apiVersion: v1
kind: ServiceAccount
metadata:
  name: example-service-account
  namespace: {{namespace}}
`,
  },
  {
    kind: 'StatefulSet',
    namespaced: true,
    manifest: `apiVersion: apps/v1
kind: StatefulSet
metadata:
  name: web
  namespace: {{namespace}}
spec:
  selector:
    matchLabels:
      app: nginx
  serviceName: "nginx"
  replicas: 3
  template:
    metadata:
      labels:
        app: nginx
    spec:
      terminationGracePeriodSeconds: 10
      containers:
        - name: nginx
          image: registry.k8s.io/nginx-slim:0.8
          ports:
            - containerPort: 80
              name: web
          volumeMounts:
            - name: www
              mountPath: /usr/share/nginx/html
  volumeClaimTemplates:
    - metadata:
        name: www
      spec:
        accessModes: ["ReadWriteOnce"]
        storageClassName: "my-storage-class"
        resources:
          requests:
            storage: 1Gi
`,
  },
  {
    kind: 'StorageClass',
    namespaced: false,
    manifest: `apiVersion: storage.k8s.io/v1
kind: StorageClass
metadata:
  name: example-storage-class
provisioner: kubernetes.io/no-provisioner
volumeBindingMode: WaitForFirstConsumer
`,
  },
  {
    kind: 'Gateway',
    group: GATEWAY,
    namespaced: true,
    manifest: `apiVersion: gateway.networking.k8s.io/v1
kind: Gateway
metadata:
  name: example-gateway
  namespace: {{namespace}}
spec:
  gatewayClassName: example-gateway-class
  listeners:
    - name: http
      protocol: HTTP
      port: 80
`,
  },
  {
    kind: 'GatewayClass',
    group: GATEWAY,
    namespaced: false,
    manifest: `apiVersion: gateway.networking.k8s.io/v1
kind: GatewayClass
metadata:
  name: example-gateway-class
spec:
  controllerName: example.com/gateway-controller
`,
  },
  {
    kind: 'GRPCRoute',
    group: GATEWAY,
    namespaced: true,
    manifest: `apiVersion: gateway.networking.k8s.io/v1
kind: GRPCRoute
metadata:
  name: example-grpc-route
  namespace: {{namespace}}
spec:
  parentRefs:
    - name: example-gateway
  rules:
    - backendRefs:
        - name: example-service
          port: 50051
`,
  },
  {
    kind: 'HTTPRoute',
    group: GATEWAY,
    namespaced: true,
    manifest: `apiVersion: gateway.networking.k8s.io/v1
kind: HTTPRoute
metadata:
  name: example-http-route
  namespace: {{namespace}}
spec:
  parentRefs:
    - name: example-gateway
  rules:
    - matches:
        - path:
            type: PathPrefix
            value: /
      backendRefs:
        - name: example-service
          port: 80
`,
  },
  {
    kind: 'ReferenceGrant',
    group: GATEWAY,
    namespaced: true,
    manifest: `apiVersion: gateway.networking.k8s.io/v1beta1
kind: ReferenceGrant
metadata:
  name: example-reference-grant
  namespace: {{namespace}}
spec:
  from:
    - group: gateway.networking.k8s.io
      kind: HTTPRoute
      namespace: default
  to:
    - group: ""
      kind: Service
`,
  },
]

/**
 * Fills the namespace in, or leaves the line out.
 *
 * When the list spans several namespaces there is no right answer, so the field is left
 * blank for the person to fill: guessing would create the object somewhere they did not
 * choose, which is the one mistake a template must not make (ADR-054).
 */
export function renderTemplate(template: Template, namespaces: string[]): string {
  if (!template.namespaced) return template.manifest

  const namespace = namespaces.length === 1 ? namespaces[0] : ''
  return template.manifest.replace('{{namespace}}', namespace ?? '')
}

/** Alphabetical within each group, with the ungrouped kinds first. */
export function groupedTemplates(available: Set<string> | null): { group: string; items: Template[] }[] {
  const usable = available
    ? TEMPLATES.filter((template) => available.has(template.kind))
    : TEMPLATES

  const groups = new Map<string, Template[]>()
  for (const template of usable) {
    const key = template.group ?? ''
    groups.set(key, [...(groups.get(key) ?? []), template])
  }

  return [...groups.entries()]
    .sort(([a], [b]) => a.localeCompare(b))
    .map(([group, items]) => ({
      group,
      items: [...items].sort((a, b) => a.kind.localeCompare(b.kind)),
    }))
}
