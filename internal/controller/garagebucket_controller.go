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

package controller

import (
	"context"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	garagev1alpha1 "github.com/dimedis-gmbh/garage-operator/api/v1alpha1"
)

const (
	bucketFinalizer = "garage.dimedis.io/bucket-finalizer"
)

// GarageBucketReconciler reconciles a GarageBucket object
type GarageBucketReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=garage.dimedis.io,resources=garagebuckets,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=garage.dimedis.io,resources=garagebuckets/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=garage.dimedis.io,resources=garagebuckets/finalizers,verbs=update
// +kubebuilder:rbac:groups=garage.dimedis.io,resources=garageclusters,verbs=get;list;watch
// +kubebuilder:rbac:groups=core,resources=secrets,verbs=get;list;watch;create;update;patch;delete

func (r *GarageBucketReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	logger.Info("Reconciling GarageBucket")

	// Fetch the GarageBucket instance
	bucket := &garagev1alpha1.GarageBucket{}
	if err := r.Get(ctx, req.NamespacedName, bucket); err != nil {
		if errors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		logger.Error(err, "Failed to get GarageBucket")
		return ctrl.Result{}, err
	}

	// Handle deletion
	if !bucket.ObjectMeta.DeletionTimestamp.IsZero() {
		return r.reconcileDelete(ctx, bucket)
	}

	// Add finalizer if not present
	if !controllerutil.ContainsFinalizer(bucket, bucketFinalizer) {
		logger.Info("Adding finalizer")
		controllerutil.AddFinalizer(bucket, bucketFinalizer)
		if err := r.Update(ctx, bucket); err != nil {
			return ctrl.Result{}, err
		}
		logger.Info("Finalizer added, requeuing")
		return ctrl.Result{Requeue: true}, nil
	}

	// Get referenced GarageCluster
	cluster, err := r.getCluster(ctx, bucket)
	if err != nil {
		logger.Error(err, "Failed to get GarageCluster")
		r.updateStatus(ctx, bucket, "Failed", "GarageCluster not found: "+err.Error())
		// Return with longer requeue time to avoid log spam
		return ctrl.Result{RequeueAfter: 1 * time.Minute}, nil
	}
	logger.V(1).Info("Got cluster", "clusterPhase", cluster.Status.Phase)

	// Check if cluster is ready
	if cluster.Status.Phase != "Ready" {
		msg := fmt.Sprintf("Waiting for GarageCluster %s to be ready (current phase: %s)",
			cluster.Name, cluster.Status.Phase)
		logger.Info(msg)
		r.updateStatus(ctx, bucket, "Pending", msg)
		r.Status().Update(ctx, bucket)
		return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
	}
	logger.V(1).Info("Cluster is ready")

	// Get REST config for bucket operations
	config, err := ctrl.GetConfig()
	if err != nil {
		logger.Error(err, "Failed to get REST config")
		r.updateStatus(ctx, bucket, "Failed", "Failed to get REST config")
		r.Status().Update(ctx, bucket)
		return ctrl.Result{RequeueAfter: 1 * time.Minute}, nil
	}
	logger.V(1).Info("Got REST config")

	// Create bucket manager
	bucketMgr, err := NewBucketManager(config, cluster, logger)
	if err != nil {
		logger.Error(err, "Failed to create BucketManager")
		r.updateStatus(ctx, bucket, "Failed", "BucketManager creation failed: "+err.Error())
		r.Status().Update(ctx, bucket)
		return ctrl.Result{RequeueAfter: 1 * time.Minute}, nil
	}
	logger.V(1).Info("Created BucketManager")

	// Determine bucket name
	bucketName := bucket.Spec.BucketName
	if bucketName == "" {
		bucketName = bucket.Name
	}
	logger.V(1).Info("Determined bucket name", "bucketName", bucketName)

	// Create or verify bucket
	bucketID, err := bucketMgr.CreateBucket(ctx, bucketName)
	if err != nil {
		logger.Error(err, "Failed to create bucket")
		r.updateStatus(ctx, bucket, "Failed", "Bucket creation failed: "+err.Error())
		r.Status().Update(ctx, bucket)
		return ctrl.Result{RequeueAfter: 1 * time.Minute}, nil
	}
	logger.V(1).Info("Created/verified bucket", "bucketID", bucketID)

	bucket.Status.BucketID = bucketID
	bucket.Status.ActualBucketName = bucketName
	bucket.Status.S3Endpoint = cluster.Status.S3Endpoint

	logger.V(1).Info("Starting bucket configuration", "publicRead", bucket.Spec.PublicRead,
		"hasQuotas", bucket.Spec.Quotas != nil, "hasWebsite", bucket.Spec.WebsiteConfig != nil)

	// Configure public read if enabled
	if bucket.Spec.PublicRead {
		logger.V(1).Info("Configuring public read")
		if err := bucketMgr.SetPublicRead(ctx, bucketID, true); err != nil {
			logger.Error(err, "Failed to set public read")
			r.updateStatus(ctx, bucket, "Failed", "Failed to set public read: "+err.Error())
			r.Status().Update(ctx, bucket)
			return ctrl.Result{RequeueAfter: 30 * time.Second}, err
		}
		logger.V(1).Info("Public read configured")
	}

	// Configure quotas if specified
	if bucket.Spec.Quotas != nil {
		logger.V(1).Info("Configuring quotas")
		if err := bucketMgr.SetQuotas(ctx, bucketID, bucket.Spec.Quotas); err != nil {
			logger.Error(err, "Failed to set quotas")
			r.updateStatus(ctx, bucket, "Failed", "Failed to set quotas: "+err.Error())
			r.Status().Update(ctx, bucket)
			return ctrl.Result{RequeueAfter: 30 * time.Second}, err
		}
		logger.V(1).Info("Quotas configured")
	}

	// Configure website if enabled
	if bucket.Spec.WebsiteConfig != nil && bucket.Spec.WebsiteConfig.Enabled {
		logger.V(1).Info("Configuring website")
		if err := bucketMgr.ConfigureWebsite(ctx, bucketID, bucket.Spec.WebsiteConfig); err != nil {
			logger.Error(err, "Failed to configure website")
			r.updateStatus(ctx, bucket, "Failed", "Failed to configure website: "+err.Error())
			r.Status().Update(ctx, bucket)
			return ctrl.Result{RequeueAfter: 30 * time.Second}, err
		}

		// Set website endpoint
		bucket.Status.WebsiteEndpoint = fmt.Sprintf("http://%s.web.%s",
			bucketName, cluster.Spec.S3Api.RootDomain)
		logger.V(1).Info("Website configured")
	}

	logger.V(1).Info("Starting key reconciliation", "keyCount", len(bucket.Spec.Keys))

	// Reconcile access keys
	keyStatuses := []garagev1alpha1.KeyStatus{}
	for i, keySpec := range bucket.Spec.Keys {
		logger.V(1).Info("Reconciling key", "index", i, "keyName", keySpec.Name)
		keyStatus, err := r.reconcileKey(ctx, bucket, cluster, bucketMgr, bucketID, keySpec)
		if err != nil {
			logger.Error(err, "Failed to reconcile key", "keyName", keySpec.Name)
			r.updateStatus(ctx, bucket, "Failed", "Key reconciliation failed: "+err.Error())
			r.Status().Update(ctx, bucket)
			return ctrl.Result{RequeueAfter: 30 * time.Second}, err
		}
		logger.V(1).Info("Key reconciled", "index", i, "keyName", keySpec.Name, "keyID", keyStatus.KeyID)
		keyStatuses = append(keyStatuses, *keyStatus)
	}
	bucket.Status.Keys = keyStatuses
	logger.V(1).Info("All keys reconciled", "count", len(keyStatuses))

	// Update final status only if phase changed
	oldPhase := bucket.Status.Phase
	r.updateStatus(ctx, bucket, "Ready", "Bucket successfully configured")

	if oldPhase != "Ready" {
		logger.Info("Updating final status to Ready")
		if err := r.Status().Update(ctx, bucket); err != nil {
			logger.Error(err, "Failed to update status")
			return ctrl.Result{}, err
		}
	} else {
		logger.V(1).Info("Status unchanged, skipping update")
	} // No automatic requeue - controller will only run on changes to the GarageBucket resource
	logger.Info("Reconciliation complete")
	return ctrl.Result{}, nil
}

