# Helm Chart Installation Guide

This guide provides detailed instructions for installing the Garage Operator using Helm.

## Overview

The Garage Operator Helm chart is automatically generated from Kustomize manifests and follows Kubernetes best practices for operator deployments. The chart structure separates CRDs (Custom Resource Definitions) from the main operator resources.

## Chart Structure

```
garage-operator/
├── Chart.yaml              # Chart metadata
├── values.yaml             # Default configuration values
├── crds/                   # CustomResourceDefinitions (installed separately)
│   ├── garage.dimedis.io_garageclusters.yaml
│   └── garage.dimedis.io_garagebuckets.yaml
└── templates/              # Kubernetes manifests
    └── manifests.yaml      # Operator resources (Deployment, RBAC, Service, etc.)
```

## Why CRDs are Separate

CustomResourceDefinitions are intentionally kept outside of the Helm-managed resources because:

1. **Cluster-wide scope**: CRDs are cluster-level resources that define custom APIs
2. **Lifecycle independence**: CRDs should persist even if the operator is uninstalled
3. **Startup dependencies**: CRDs must exist before the operator starts watching for custom resources
4. **Upgrade safety**: Separating CRDs prevents accidental schema changes during operator upgrades
5. **Ownership clarity**: Avoids Helm ownership metadata conflicts

## Installation Methods

### Method 1: From Release Archive (Recommended)

This method downloads the chart package and uses the included CRDs:

```bash
# Download the chart
VERSION=1.0.0
curl -L https://github.com/dimedis-gmbh/garage-operator/releases/download/v${VERSION}/garage-operator-${VERSION}.tgz -o garage-operator.tgz

# Extract the chart
tar -xzf garage-operator.tgz

# Install CRDs first
kubectl apply -f garage-operator/crds/

# Install the Helm chart
helm install garage-operator ./garage-operator \
  --namespace garage-operator-system \
  --create-namespace
```

### Method 2: Direct URL Installation

Install directly from the release URL:

```bash
VERSION=1.0.0

# Install CRDs from GitHub
kubectl apply -f https://raw.githubusercontent.com/dimedis-gmbh/garage-operator/v${VERSION}/config/crd/bases/garage.dimedis.io_garageclusters.yaml
kubectl apply -f https://raw.githubusercontent.com/dimedis-gmbh/garage-operator/v${VERSION}/config/crd/bases/garage.dimedis.io_garagebuckets.yaml

# Install the chart
helm install garage-operator \
  https://github.com/dimedis-gmbh/garage-operator/releases/download/v${VERSION}/garage-operator-${VERSION}.tgz \
  --namespace garage-operator-system \
  --create-namespace
```

### Method 3: With Custom Values

Create a custom values file to override defaults:

```bash
# Create custom values
cat > my-values.yaml <<EOF
controllerManager:
  replicas: 2
  container:
    image:
      repository: ghcr.io/dimedis-gmbh/garage-operator
      tag: v1.0.0
      pullPolicy: IfNotPresent
    resources:
      limits:
        cpu: 1000m
        memory: 512Mi
      requests:
        cpu: 100m
        memory: 128Mi

metricsService:
  enable: true
  type: ClusterIP
  ports:
    - name: https
      port: 8443
      protocol: TCP
      targetPort: 8443
EOF

# Install CRDs
kubectl apply -f garage-operator/crds/

# Install with custom values
helm install garage-operator ./garage-operator \
  --values my-values.yaml \
  --namespace garage-operator-system \
  --create-namespace
```

## Configuration Options

### Default values.yaml

```yaml
replicaCount: 1

controllerManager:
  replicas: 1
  container:
    image:
      repository: controller
      tag: latest
      pullPolicy: IfNotPresent

webhook:
  enable: false

metricsService:
  enable: true
  type: ClusterIP
  ports:
    - name: https
      port: 8443
      protocol: TCP
      targetPort: 8443
```

### Available Configuration Parameters

| Parameter                                      | Description                  | Default            |
| ---------------------------------------------- | ---------------------------- | ------------------ |
| `controllerManager.replicas`                   | Number of operator replicas  | `1`                |
| `controllerManager.container.image.repository` | Operator image repository    | `controller`       |
| `controllerManager.container.image.tag`        | Operator image tag           | `latest`           |
| `controllerManager.container.image.pullPolicy` | Image pull policy            | `IfNotPresent`     |
| `controllerManager.container.resources`        | Resource limits and requests | Not set by default |
| `webhook.enable`                               | Enable admission webhooks    | `false`            |
| `metricsService.enable`                        | Enable metrics service       | `true`             |
| `metricsService.type`                          | Service type for metrics     | `ClusterIP`        |
| `metricsService.ports`                         | Metrics service ports        | See default values |

