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
	"encoding/json"
	"fmt"
	"time"

	garagev1alpha1 "github.com/dimedis-gmbh/garage-operator/api/v1alpha1"
	"github.com/go-logr/logr"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/rest"
)

// BucketManager handles Garage bucket operations
type BucketManager struct {
	client  *GarageClient
	cluster *garagev1alpha1.GarageCluster
	logger  logr.Logger
}

// BucketInfo represents Garage bucket information
type BucketInfo struct {
	ID                string           `json:"id"`
	GlobalAliases     []string         `json:"globalAliases"`
	WebsiteAccess     bool             `json:"websiteAccess"`
	Keys              []BucketKeyInfo  `json:"keys"`
	Objects           int64            `json:"objects"`
	Bytes             int64            `json:"bytes"`
	UnfinishedUploads int64            `json:"unfinishedUploads"`
	Quotas            BucketQuotasInfo `json:"quotas"`
}

// BucketKeyInfo represents key information for a bucket
type BucketKeyInfo struct {
	AccessKeyID string `json:"accessKeyId"`
	Name        string `json:"name"`
	Permissions struct {
		Read  bool `json:"read"`
		Write bool `json:"write"`
		Owner bool `json:"owner"`
	} `json:"permissions"`
}

// BucketQuotasInfo represents bucket quota information
type BucketQuotasInfo struct {
	MaxSize    *int64 `json:"maxSize,omitempty"`
	MaxObjects *int64 `json:"maxObjects,omitempty"`
}

// KeyInfo represents Garage key information
type KeyInfo struct {
	AccessKeyID     string `json:"accessKeyId"`
	Name            string `json:"name"`
	SecretAccessKey string `json:"secretAccessKey"`
}

// NewBucketManager creates a new BucketManager
func NewBucketManager(config *rest.Config, cluster *garagev1alpha1.GarageCluster, logger logr.Logger) (*BucketManager, error) {
	client, err := NewGarageClient(config, cluster)
	if err != nil {
		return nil, fmt.Errorf("failed to create garage client: %w", err)
	}
	return &BucketManager{
		client:  client,
		cluster: cluster,
		logger:  logger,
	}, nil
}

// CreateBucket creates a bucket in Garage or returns existing bucket ID
func (bm *BucketManager) CreateBucket(ctx context.Context, bucketName string) (string, error) {
	// First, try to find bucket by global alias
	getBucketPayload := map[string]interface{}{"globalAlias": bucketName}
	output, err := bm.client.ExecJSON(ctx, "GetBucketInfo", getBucketPayload)
	if err == nil {
		// Bucket exists, parse and return ID
		var bucketInfo BucketInfo
		if err := json.Unmarshal([]byte(output), &bucketInfo); err == nil {
			return bucketInfo.ID, nil
		}
	}

	// Bucket doesn't exist, create it
	createBucketPayload := map[string]interface{}{"globalAlias": bucketName}
	createOutput, err := bm.client.ExecJSON(ctx, "CreateBucket", createBucketPayload)
	if err != nil {
		return "", fmt.Errorf("failed to create bucket: %w", err)
	}

	var bucketInfo BucketInfo
	if err := json.Unmarshal([]byte(createOutput), &bucketInfo); err != nil {
		return "", fmt.Errorf("failed to parse bucket info: %w", err)
	}

	return bucketInfo.ID, nil
}

// SetPublicRead sets public read access for a bucket
func (bm *BucketManager) SetPublicRead(ctx context.Context, bucketID string, enabled bool) error {
	args := []string{"bucket", "website"}
	if enabled {
		args = append(args, "--allow")
	} else {
		args = append(args, "--deny")
	}
	args = append(args, bucketID)

	_, err := bm.client.ExecCommand(ctx, args...)
	return err
}

// SetQuotas sets quotas for a bucket
func (bm *BucketManager) SetQuotas(ctx context.Context, bucketID string, quotas *garagev1alpha1.BucketQuotas) error {
	if quotas == nil {
		return nil
	}

	args := []string{"bucket", "set-quotas", bucketID}

	if quotas.MaxSize != "" {
		args = append(args, "--max-size", quotas.MaxSize)
	}

	if quotas.MaxObjects != nil {
		args = append(args, "--max-objects", fmt.Sprintf("%d", *quotas.MaxObjects))
	}

	_, err := bm.client.ExecCommand(ctx, args...)
	return err
}