func (r *GarageBucketReconciler) getCluster(ctx context.Context, bucket *garagev1alpha1.GarageBucket) (*garagev1alpha1.GarageCluster, error) {
	clusterNamespace := bucket.Spec.ClusterRef.Namespace
	if clusterNamespace == "" {
		clusterNamespace = bucket.Namespace
	}

	cluster := &garagev1alpha1.GarageCluster{}
	err := r.Get(ctx, types.NamespacedName{
		Name:      bucket.Spec.ClusterRef.Name,
		Namespace: clusterNamespace,
	}, cluster)

	return cluster, err
}

func (r *GarageBucketReconciler) reconcileKey(ctx context.Context, bucket *garagev1alpha1.GarageBucket,
	cluster *garagev1alpha1.GarageCluster, bucketMgr *BucketManager, bucketID string,
	keySpec garagev1alpha1.BucketKey) (*garagev1alpha1.KeyStatus, error) {

	logger := log.FromContext(ctx)

	// Determine key name
	keyName := keySpec.Name
	if keyName == "" {
		keyName = fmt.Sprintf("%s-key-%d", bucket.Name, time.Now().Unix())
	}

	// Check if key is expired (for status information only)
	isExpired := false
	if keySpec.ExpirationDate != nil && time.Now().After(keySpec.ExpirationDate.Time) {
		isExpired = true
		logger.Info("Key is expired", "keyName", keyName, "expirationDate", keySpec.ExpirationDate)
	}

	// Check if expiration date changed - if so, we need to recreate the key
	var existingKeyStatus *garagev1alpha1.KeyStatus
	for i := range bucket.Status.Keys {
		if bucket.Status.Keys[i].Name == keyName {
			existingKeyStatus = &bucket.Status.Keys[i]
			break
		}
	}

	logger.Info("Checking for key recreation", "keyName", keyName,
		"hasExistingStatus", existingKeyStatus != nil,
		"newExpiration", keySpec.ExpirationDate)

	if existingKeyStatus != nil {
		logger.Info("Found existing key status",
			"oldExpiration", existingKeyStatus.ExpirationDate,
			"oldKeyID", existingKeyStatus.KeyID)
	}

	needsRecreate := false
	if existingKeyStatus != nil {
		// Check if expiration date changed
		if (keySpec.ExpirationDate == nil && existingKeyStatus.ExpirationDate != nil) ||
			(keySpec.ExpirationDate != nil && existingKeyStatus.ExpirationDate == nil) ||
			(keySpec.ExpirationDate != nil && existingKeyStatus.ExpirationDate != nil &&
				!keySpec.ExpirationDate.Time.Equal(existingKeyStatus.ExpirationDate.Time)) {
			needsRecreate = true
			logger.Info("Key expiration date changed, recreating key", "keyName", keyName,
				"oldExpiration", existingKeyStatus.ExpirationDate, "newExpiration", keySpec.ExpirationDate)
		}
	}

	// If key needs to be recreated, delete the old one first
	if needsRecreate && existingKeyStatus != nil {
		logger.Info("Deleting old key before recreation", "keyID", existingKeyStatus.KeyID)
		if err := bucketMgr.DeleteKey(ctx, existingKeyStatus.KeyID); err != nil {
			logger.Error(err, "Failed to delete old key", "keyID", existingKeyStatus.KeyID)
			// Continue anyway to try creating new key
		} else {
			logger.Info("Old key deleted successfully")
		}
	}

	// Create or get key (with expiration if specified)
	logger.Info("Calling CreateKey", "keyName", keyName, "expiresAt", keySpec.ExpirationDate)
	keyID, secretKey, err := bucketMgr.CreateKey(ctx, keyName, keySpec.ExpirationDate)
	if err != nil {
		return nil, fmt.Errorf("failed to create key: %w", err)
	}

	// Set key permissions on bucket
	if err := bucketMgr.SetKeyPermissions(ctx, bucketID, keyID, keySpec.Permissions); err != nil {
		return nil, fmt.Errorf("failed to set key permissions: %w", err)
	}

	// Determine secret name and namespace
	secretName := ""
	secretNamespace := bucket.Namespace

	if keySpec.SecretRef != nil {
		secretName = keySpec.SecretRef.Name
		if keySpec.SecretRef.Namespace != "" {
			secretNamespace = keySpec.SecretRef.Namespace
		}
	}

	if secretName == "" {
		secretName = fmt.Sprintf("%s-%s", bucket.Name, keyName)
	}

	// Only create/update secret if we have the secret key
	// (secretKey is empty if key already existed and wasn't recreated)
	if secretKey != "" {
		// Determine public endpoint if ingress is configured
		publicEndpoint := ""
		if cluster.Spec.Ingress != nil && cluster.Spec.Ingress.Enabled && cluster.Spec.Ingress.Host != "" {
			scheme := "http"
			if cluster.Spec.Ingress.TLS != nil && cluster.Spec.Ingress.TLS.Enabled {
				scheme = "https"
			}
			publicEndpoint = fmt.Sprintf("%s://%s", scheme, cluster.Spec.Ingress.Host)
		}

		// Create or update secret
		secretData := map[string]string{
			"accessKeyId":     keyID,
			"secretAccessKey": secretKey,
			"bucket":          bucket.Status.ActualBucketName,
			"serviceEndpoint": cluster.Status.S3Endpoint,
		}

		// Add publicEndpoint if available
		if publicEndpoint != "" {
			secretData["publicEndpoint"] = publicEndpoint
		}

		secret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      secretName,
				Namespace: secretNamespace,
			},
			Type:       corev1.SecretTypeOpaque,
			StringData: secretData,
		}

		// Only set owner reference if secret is in same namespace
		if secretNamespace == bucket.Namespace {
			if err := controllerutil.SetControllerReference(bucket, secret, r.Scheme); err != nil {
				return nil, fmt.Errorf("failed to set owner reference: %w", err)
			}
		}

		foundSecret := &corev1.Secret{}
		err = r.Get(ctx, types.NamespacedName{Name: secretName, Namespace: secretNamespace}, foundSecret)
		if err != nil && errors.IsNotFound(err) {
			if err := r.Create(ctx, secret); err != nil {
				return nil, fmt.Errorf("failed to create secret: %w", err)
			}
			logger.Info("Created secret for key", "secretName", secretName, "keyID", keyID)
		} else if err == nil {
			// Check if secret data actually changed
			needsUpdate := false
			if string(foundSecret.Data["accessKeyId"]) != keyID ||
				string(foundSecret.Data["secretAccessKey"]) != secretKey ||
				string(foundSecret.Data["bucket"]) != bucket.Status.ActualBucketName ||
				string(foundSecret.Data["serviceEndpoint"]) != cluster.Status.S3Endpoint ||
				string(foundSecret.Data["publicEndpoint"]) != publicEndpoint {
				needsUpdate = true
			}

			if needsUpdate {
				foundSecret.StringData = secret.StringData
				if err := r.Update(ctx, foundSecret); err != nil {
					return nil, fmt.Errorf("failed to update secret: %w", err)
				}
				logger.Info("Updated secret for key", "secretName", secretName, "keyID", keyID)
			}
		} else {
			return nil, fmt.Errorf("failed to get secret: %w", err)
		}
	}

	return &garagev1alpha1.KeyStatus{
		Name:            keyName,
		KeyID:           keyID,
		SecretName:      secretName,
		SecretNamespace: secretNamespace,
		ExpirationDate:  keySpec.ExpirationDate,
		Expired:         isExpired,
	}, nil
}