## Upgrading

To upgrade the operator to a new version:

```bash
# Upgrade CRDs first (if changed)
kubectl apply -f garage-operator/crds/

# Upgrade the Helm release
helm upgrade garage-operator ./garage-operator \
  --namespace garage-operator-system
```

## Uninstalling

To uninstall the operator:

```bash
# Uninstall the Helm release
helm uninstall garage-operator --namespace garage-operator-system

# Optionally remove CRDs (this will delete all GarageCluster and GarageBucket resources!)
kubectl delete crd garageclusters.garage.dimedis.io
kubectl delete crd garagebuckets.garage.dimedis.io
```

**Warning**: Deleting CRDs will also delete all custom resources (GarageCluster and GarageBucket instances).

## Verification

After installation, verify the operator is running:

```bash
# Check Helm release status
helm status garage-operator --namespace garage-operator-system

# Check operator pods
kubectl get pods -n garage-operator-system

# Verify CRDs are installed
kubectl get crds | grep garage

# Expected output:
# garagebuckets.garage.dimedis.io
# garageclusters.garage.dimedis.io
```

## Troubleshooting

### CRD Already Exists Error

If you see an error about CRDs already existing:

```
Error: Unable to continue with install: CustomResourceDefinition "garageclusters.garage.dimedis.io" 
already exists and cannot be imported into the current release
```

This means CRDs were already installed (possibly from a previous installation). You can:

1. Skip CRD installation and proceed with the chart:
   ```bash
   # Don't run kubectl apply -f crds/ again
   helm install garage-operator ./garage-operator \
     --namespace garage-operator-system \
     --create-namespace
   ```

2. Or remove existing CRDs first (this will delete all custom resources!):
   ```bash
   kubectl delete crd garageclusters.garage.dimedis.io
   kubectl delete crd garagebuckets.garage.dimedis.io
   ```

### Operator Pod Not Starting

Check the logs:

```bash
kubectl logs -n garage-operator-system deployment/garage-operator-controller-manager
```

Common issues:
- CRDs not installed: Install CRDs before the operator
- Image pull errors: Check image repository and credentials
- RBAC permissions: Verify ServiceAccount and Roles are created

## Building Charts Locally

For development or testing, you can build the chart locally:

```bash
# Generate the Helm chart from kustomize manifests
make generate-helm-chart

# This creates dist/chart/ with the chart structure

# Test the chart
kubectl apply -f dist/chart/crds/
helm install garage-operator dist/chart \
  --namespace garage-operator-system \
  --create-namespace \
  --dry-run --debug

# Package the chart
helm package dist/chart

# Creates: garage-operator-<version>.tgz
```

## Advanced Topics

### Using in GitOps Workflows

For GitOps tools like ArgoCD or Flux:

1. Create a Helm Release resource that references the chart
2. Ensure CRDs are applied before the Helm release (using sync waves or dependencies)
3. Consider using a separate CRD management strategy

Example ArgoCD Application:

```yaml
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: garage-operator-crds
spec:
  project: default
  source:
    repoURL: https://github.com/dimedis-gmbh/garage-operator
    targetRevision: v1.0.0
    path: config/crd/bases
  destination:
    server: https://kubernetes.default.svc
    namespace: default
  syncPolicy:
    syncOptions:
      - CreateNamespace=false
---
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: garage-operator
spec:
  project: default
  source:
    repoURL: https://github.com/dimedis-gmbh/garage-operator/releases/download/v1.0.0
    chart: garage-operator
    targetRevision: 1.0.0
  destination:
    server: https://kubernetes.default.svc
    namespace: garage-operator-system
  syncPolicy:
    syncOptions:
      - CreateNamespace=true
```

### Multi-Cluster Deployments

For managing multiple clusters:

1. Install CRDs once per cluster
2. Deploy the operator with cluster-specific configurations
3. Use different values.yaml files per environment

## Support

For issues or questions:
- GitHub Issues: https://github.com/dimedis-gmbh/garage-operator/issues
- Documentation: https://github.com/dimedis-gmbh/garage-operator/tree/main/docs