// ConfigureWebsite configures website hosting for a bucket
func (bm *BucketManager) ConfigureWebsite(ctx context.Context, bucketID string, config *garagev1alpha1.WebsiteConfig) error {
	if config == nil || !config.Enabled {
		return nil
	}

	// Enable website access
	if err := bm.SetPublicRead(ctx, bucketID, true); err != nil {
		return err
	}

	// Set index document
	indexDoc := config.IndexDocument
	if indexDoc == "" {
		indexDoc = "index.html"
	}

	args := []string{"bucket", "website", "--index", indexDoc}

	if config.ErrorDocument != "" {
		args = append(args, "--error", config.ErrorDocument)
	}

	args = append(args, bucketID)

	_, err := bm.client.ExecCommand(ctx, args...)
	return err
}

// CreateKey creates a new access key or returns existing one
func (bm *BucketManager) CreateKey(ctx context.Context, keyName string, expiresAt *metav1.Time) (string, string, error) {
	// Try to get existing key by name (using search)
	searchPayload := map[string]interface{}{"search": keyName}
	output, err := bm.client.ExecJSON(ctx, "GetKeyInfo", searchPayload)
	if err == nil {
		// Key exists
		var keyInfo KeyInfo
		if err := json.Unmarshal([]byte(output), &keyInfo); err == nil {
			// Note: SecretAccessKey is only returned on creation, not on retrieval
			// For existing keys, we can't get the secret again
			if keyInfo.SecretAccessKey != "" {
				return keyInfo.AccessKeyID, keyInfo.SecretAccessKey, nil
			}
			// Return only the access key ID, secret will be empty
			return keyInfo.AccessKeyID, "", nil
		}
	}

	// Key doesn't exist, create it
	createPayload := map[string]interface{}{"name": keyName}

	// Add expiration if specified (RFC 3339 format)
	if expiresAt != nil {
		expirationStr := expiresAt.Format(time.RFC3339)
		createPayload["expiration"] = expirationStr
		bm.logger.V(1).Info("Creating key with expiration", "keyName", keyName, "expiration", expirationStr)
	} else {
		bm.logger.V(1).Info("Creating key without expiration", "keyName", keyName)
	}

	bm.logger.V(1).Info("CreateKey payload", "payload", createPayload)

	createOutput, err := bm.client.ExecJSON(ctx, "CreateKey", createPayload)
	if err != nil {
		return "", "", fmt.Errorf("failed to create key: %w", err)
	}

	bm.logger.V(1).Info("CreateKey response", "response", createOutput)

	var keyInfo KeyInfo
	if err := json.Unmarshal([]byte(createOutput), &keyInfo); err != nil {
		return "", "", fmt.Errorf("failed to parse key info: %w", err)
	}

	return keyInfo.AccessKeyID, keyInfo.SecretAccessKey, nil
}

// SetKeyPermissions sets permissions for a key on a bucket
func (bm *BucketManager) SetKeyPermissions(ctx context.Context, bucketID, keyID string, permissions garagev1alpha1.KeyPermissions) error {
	args := []string{"bucket", "allow"}

	if permissions.Read {
		args = append(args, "--read")
	}

	if permissions.Write {
		args = append(args, "--write")
	}

	if permissions.Owner {
		args = append(args, "--owner")
	}

	args = append(args, bucketID, "--key", keyID)

	_, err := bm.client.ExecCommand(ctx, args...)
	return err
}

// DeleteKey deletes an access key
func (bm *BucketManager) DeleteKey(ctx context.Context, keyID string) error {
	_, err := bm.client.ExecCommand(ctx, "key", "delete", "--yes", keyID)
	return err
}

// DeleteBucket deletes a bucket
func (bm *BucketManager) DeleteBucket(ctx context.Context, bucketID string) error {
	_, err := bm.client.ExecCommand(ctx, "bucket", "delete", "--yes", bucketID)
	return err
}