func (r *GarageBucketReconciler) reconcileDelete(ctx context.Context, bucket *garagev1alpha1.GarageBucket) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	if controllerutil.ContainsFinalizer(bucket, bucketFinalizer) {
		logger.Info("Cleaning up GarageBucket resources")

		// Get cluster
		cluster, err := r.getCluster(ctx, bucket)
		if err == nil && cluster.Status.Phase == "Ready" {
			// Get REST config
			config, err := ctrl.GetConfig()
			if err == nil {
				// Create bucket manager
				bucketMgr, err := NewBucketManager(config, cluster, logger)
				if err == nil {
					// Delete keys
					for _, keyStatus := range bucket.Status.Keys {
						if err := bucketMgr.DeleteKey(ctx, keyStatus.KeyID); err != nil {
							logger.Error(err, "Failed to delete key", "keyID", keyStatus.KeyID)
						}

						// Delete secret if in same namespace
						if keyStatus.SecretNamespace == bucket.Namespace {
							secret := &corev1.Secret{
								ObjectMeta: metav1.ObjectMeta{
									Name:      keyStatus.SecretName,
									Namespace: keyStatus.SecretNamespace,
								},
							}
							if err := r.Delete(ctx, secret); err != nil && !errors.IsNotFound(err) {
								logger.Error(err, "Failed to delete secret", "secretName", keyStatus.SecretName)
							}
						}
					}

					// Delete bucket
					if bucket.Status.BucketID != "" {
						if err := bucketMgr.DeleteBucket(ctx, bucket.Status.BucketID); err != nil {
							logger.Error(err, "Failed to delete bucket", "bucketID", bucket.Status.BucketID)
						}
					}
				}
			}
		}

		controllerutil.RemoveFinalizer(bucket, bucketFinalizer)
		if err := r.Update(ctx, bucket); err != nil {
			return ctrl.Result{}, err
		}
	}
	return ctrl.Result{}, nil
}

func (r *GarageBucketReconciler) updateStatus(ctx context.Context, bucket *garagev1alpha1.GarageBucket, phase, message string) {
	bucket.Status.Phase = phase
	r.Status().Update(ctx, bucket)
}

// SetupWithManager sets up the controller with the Manager.
func (r *GarageBucketReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&garagev1alpha1.GarageBucket{}).
		Complete(r)
}
