# Garage Kubernetes Operator

A Kubernetes operator for managing Garage Object Storage clusters, built with Kubebuilder.

## Description

This operator automates the deployment and management of Garage Object Storage clusters on Kubernetes. Garage is a lightweight, geo-distributed object storage system that implements the Amazon S3 API.

Key features:

- Automated cluster deployment and configuration
- Pod anti-affinity for high availability
- Automatic layout management with zone assignment
- S3 API endpoint configuration
- Resource management and monitoring
- Ingress support with TLS

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

## Configuration Options

| Field             | Type   | Required | Default        | Description                            |
| ----------------- | ------ | -------- | -------------- | -------------------------------------- |
| `replicaCount`    | int32  | Yes      | -              | Number of Garage replicas (minimum 1)  |
| `replicationMode` | string | Yes      | -              | Replication mode: "1", "2", or "3"     |
| `volumeSize`      | string | No       | "20Gi"         | Size of persistent volumes per replica |
| `storageClass`    | string | No       | default        | Storage class for persistent volumes   |
| `rpcSecret`       | string | No       | auto-generated | RPC secret for cluster communication   |
| `s3Api`           | object | No       | -              | S3 API configuration                   |
| `ingress`         | object | No       | -              | Ingress configuration                  |
| `resources`       | object | No       | -              | Resource requests and limits           |

## Working with Garage

### Access the cluster

```bash
# Get S3 endpoint
kubectl get garagecluster my-garage -o jsonpath='{.status.s3Endpoint}'

# Access via port-forward
kubectl port-forward svc/my-garage 3900:3900
```

### Create buckets and keys

```bash
# Execute commands in garage pod
kubectl exec my-garage-0 -- ./garage status
kubectl exec my-garage-0 -- ./garage bucket create my-bucket
kubectl exec my-garage-0 -- ./garage key create my-key
kubectl exec my-garage-0 -- ./garage bucket allow --read --write my-bucket --key my-key
```

## Development

### Build and Test

```bash
make test                    # Run tests
make build                   # Build binary
make fmt                     # Format code
make vet                     # Run go vet
make manifests              # Generate CRDs and RBAC
make generate               # Generate code
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

## Project Distribution

Following the options to release and provide this solution to the users.

### By providing a bundle with all YAML files

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
kubectl apply -f https://raw.githubusercontent.com/<org>/garage-operator/<tag or branch>/dist/install.yaml
```

### By providing a Helm Chart

1. Build the chart using the optional helm plugin

```sh
kubebuilder edit --plugins=helm/v1-alpha
```

2. See that a chart was generated under 'dist/chart', and users
   can obtain this solution from there.

**NOTE:** If you change the project, you need to update the Helm Chart
using the same command above to sync the latest changes. Furthermore,
if you create webhooks, you need to use the above command with
the '--force' flag and manually ensure that any custom configuration
previously added to 'dist/chart/values.yaml' or 'dist/chart/manager/manager.yaml'
is manually re-applied afterwards.

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
