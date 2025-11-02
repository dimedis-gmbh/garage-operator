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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// GarageBucketSpec defines the desired state of GarageBucket
type GarageBucketSpec struct {
	// ClusterRef references the GarageCluster where this bucket should be created
	// +kubebuilder:validation:Required
	ClusterRef ClusterReference `json:"clusterRef"`

	// BucketName is the name of the bucket to create
	// If not specified, the name of the GarageBucket resource will be used
	// +kubebuilder:validation:Pattern=^[a-z0-9][a-z0-9.-]*[a-z0-9]$
	// +kubebuilder:validation:MinLength=3
	// +kubebuilder:validation:MaxLength=63
	// +optional
	BucketName string `json:"bucketName,omitempty"`

	// PublicRead determines if the bucket should be publicly readable
	// +kubebuilder:default=false
	// +optional
	PublicRead bool `json:"publicRead,omitempty"`

	// Quotas for the bucket
	// +optional
	Quotas *BucketQuotas `json:"quotas,omitempty"`

	// WebsiteConfig enables static website hosting for this bucket
	// +optional
	WebsiteConfig *WebsiteConfig `json:"websiteConfig,omitempty"`

	// Keys defines access keys for this bucket
	// +optional
	Keys []BucketKey `json:"keys,omitempty"`
}

// ClusterReference references a GarageCluster
type ClusterReference struct {
	// Name of the GarageCluster
	// +kubebuilder:validation:Required
	Name string `json:"name"`

	// Namespace of the GarageCluster
	// If not specified, the same namespace as the GarageBucket will be used
	// +optional
	Namespace string `json:"namespace,omitempty"`
}

// BucketQuotas defines size and object quotas for a bucket
type BucketQuotas struct {
	// MaxSize is the maximum total size of objects in the bucket
	// Examples: "100Gi", "1Ti"
	// +optional
	MaxSize string `json:"maxSize,omitempty"`

	// MaxObjects is the maximum number of objects in the bucket
	// +optional
	MaxObjects *int64 `json:"maxObjects,omitempty"`
}

// WebsiteConfig defines static website hosting configuration
type WebsiteConfig struct {
	// Enabled determines if website hosting is enabled
	// +kubebuilder:default=false
	// +optional
	Enabled bool `json:"enabled,omitempty"`

	// IndexDocument is the index document for the website
	// +kubebuilder:default="index.html"
	// +optional
	IndexDocument string `json:"indexDocument,omitempty"`

	// ErrorDocument is the error document for the website
	// +optional
	ErrorDocument string `json:"errorDocument,omitempty"`
}

// BucketKey defines an access key for a bucket
type BucketKey struct {
	// Name is the name of the key
	// If not specified, a name will be generated
	// +optional
	Name string `json:"name,omitempty"`

	// SecretRef is the reference to the Secret where the key credentials will be stored
	// If not specified, a Secret will be created with the name <bucket-name>-<key-name>
	// +optional
	SecretRef *SecretReference `json:"secretRef,omitempty"`

	// ExpirationDate is the date when the key expires (RFC3339 format)
	// +optional
	ExpirationDate *metav1.Time `json:"expirationDate,omitempty"`

	// Permissions defines the access permissions for this key
	// +kubebuilder:validation:Required
	Permissions KeyPermissions `json:"permissions"`
}

// SecretReference references a Kubernetes Secret
type SecretReference struct {
	// Name of the Secret
	// +kubebuilder:validation:Required
	Name string `json:"name"`

	// Namespace of the Secret
	// If not specified, the same namespace as the GarageBucket will be used
	// +optional
	Namespace string `json:"namespace,omitempty"`
}

// KeyPermissions defines access permissions for a key
type KeyPermissions struct {
	// Read grants read access to the bucket
	// +kubebuilder:default=false
	// +optional
	Read bool `json:"read,omitempty"`

	// Write grants write access to the bucket
	// +kubebuilder:default=false
	// +optional
	Write bool `json:"write,omitempty"`

	// Owner grants ownership of the bucket (allows bucket deletion and permission changes)
	// +kubebuilder:default=false
	// +optional
	Owner bool `json:"owner,omitempty"`
}

// GarageBucketStatus defines the observed state of GarageBucket
type GarageBucketStatus struct {
	// Phase represents the current phase of the bucket
	// +kubebuilder:validation:Enum=Pending;Creating;Ready;Failed
	// +optional
	Phase string `json:"phase,omitempty"`

	// Conditions represent the latest available observations
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// BucketID is the internal Garage bucket ID
	// +optional
	BucketID string `json:"bucketID,omitempty"`

	// ActualBucketName is the actual name of the bucket in Garage
	// +optional
	ActualBucketName string `json:"actualBucketName,omitempty"`

	// S3Endpoint is the S3 endpoint URL for this bucket
	// +optional
	S3Endpoint string `json:"s3Endpoint,omitempty"`

	// WebsiteEndpoint is the website endpoint URL if website hosting is enabled
	// +optional
	WebsiteEndpoint string `json:"websiteEndpoint,omitempty"`

	// Keys contains the status of created keys
	// +optional
	Keys []KeyStatus `json:"keys,omitempty"`

	// CurrentSize is the current total size of objects in the bucket
	// +optional
	CurrentSize string `json:"currentSize,omitempty"`

	// CurrentObjects is the current number of objects in the bucket
	// +optional
	CurrentObjects int64 `json:"currentObjects,omitempty"`
}

// KeyStatus represents the status of a bucket key
type KeyStatus struct {
	// Name of the key
	Name string `json:"name"`

	// KeyID is the Garage access key ID
	KeyID string `json:"keyID"`

	// SecretName is the name of the Secret containing the key credentials
	SecretName string `json:"secretName"`

	// SecretNamespace is the namespace of the Secret
	SecretNamespace string `json:"secretNamespace"`

	// ExpirationDate is the expiration date of the key
	// +optional
	ExpirationDate *metav1.Time `json:"expirationDate,omitempty"`

	// Expired indicates if the key has expired
	Expired bool `json:"expired"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Bucket",type=string,JSONPath=`.status.actualBucketName`
// +kubebuilder:printcolumn:name="Cluster",type=string,JSONPath=`.spec.clusterRef.name`
// +kubebuilder:printcolumn:name="Public",type=boolean,JSONPath=`.spec.publicRead`
// +kubebuilder:printcolumn:name="Keys",type=integer,JSONPath=`.status.keys[*].name`
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`
// +kubebuilder:resource:shortName=gbucket;gbkt

// GarageBucket is the Schema for the garagebuckets API
type GarageBucket struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   GarageBucketSpec   `json:"spec,omitempty"`
	Status GarageBucketStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// GarageBucketList contains a list of GarageBucket
type GarageBucketList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []GarageBucket `json:"items"`
}

func init() {
	SchemeBuilder.Register(&GarageBucket{}, &GarageBucketList{})
}
