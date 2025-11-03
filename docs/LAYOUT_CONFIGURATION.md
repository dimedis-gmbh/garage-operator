# Layout Configuration Feature

## Overview

The GarageCluster CRD supports flexible zone assignment strategies for Garage nodes through the `layout` field in the spec. The operator automatically manages layout configuration both during initial cluster creation and when scaling up.

## Features

- **Automatic initial layout configuration**: When a cluster is first created, all nodes are assigned to zones and the layout is applied
- **Automatic scale-up support**: When you increase `replicaCount`, new nodes are automatically detected, connected to the cluster, assigned to zones, and added to the layout
- **Layout mode immutability**: The layout mode cannot be changed after initial configuration to prevent data inconsistency

## Configuration Options

### Layout Modes

Two modes are available:

1. **zonePerNode** (default)

   - Each replica gets its own sequential zone (z1, z2, z3, ...)
   - Simple and predictable zone assignment
   - No dependency on node labels

2. **zoneFromNodeLabel**
   - Zones are derived from Kubernetes node labels
   - Allows alignment with infrastructure topology
   - Useful for multi-datacenter or multi-availability-zone deployments

### Configuration Schema

```yaml
spec:
  layout:
    mode: zonePerNode | zoneFromNodeLabel
    zoneNodeLabel: topology.kubernetes.io/zone # only used with zoneFromNodeLabel
```

## Examples

### Example 1: Default Mode (zonePerNode)

```yaml
apiVersion: garage.dimedis.io/v1alpha1
kind: GarageCluster
metadata:
  name: my-garage
spec:
  replicaCount: 4
  replicationFactor: 2
  # layout configuration is optional, defaults to zonePerNode
```

This creates zones: z1, z2, z3, z4

### Example 2: Using Node Labels

```yaml
apiVersion: garage.dimedis.io/v1alpha1
kind: GarageCluster
metadata:
  name: my-garage
spec:
  replicaCount: 4
  replicationFactor: 2
  layout:
    mode: zoneFromNodeLabel
    zoneNodeLabel: "topology.kubernetes.io/zone"
```

This reads the `topology.kubernetes.io/zone` label from the Kubernetes nodes where the pods are scheduled.
For example, if your nodes have labels like:

- `topology.kubernetes.io/zone: us-east-1a`
- `topology.kubernetes.io/zone: us-east-1b`
- `topology.kubernetes.io/zone: us-east-1c`

The Garage cluster will use these actual zone identifiers instead of z1, z2, z3.

### Example 3: Custom Zone Label

```yaml
apiVersion: garage.dimedis.io/v1alpha1
kind: GarageCluster
metadata:
  name: my-garage
spec:
  replicaCount: 3
  replicationFactor: 2
  layout:
    mode: zoneFromNodeLabel
    zoneNodeLabel: "my.custom.label/zone"
```

This allows you to use any custom node label for zone identification.

## Scaling Support

### Scaling Up

When you increase the `replicaCount` in your GarageCluster spec, the operator automatically:

1. Waits for new pods to become ready
2. Detects which nodes are new (not yet in the layout)
3. Connects new nodes to the existing cluster
4. Assigns zones to new nodes based on the configured layout mode
5. Applies the updated layout with an incremented version number

**Example:**

```yaml
# Initial cluster with 3 replicas
apiVersion: garage.dimedis.io/v1alpha1
kind: GarageCluster
metadata:
  name: my-garage
spec:
  replicaCount: 3
  replicationFactor: 2
  layout:
    mode: zoneFromNodeLabel
    zoneNodeLabel: "topology.kubernetes.io/zone"
```

To scale up, simply update the spec:

```bash
kubectl patch garagecluster my-garage -p '{"spec":{"replicaCount":4}}' --type=merge
```

The operator will automatically add the 4th node to the layout.

### Scaling Down

**Note:** Scaling down (reducing `replicaCount`) is not currently automated. You would need to manually:

1. Remove nodes from the layout using `garage layout remove`
2. Apply the layout
3. Then reduce the `replicaCount`

This is intentional to prevent accidental data loss.

## Important Constraints

### Layout Mode is Immutable

Once a GarageCluster is created and the layout is configured, **the layout mode cannot be changed**.

If you attempt to change the mode on an existing cluster, the operator will:

1. Detect the change
2. Set the cluster status to `Failed`
3. Log an error message indicating the applied mode and the requested mode
4. Refuse to proceed with reconciliation

**Example error:**

```
Layout mode cannot be changed after initial configuration. Applied mode: zonePerNode, requested mode: zoneFromNodeLabel
```

### Why is it Immutable?

Changing the layout mode after initial configuration would require:

- Reassignment of all nodes to new zones
- Rebalancing of data across the cluster
- Potential data migration

These operations are complex and could lead to data inconsistency or cluster instability. Therefore, the layout mode must be chosen carefully during initial cluster creation.

### Workaround

If you need to change the layout mode:

1. Create a new GarageCluster with the desired layout mode
2. Migrate your data to the new cluster
3. Delete the old cluster

## Status Tracking

The operator tracks the applied layout mode in the cluster status:

```yaml
status:
  layoutVersion: 1
  appliedLayoutMode: zoneFromNodeLabel
```

This field is set automatically when the layout is first configured and is used to validate that the mode hasn't changed in subsequent reconciliations.

## Requirements for zoneFromNodeLabel Mode

When using `zoneFromNodeLabel` mode:

1. **All nodes must have the specified label**

   - If a node doesn't have the label, the layout configuration will fail
   - Error message: `node <name> does not have label <label>`

2. **Pods must be scheduled before layout configuration**

   - The operator needs to determine which node each pod is running on
   - This happens automatically during the normal reconciliation flow

3. **Label must contain a non-empty value**
   - Empty label values are not accepted

## Implementation Details

### Modified Files

1. `api/v1alpha1/garagecluster_types.go`

   - Added `LayoutConfig` struct
   - Added `layout` field to `GarageClusterSpec`
   - Added `appliedLayoutMode` field to `GarageClusterStatus`

2. `internal/controller/layout_manager.go`

   - Added `getZoneAssignments()` method to support both modes
   - Modified `ConfigureLayout()` to use the new zone assignment logic

3. `internal/controller/garagecluster_controller.go`
   - Added validation to prevent layout mode changes
   - Added status tracking for applied layout mode

### CRD Updates

The CRD manifests in `config/crd/bases/` have been regenerated to include:

- Layout configuration schema
- Enum validation for mode field
- Default values

### Sample Configurations

Two sample files are provided:

- `config/samples/garage_v1alpha1_garagecluster.yaml` - includes commented layout example
- `config/samples/garage_v1alpha1_garagecluster_zone_label.yaml` - demonstrates zoneFromNodeLabel mode
