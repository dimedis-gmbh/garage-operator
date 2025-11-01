/*
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
*/

package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// GarageClusterSpec defines the desired state of GarageCluster
type GarageClusterSpec struct {
	// ReplicaCount is the number of Garage pod replicas in the StatefulSet
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Required
	ReplicaCount int32 `json:"replicaCount"`

	// ReplicationFactor is the number of copies of data that will be stored in the cluster
	// See https://garagehq.deuxfleurs.fr/documentation/reference-manual/configuration/#replication_factor
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:default=3
	// +optional
	ReplicationFactor int32 `json:"replicationFactor,omitempty"`

	// ConsistencyMode defines the consistency mode for Garage
	// - consistent: full consistency (n out of n nodes must respond)
	// - degraded: degraded consistency (n/2 + 1 out of n nodes must respond)
	// - dangerous: no consistency check (1 out of n nodes must respond)
	// See https://garagehq.deuxfleurs.fr/documentation/reference-manual/configuration/#consistency_mode
	// +kubebuilder:validation:Enum=consistent;degraded;dangerous
	// +kubebuilder:default=consistent
	// +optional
	ConsistencyMode string `json:"consistencyMode,omitempty"`

	// BlockSize is the size of Garage data blocks
	// See https://garagehq.deuxfleurs.fr/documentation/reference-manual/configuration/#block_size
	// +kubebuilder:default="1Mi"
	// +optional
	BlockSize string `json:"blockSize,omitempty"`

	// Persistence configuration for data and metadata volumes
	// +optional
	Persistence *PersistenceConfig `json:"persistence,omitempty"`

	// S3Api configuration
	// +optional
	S3Api *S3ApiConfig `json:"s3Api,omitempty"`

	// Ingress configuration
	// +optional
	Ingress *IngressConfig `json:"ingress,omitempty"`

	// Resources for Garage pods
	// +optional
	Resources *corev1.ResourceRequirements `json:"resources,omitempty"`
}

// PersistenceConfig defines persistent volume configuration
type PersistenceConfig struct {
	// Data volume configuration
	// +optional
	Data *VolumeConfig `json:"data,omitempty"`

	// Meta volume configuration
	// +optional
	Meta *VolumeConfig `json:"meta,omitempty"`
}

// VolumeConfig defines volume settings
type VolumeConfig struct {
	// Size of the volume
	// +kubebuilder:default="20Gi"
	// +optional
	Size string `json:"size,omitempty"`

	// StorageClass for the volume
	// +optional
	StorageClass string `json:"storageClass,omitempty"`
}

// S3ApiConfig defines S3 API settings
type S3ApiConfig struct {
	// Enabled determines if S3 API is enabled
	// +kubebuilder:default=true
	// +optional
	Enabled bool `json:"enabled,omitempty"`

	// Region name for S3 API
	// +kubebuilder:default="garage"
	// +optional
	Region string `json:"region,omitempty"`

	// RootDomain for S3 API
	// +kubebuilder:default=".s3.garage.localhost"
	// +optional
	RootDomain string `json:"rootDomain,omitempty"`
}

// IngressConfig defines ingress settings
type IngressConfig struct {
	// Enabled determines if ingress is created
	// +optional
	Enabled bool `json:"enabled,omitempty"`

	// ClassName is the ingress class
	// +optional
	ClassName string `json:"className,omitempty"`

	// Host for the ingress
	// +optional
	Host string `json:"host,omitempty"`

	// TLS enabled
	// +optional
	TLS bool `json:"tls,omitempty"`

	// Annotations for the ingress
	// +optional
	Annotations map[string]string `json:"annotations,omitempty"`
}

// GarageClusterStatus defines the observed state of GarageCluster
type GarageClusterStatus struct {
	// Phase represents the current phase of the cluster
	// +kubebuilder:validation:Enum=Pending;Deploying;Ready;Failed
	// +optional
	Phase string `json:"phase,omitempty"`

	// Conditions represent the latest available observations
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// ReadyReplicas is the number of ready replicas
	// +optional
	ReadyReplicas int32 `json:"readyReplicas,omitempty"`

	// S3Endpoint is the internal S3 API endpoint
	// +optional
	S3Endpoint string `json:"s3Endpoint,omitempty"`

	// ConnectedNodes is the number of nodes currently connected in the cluster
	// +optional
	ConnectedNodes int32 `json:"connectedNodes,omitempty"`

	// LayoutVersion is the current version of the cluster layout
	// +optional
	LayoutVersion int64 `json:"layoutVersion,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Replicas",type=integer,JSONPath=`.spec.replicaCount`
// +kubebuilder:printcolumn:name="RF",type=integer,JSONPath=`.spec.replicationFactor`
// +kubebuilder:printcolumn:name="Consistency",type=string,JSONPath=`.spec.consistencyMode`
// +kubebuilder:printcolumn:name="Ready",type=integer,JSONPath=`.status.readyReplicas`
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`
// +kubebuilder:validation:XValidation:rule="!has(self.spec.replicationFactor) || self.spec.replicationFactor <= self.spec.replicaCount",message="replicationFactor cannot be greater than replicaCount"

// GarageCluster is the Schema for the garageclusters API
type GarageCluster struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   GarageClusterSpec   `json:"spec,omitempty"`
	Status GarageClusterStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// GarageClusterList contains a list of GarageCluster
type GarageClusterList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []GarageCluster `json:"items"`
}

func init() {
	SchemeBuilder.Register(&GarageCluster{}, &GarageClusterList{})
}
