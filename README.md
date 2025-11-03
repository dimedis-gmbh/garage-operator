# Garage Kubernetes Operator

A Kubernetes operator for managing Garage Object Storage clusters, built with Kubebuilder and Claude AI using VS Code Agent.

## Description

This operator automates the deployment and management of Garage Object Storage clusters on Kubernetes. Garage is a lightweight, geo-distributed object storage system that implements the Amazon S3 API.

Key features:

- Automated cluster deployment and configuration
- Automatic layout management with zone assignment
- Pod anti-affinity for high availability
- S3 API endpoint configuration
- Resource management and monitoring
- Ingress support with TLS
- Declarative bucket provisioning with GarageBucket CRD
- Automated access key management with Kubernetes Secrets
- Bucket quotas and website hosting support

## Getting Started

### Prerequisites

- Go >= 1.21
- Docker
- kubectl
- Access to a Kubernetes cluster
- [kubebuilder](https://book.kubebuilder.io/quick-start.html#installation) >= 3.0

### Quick Start

#### 1. Install CRDs

```bash
make install
```

#### 2. Run operator locally

```bash
# Run against currently configured kubectl context
make run
```

#### 3. Deploy a GarageCluster

```bash
kubectl apply -f config/samples/garage_v1alpha1_garagecluster.yaml
```

#### 4. Check status

```bash
kubectl get garagecluster
kubectl describe garagecluster garagecluster-sample
```

## Local development with Kind cluster

### Create cluster

```bash
cd kind/infra
./create.sh
```

### Deploy platform services

```bash
cd kind/k8s
terraform init
terraform apply
```

### Build and deploy into Kind cluster

```bash
# Build Docker image, load into Kind and deploy operator
make docker-build && make docker-kind && make deploy

# Since we're using a :dev tag restart operator deployment
kubectl rollout restart -n garage-operator-system \
        deployments/garage-operator-controller-manager
```

## GarageCluster Configuration

### Basic Example

```yaml
apiVersion: garage.dimedis.io/v1alpha1
kind: GarageCluster
metadata:
  name: my-garage
spec:
  replicaCount: 5
  replicationFactor: 2
  consistencyMode: "consistent"
  image:
    tag: "v2.1.0"
  persistence:
    data:
      size: "10Gi"
    meta:
      size: "1Gi"
```

### Production Example

```yaml
apiVersion: garage.dimedis.io/v1alpha1
kind: GarageCluster
metadata:
  name: production-garage
spec:
  replicaCount: 10
  replicationFactor: 3
  consistencyMode: "consistent"
  blockSize: 10Mi

  image:
    repository: "dxflrs/garage"
    tag: "v2.1.0"
    pullPolicy: IfNotPresent

  persistence:
    data:
      size: "2000Gi"
      storageClass: "hdd-xfs"
    meta:
      size: "100Gi"
      storageClass: "ssd-zfs"

  resources:
    requests:
      cpu: "500m"
      memory: "1Gi"
    limits:
      cpu: "2000m"
      memory: "4Gi"

  s3Api:
    enabled: true
    region: "garage"
    rootDomain: ".s3.garage.example.com"
```

### Multi-Zone with Node Labels

Use Kubernetes node topology labels for zone assignment:

```yaml
apiVersion: garage.dimedis.io/v1alpha1
kind: GarageCluster
metadata:
  name: multi-zone-garage
spec:
  replicaCount: 6
  replicationFactor: 3
  consistencyMode: "consistent"

  image:
    tag: "v2.1.0"

  # Use actual datacenter zones from node labels
  layout:
    mode: zoneFromNodeLabel
    zoneNodeLabel: "topology.kubernetes.io/zone"

  persistence:
    data:
      size: "1000Gi"
      storageClass: "fast-ssd"
    meta:
      size: "50Gi"
      storageClass: "fast-ssd"

  resources:
    requests:
      cpu: "1000m"
      memory: "2Gi"
    limits:
      cpu: "4000m"
      memory: "8Gi"
```

This configuration will read the zone from each node's `topology.kubernetes.io/zone` label (e.g., `us-east-1a`, `us-east-1b`, `us-east-1c`) and assign Garage nodes accordingly.

## Configuration Options

### GarageCluster Configuration

| Field               | Type                 | Required | Default      | Description                                                |
| ------------------- | -------------------- | -------- | ------------ | ---------------------------------------------------------- |
| `replicaCount`      | int32                | Yes      | -            | Number of Garage pod replicas (minimum 1)                  |
| `replicationFactor` | int32                | No       | 3            | Number of data copies stored in cluster                    |
| `consistencyMode`   | string               | No       | "consistent" | Consistency mode: "consistent", "degraded", or "dangerous" |
| `blockSize`         | string               | No       | "1Mi"        | Size of Garage data blocks                                 |
| `image`             | ImageConfig          | No       | -            | Container image configuration                              |
| `persistence`       | PersistenceConfig    | No       | -            | Persistent volume configuration for data and meta          |
| `s3Api`             | S3ApiConfig          | No       | -            | S3 API configuration                                       |
| `ingress`           | IngressConfig        | No       | -            | Ingress configuration                                      |
| `resources`         | ResourceRequirements | No       | -            | Resource requests and limits                               |
| `layout`            | LayoutConfig         | No       | -            | Zone assignment strategy for cluster layout                |

#### ImageConfig

| Field        | Type       | Required | Default         | Description                                    |
| ------------ | ---------- | -------- | --------------- | ---------------------------------------------- |
| `repository` | string     | No       | "dxflrs/garage" | Container image repository                     |
| `tag`        | string     | Yes      | -               | Container image tag (e.g., "v2.1.0")           |
| `pullPolicy` | PullPolicy | No       | "IfNotPresent"  | Image pull policy: Always, Never, IfNotPresent |

#### LayoutConfig

| Field           | Type   | Required | Default                       | Description                                                              |
| --------------- | ------ | -------- | ----------------------------- | ------------------------------------------------------------------------ |
| `mode`          | string | No       | "zonePerNode"                 | Zone assignment mode: "zonePerNode" or "zoneFromNodeLabel"               |
| `zoneNodeLabel` | string | No       | "topology.kubernetes.io/zone" | Node label to use for zone assignment (only used with zoneFromNodeLabel) |

**Layout Modes:**

- **zonePerNode** (default): Each replica gets its own sequential zone (z1, z2, z3, ...). Simple and predictable.
- **zoneFromNodeLabel**: Zones are derived from Kubernetes node labels. Allows alignment with infrastructure topology (e.g., datacenter zones).

**Important:** The layout mode cannot be changed after initial cluster creation to prevent data inconsistency.

#### PersistenceConfig

| Field  | Type         | Required | Default | Description                   |
| ------ | ------------ | -------- | ------- | ----------------------------- |
| `data` | VolumeConfig | No       | -       | Data volume configuration     |
| `meta` | VolumeConfig | No       | -       | Metadata volume configuration |

#### VolumeConfig

| Field          | Type   | Required | Default | Description                  |
| -------------- | ------ | -------- | ------- | ---------------------------- |
| `size`         | string | No       | "20Gi"  | Size of the volume           |
| `storageClass` | string | No       | default | Storage class for the volume |

### GarageBucket Configuration

| Field           | Type             | Required | Default       | Description                                    |
| --------------- | ---------------- | -------- | ------------- | ---------------------------------------------- |
| `clusterRef`    | ClusterReference | Yes      | -             | Reference to GarageCluster                     |
| `bucketName`    | string           | No       | metadata.name | Name of the bucket (DNS-compliant, 3-63 chars) |
| `publicRead`    | bool             | No       | false         | Make bucket publicly readable                  |
| `quotas`        | BucketQuotas     | No       | -             | Size and object count limits                   |
| `websiteConfig` | WebsiteConfig    | No       | -             | Static website hosting configuration           |
| `keys`          | []BucketKey      | No       | -             | Access keys with permissions                   |

#### ClusterReference

| Field       | Type   | Required | Default            | Description                    |
| ----------- | ------ | -------- | ------------------ | ------------------------------ |
| `name`      | string | Yes      | -                  | Name of the GarageCluster      |
| `namespace` | string | No       | bucket's namespace | Namespace of the GarageCluster |

#### BucketQuotas

| Field        | Type   | Required | Default | Description                        |
| ------------ | ------ | -------- | ------- | ---------------------------------- |
| `maxSize`    | string | No       | -       | Maximum total size (e.g., "100Gi") |
| `maxObjects` | int64  | No       | -       | Maximum number of objects          |

#### WebsiteConfig

| Field           | Type   | Required | Default      | Description            |
| --------------- | ------ | -------- | ------------ | ---------------------- |
| `enabled`       | bool   | No       | false        | Enable website hosting |
| `indexDocument` | string | No       | "index.html" | Index document name    |
| `errorDocument` | string | No       | -            | Error document name    |

#### BucketKey Configuration

| Field            | Type            | Required | Default | Description                           |
| ---------------- | --------------- | -------- | ------- | ------------------------------------- |
| `name`           | string          | No       | auto    | Name of the key                       |
| `permissions`    | KeyPermissions  | Yes      | -       | Read, write, and owner permissions    |
| `expirationDate` | Time            | No       | -       | When the key expires (RFC3339 format) |
| `secretRef`      | SecretReference | No       | auto    | Where to store credentials            |

#### KeyPermissions

| Field   | Type | Required | Default | Description                                    |
| ------- | ---- | -------- | ------- | ---------------------------------------------- |
| `read`  | bool | No       | false   | Grant read access to the bucket                |
| `write` | bool | No       | false   | Grant write access to the bucket               |
| `owner` | bool | No       | false   | Grant ownership (deletion, permission changes) |

#### SecretReference

| Field       | Type   | Required | Default            | Description             |
| ----------- | ------ | -------- | ------------------ | ----------------------- |
| `name`      | string | Yes      | -                  | Name of the Secret      |
| `namespace` | string | No       | bucket's namespace | Namespace of the Secret |

## Working with Garage

### Access the cluster

```bash
# Get S3 endpoint
kubectl get garagecluster my-garage -o jsonpath='{.status.s3Endpoint}'

# Access via port-forward
kubectl port-forward svc/my-garage 3900:3900
```

### Provision buckets with GarageBucket

Create a bucket with access keys declaratively:

```bash
kubectl apply -f - <<EOF
apiVersion: garage.dimedis.io/v1alpha1
kind: GarageBucket
metadata:
  name: my-app-storage
spec:
  clusterRef:
    name: my-garage
  publicRead: false
  quotas:
    maxSize: "100Gi"
    maxObjects: 1000000
  keys:
    - name: app-key
      permissions:
        read: true
        write: true
        owner: false
    - name: admin-key
      permissions:
        read: true
        write: true
        owner: true
EOF
```

#### Understanding the generated Secrets

For each key defined in the `keys` array, the operator automatically creates a Kubernetes Secret containing the S3 credentials. The secret is created with the naming pattern `<bucket-name>-<key-name>` (unless a custom `secretRef` is specified).

**Secret structure:**

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: my-app-storage-app-key
  namespace: default
  ownerReferences:
    - apiVersion: garage.dimedis.io/v1alpha1
      kind: GarageBucket
      name: my-app-storage
      # ... (ensures secret is deleted when bucket is deleted)
type: Opaque
data:
  accessKeyId: R0s... # Base64 encoded Garage access key ID
  secretAccessKey: Y2F... # Base64 encoded Garage secret access key
  bucket: bXktYXBwLXN0b3JhZ2U= # Base64 encoded bucket name
  serviceEndpoint: aHR0cDovL215LWdhcmFnZTo5MDAw # Base64 encoded S3 service endpoint (cluster-internal)
  publicEndpoint: aHR0cHM6Ly9nYXJhZ2UuZXhhbXBsZS5jb20= # Base64 encoded public S3 endpoint (optional, if ingress configured)
```

**Secret fields:**

| Field             | Description                                                        | Example Value                         | Always Present          |
| ----------------- | ------------------------------------------------------------------ | ------------------------------------- | ----------------------- |
| `accessKeyId`     | Garage S3 access key ID                                            | `GK5a8f9b2c1d3e4f5a6b7c8d9e0f1a2b`    | Yes                     |
| `secretAccessKey` | Garage S3 secret access key                                        | `7c8d9e0f1a2b3c4d5e6f7a8b9c0d1e2f...` | Yes                     |
| `bucket`          | Name of the bucket                                                 | `my-app-storage`                      | Yes                     |
| `serviceEndpoint` | S3 endpoint URL for cluster-internal access                        | `http://my-garage.default.svc:3900`   | Yes                     |
| `publicEndpoint`  | Public S3 endpoint URL (via ingress, with http/https based on TLS) | `https://garage.example.com`          | Only if ingress enabled |

#### Using the Secret in applications

Mount the secret in your application pod:

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: my-app
spec:
  containers:
    - name: app
      image: my-app:latest
      env:
        - name: AWS_ACCESS_KEY_ID
          valueFrom:
            secretKeyRef:
              name: my-app-storage-app-key
              key: accessKeyId
        - name: AWS_SECRET_ACCESS_KEY
          valueFrom:
            secretKeyRef:
              name: my-app-storage-app-key
              key: secretAccessKey
        - name: S3_BUCKET
          valueFrom:
            secretKeyRef:
              name: my-app-storage-app-key
              key: bucket
        - name: S3_SERVICE_ENDPOINT
          valueFrom:
            secretKeyRef:
              name: my-app-storage-app-key
              key: serviceEndpoint
        # Optional: Use publicEndpoint for external access
        - name: S3_PUBLIC_ENDPOINT
          valueFrom:
            secretKeyRef:
              name: my-app-storage-app-key
              key: publicEndpoint
              optional: true
```

Or mount as files:

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: my-app
spec:
  containers:
    - name: app
      image: my-app:latest
      volumeMounts:
        - name: s3-credentials
          mountPath: /etc/s3
          readOnly: true
  volumes:
    - name: s3-credentials
      secret:
        secretName: my-app-storage-app-key
```

#### Access the credentials via CLI

```bash
# Get access key ID and secret
kubectl get secret my-app-storage-app-key -o jsonpath='{.data.accessKeyId}' | base64 -d
kubectl get secret my-app-storage-app-key -o jsonpath='{.data.secretAccessKey}' | base64 -d

# Get all credentials at once
kubectl get secret my-app-storage-app-key -o json | jq -r '.data | map_values(@base64d)'

# Export for use with AWS CLI
export AWS_ACCESS_KEY_ID=$(kubectl get secret my-app-storage-app-key -o jsonpath='{.data.accessKeyId}' | base64 -d)
export AWS_SECRET_ACCESS_KEY=$(kubectl get secret my-app-storage-app-key -o jsonpath='{.data.secretAccessKey}' | base64 -d)
export AWS_ENDPOINT_URL=$(kubectl get secret my-app-storage-app-key -o jsonpath='{.data.endpoint}' | base64 -d)

# Test access
aws s3 ls --endpoint-url=$AWS_ENDPOINT_URL
```

#### Key expiration and rotation

When a key's `expirationDate` is changed in the GarageBucket spec, the operator will automatically:

1. Delete the old key in Garage
2. Create a new key with the updated expiration date
3. Update the Secret with the new credentials

**Important:** This means the `accessKeyId` and `secretAccessKey` values will change! Applications using these credentials must be restarted to pick up the new values.

**Example of updating expiration:**

```bash
# Update the expiration date
kubectl patch garagebucket my-app-storage --type='json' -p='[
  {
    "op": "replace",
    "path": "/spec/keys/0/expirationDate",
    "value": "2027-12-31T23:59:59Z"
  }
]'

# Watch the operator recreate the key
kubectl logs -n garage-operator-system -l control-plane=controller-manager -f

# Applications need to be restarted to get new credentials
kubectl rollout restart deployment/my-app
```

**Best practices for key rotation:**

- Use Kubernetes Deployment restarts or similar mechanisms to ensure applications pick up new credentials
- Consider using init containers or sidecar patterns that regularly reload credentials
- Monitor key expiration dates in the GarageBucket status:
  ```bash
  kubectl get garagebucket my-app-storage -o jsonpath='{.status.keys[*].expirationDate}'
  ```

### Create buckets and keys manually

```bash
# Execute commands in garage pod
kubectl exec my-garage-0 -- ./garage status
kubectl exec my-garage-0 -- ./garage bucket create my-bucket
kubectl exec my-garage-0 -- ./garage key create my-key
kubectl exec my-garage-0 -- ./garage bucket allow --read --write my-bucket --key my-key
```

## Cluster Operations

### Scaling a GarageCluster

#### Scaling Up

The operator automatically handles scaling up. Simply increase the `replicaCount`:

```bash
kubectl patch garagecluster my-garage -p '{"spec":{"replicaCount":6}}' --type=merge
```

The operator will:

1. Wait for new pods to become ready
2. Detect new nodes not yet in the cluster layout
3. Connect new nodes to the existing cluster
4. Assign zones based on the configured layout mode
5. Apply the updated layout with an incremented version

**Monitor the scaling process:**

```bash
# Watch cluster status
kubectl get garagecluster my-garage -w

# Check layout version (increases after scaling)
kubectl get garagecluster my-garage -o jsonpath='{.status.layoutVersion}'

# View operator logs
kubectl logs -n garage-operator-system -l control-plane=controller-manager -f
```

#### Scaling Down

**Important:** Scaling down is not automated to prevent accidental data loss.

To scale down safely:

1. Access a Garage pod:

   ```bash
   kubectl exec -it my-garage-0 -- /bin/sh
   ```

2. Remove nodes from the layout:

   ```bash
   # View current layout
   ./garage layout show

   # Remove a node (replace with actual node ID)
   ./garage layout remove <node-id>

   # Apply the layout
   ./garage layout apply --version <new-version>
   ```

3. Wait for data rebalancing to complete:

   ```bash
   ./garage status
   ```

4. Then reduce the `replicaCount`:
   ```bash
   kubectl patch garagecluster my-garage -p '{"spec":{"replicaCount":4}}' --type=merge
   ```

**Note:** The layout mode (zonePerNode vs zoneFromNodeLabel) is set during initial cluster creation and cannot be changed later.

## GarageBucket Examples

### Simple Application Bucket

```yaml
apiVersion: garage.dimedis.io/v1alpha1
kind: GarageBucket
metadata:
  name: myapp-data
spec:
  clusterRef:
    name: production-garage
  keys:
    - name: app
      permissions:
        read: true
        write: true
```

### Public Static Website

```yaml
apiVersion: garage.dimedis.io/v1alpha1
kind: GarageBucket
metadata:
  name: company-website
spec:
  clusterRef:
    name: production-garage
  publicRead: true
  websiteConfig:
    enabled: true
    indexDocument: "index.html"
    errorDocument: "404.html"
  keys:
    - name: deployer
      permissions:
        read: true
        write: true
        owner: true
```

### Multi-Tenant with Quotas

```yaml
apiVersion: garage.dimedis.io/v1alpha1
kind: GarageBucket
metadata:
  name: tenant-acme
spec:
  clusterRef:
    name: shared-garage
  quotas:
    maxSize: "500Gi"
    maxObjects: 5000000
  keys:
    - name: admin
      permissions:
        read: true
        write: true
        owner: true
    - name: readonly
      permissions:
        read: true
      expirationDate: "2025-12-31T23:59:59Z"
```

## Development

### Build and Test

```bash
make test       # Run tests
make build      # Build binary
make fmt        # Format code
make vet        # Run go vet
make manifests  # Generate CRDs and RBAC
make generate   # Generate code
```

### Deploy to cluster

**Build your image:**

```bash
make docker-build IMG=$DOCKER_REPO_HOST/garage-operator:latest
```

**Install the CRDs:**

```bash
make install
```

**Deploy the operator:**

```bash
make deploy IMG=$DOCKER_REPO_HOST/garage-operator:latest
```

**Create GarageCluster instances:**

```bash
kubectl apply -f config/samples/garage_v1alpha1_garagecluster.yaml
```

### Cleanup

```bash
make undeploy               # Remove operator from cluster
make uninstall              # Remove CRDs
```

## Troubleshooting

### Pods stuck in pending

Check if you have enough nodes for pod anti-affinity:

```bash
kubectl get nodes
kubectl describe pod my-garage-0
```

### Layout not configured

Check operator logs and manually configure if needed:

```bash
kubectl logs -n garage-operator-system deployment/garage-operator-controller-manager
kubectl exec my-garage-0 -- ./garage status
```

### GarageBucket not ready

Check the bucket status and controller logs:

```bash
kubectl describe garagebucket my-app-storage
kubectl logs -n garage-operator-system deployment/garage-operator-controller-manager | grep GarageBucket
```

Check if referenced cluster is ready:

```bash
kubectl get garagecluster my-garage
kubectl describe garagecluster my-garage
```

Verify secrets were created:

```bash
kubectl get secrets | grep my-app-storage
kubectl describe secret my-app-storage-app-key
```

### Bucket access denied

Check key permissions in bucket status:

```bash
kubectl get garagebucket my-app-storage -o jsonpath='{.status.keys}' | jq
```

Verify credentials in secret:

```bash
kubectl get secret my-app-storage-app-key -o jsonpath='{.data.accessKeyId}' | base64 -d
```

Test access with AWS CLI:

```bash
export AWS_ACCESS_KEY_ID=$(kubectl get secret my-app-storage-app-key -o jsonpath='{.data.accessKeyId}' | base64 -d)
export AWS_SECRET_ACCESS_KEY=$(kubectl get secret my-app-storage-app-key -o jsonpath='{.data.secretAccessKey}' | base64 -d)
export AWS_ENDPOINT_URL=$(kubectl get garagecluster my-garage -o jsonpath='{.status.s3Endpoint}')

aws s3 ls --endpoint-url=$AWS_ENDPOINT_URL
```

### To Uninstall

**Delete the instances (CRs) from the cluster:**

```sh
kubectl delete -k config/samples/
```

**Delete the APIs(CRDs) from the cluster:**

```sh
make uninstall
```

**UnDeploy the controller from the cluster:**

```sh
make undeploy
```

## Documentation

- [Layout Configuration Guide](docs/LAYOUT_CONFIGURATION.md) - Detailed guide on zone assignment strategies and scaling
- [Release Process](docs/RELEASE_PROCESS.md) - How to create and publish releases

## Project Distribution

The operator is distributed through GitHub Releases with multiple installation methods.

### Quick Install

**Using Kustomize (latest version):**

```sh
kubectl apply -f https://github.com/dimedis-gmbh/garage-operator/releases/latest/download/install.yaml
```

**Using Helm (latest version):**

```sh
# Direct installation with chart URL
helm install garage-operator \
  https://github.com/dimedis-gmbh/garage-operator/releases/latest/download/garage-operator-<version>.tgz \
  --namespace garage-operator-system \
  --create-namespace
```

See [Release Process](docs/RELEASE_PROCESS.md) for detailed installation options and version-specific URLs.

### Installation Methods

Following the options to release and provide this solution to the users.

#### By providing a bundle with all YAML files

1. Build the installer for the image built and published in the registry:

```sh
make build-installer IMG=<some-registry>/garage-operator:tag
```

**NOTE:** The makefile target mentioned above generates an 'install.yaml'
file in the dist directory. This file contains all the resources built
with Kustomize, which are necessary to install this project without its
dependencies.

2. Using the installer

Users can just run 'kubectl apply -f <URL for YAML BUNDLE>' to install
the project, i.e.:

```sh
kubectl apply -f https://github.com/dimedis-gmbh/garage-operator/releases/download/v1.0.0/install.yaml
```

#### By providing a Helm Chart

The Helm chart is automatically generated and packaged with each release.

**Install from release:**

```sh
helm install garage-operator \
  https://github.com/dimedis-gmbh/garage-operator/releases/download/v1.0.0/garage-operator-1.0.0.tgz
```

**Or use as Helm repository:**

```sh
helm repo add garage-operator https://github.com/dimedis-gmbh/garage-operator/releases/download/v1.0.0
helm repo update
helm install garage-operator garage-operator/garage-operator --version 1.0.0
```

**With custom configuration:**

```sh
# Create custom values
cat > my-values.yaml <<EOF
controllerManager:
  replicas: 2
  container:
    resources:
      limits:
        cpu: 1000m
        memory: 512Mi
EOF

# Install with custom values
helm install garage-operator \
  https://github.com/dimedis-gmbh/garage-operator/releases/download/v1.0.0/garage-operator-1.0.0.tgz \
  --values my-values.yaml \
  --namespace garage-operator-system \
  --create-namespace
```

**NOTE:** The chart is automatically updated with each release. Check the
`dist/chart/values.yaml` file for all available configuration options.

### Building Charts Locally

1. Update chart version and dependencies:

```sh
# Update Chart.yaml with new version
vim dist/chart/Chart.yaml

# Package the chart
make build-chart
```

2. Test the chart before releasing:

```sh
# Dry-run installation
helm install garage-operator dist/chart --dry-run --debug

# Lint the chart
helm lint dist/chart
```

## Contributing

// TODO(user): Add detailed information on how you would like others to contribute to this project

**NOTE:** Run `make help` for more information on all potential `make` targets

More information can be found via the [Kubebuilder Documentation](https://book.kubebuilder.io/introduction.html)

## License

Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
