# Release Process

This document describes how to create and publish releases for the Garage Operator.

## Overview

The release process is fully automated via GitHub Actions. When you push a Git tag matching the pattern `v*.*.*`, the workflow will:

1. Build and push multi-arch Docker images to GitHub Container Registry
2. Generate Kustomize installation manifests
3. Package and publish Helm charts
4. Create a GitHub Release with all artifacts

## Release Workflow

### 1. Prepare for Release

Before creating a release, ensure:

- All changes are merged to `main`
- Tests pass (`make test`)
- E2E tests pass (`make test-e2e`)
- Code is properly formatted and linted (`make fmt`, `make vet`, `make lint`)

### 2. Create a Release Tag

Follow [Semantic Versioning](https://semver.org/):

- **Major version** (v2.0.0): Breaking changes
- **Minor version** (v1.1.0): New features, backward compatible
- **Patch version** (v1.0.1): Bug fixes, backward compatible

Create and push a tag:

```bash
# Example: Release version 1.0.0
VERSION=v1.0.0

# Create annotated tag
git tag -a ${VERSION} -m "Release ${VERSION}"

# Push tag to trigger release workflow
git push origin ${VERSION}
```

### 3. Monitor the Release Workflow

1. Go to: https://github.com/dimedis-gmbh/garage-operator/actions
2. Watch the "Release" workflow
3. The workflow will:
   - Build Docker images for `linux/amd64` and `linux/arm64`
   - Push images to `ghcr.io/dimedis-gmbh/garage-operator:VERSION`
   - Generate `install.yaml` and `install-VERSION.yaml`
   - Package Helm chart as `garage-operator-VERSION.tgz`
   - Create GitHub Release with all artifacts

### 4. Verify the Release

After the workflow completes:

1. Check the [Releases page](https://github.com/dimedis-gmbh/garage-operator/releases)
2. Verify the release contains:

   - `install.yaml` - Latest Kustomize manifest
   - `install-vX.Y.Z.yaml` - Versioned Kustomize manifest
   - `garage-operator-X.Y.Z.tgz` - Helm chart package
   - `index.yaml` - Helm repository index
   - Release notes with installation instructions

3. Test the installation:

```bash
# Test Kustomize installation
kubectl apply -f https://github.com/dimedis-gmbh/garage-operator/releases/download/${VERSION}/install.yaml

# Test Helm installation
helm install garage-operator \
  https://github.com/dimedis-gmbh/garage-operator/releases/download/${VERSION}/garage-operator-X.Y.Z.tgz
```

## Installation Methods

### Using Kustomize (Recommended for GitOps)

**Latest version:**

```bash
kubectl apply -f https://github.com/dimedis-gmbh/garage-operator/releases/latest/download/install.yaml
```

**Specific version:**

```bash
kubectl apply -f https://github.com/dimedis-gmbh/garage-operator/releases/download/v1.0.0/install-v1.0.0.yaml
```

### Using Helm

**Direct chart URL:**

```bash
helm install garage-operator \
  https://github.com/dimedis-gmbh/garage-operator/releases/download/v1.0.0/garage-operator-1.0.0.tgz
```

**Using Helm repository:**

```bash
# Add repository (points to specific release)
helm repo add garage-operator https://github.com/dimedis-gmbh/garage-operator/releases/download/v1.0.0

# Update repositories
helm repo update

# Install
helm install garage-operator garage-operator/garage-operator --version 1.0.0
```

**With custom values:**

```bash
helm install garage-operator \
  https://github.com/dimedis-gmbh/garage-operator/releases/download/v1.0.0/garage-operator-1.0.0.tgz \
  --values custom-values.yaml \
  --namespace garage-operator-system \
  --create-namespace
```

## Docker Images

Images are published to GitHub Container Registry with multiple tags:

```bash
# Specific version
ghcr.io/dimedis-gmbh/garage-operator:v1.0.0

# Major.minor version (auto-updated)
ghcr.io/dimedis-gmbh/garage-operator:1.0

# Major version (auto-updated)
ghcr.io/dimedis-gmbh/garage-operator:1

# Latest (auto-updated)
ghcr.io/dimedis-gmbh/garage-operator:latest
```

Supported platforms:

- `linux/amd64`
- `linux/arm64`

## Pre-releases

For alpha, beta, or release candidate versions, use appropriate suffixes:

```bash
# Examples
git tag -a v1.0.0-alpha.1 -m "Release v1.0.0-alpha.1"
git tag -a v1.0.0-beta.1 -m "Release v1.0.0-beta.1"
git tag -a v1.0.0-rc.1 -m "Release v1.0.0-rc.1"
git push origin v1.0.0-alpha.1
```

Pre-releases will be marked as such in GitHub Releases.

## Testing Release Process Locally

To test the release artifacts locally before creating a tag:

```bash
# Prepare release artifacts
make release-prepare VERSION=v1.0.0

# This will create in dist/:
# - install.yaml (Kustomize manifest)
# - garage-operator-1.0.0.tgz (Helm chart)

# Test Kustomize manifest
kubectl apply -f dist/install.yaml --dry-run=client

# Test Helm chart
helm install garage-operator dist/garage-operator-1.0.0.tgz --dry-run --debug
```

Or use the test script:

```bash
./hack/test-release.sh v1.0.0
```

## Troubleshooting

### Release workflow fails

1. Check the workflow logs in GitHub Actions
2. Common issues:
   - Docker build failures: Check Dockerfile and build context
   - Kustomize errors: Run `make manifests` locally
   - Helm package errors: Validate `dist/chart/Chart.yaml`

### Docker image not public

GitHub Container Registry packages are private by default. To make them public:

1. Go to https://github.com/orgs/dimedis-gmbh/packages/container/garage-operator/settings
2. Change visibility to "Public"
3. Or configure in GitHub Actions workflow with appropriate permissions

### Helm chart values incorrect

The workflow automatically updates `dist/chart/values.yaml` with the correct image and tag. If manual changes are needed:

```bash
# Edit values.yaml
vim dist/chart/values.yaml

# Test locally
helm install test dist/chart --dry-run --debug

# Commit changes
git add dist/chart/values.yaml
git commit -m "Update Helm chart configuration"
```

### Webhook validation errors

Webhooks are disabled by default (`webhook.enable: false` in values.yaml). If you encounter webhook-related errors:

```bash
# Check current webhook setting
grep -A 2 "webhook:" dist/chart/values.yaml

# Ensure it's set to false
# webhook:
#   enable: false
```

## Release Checklist

Before creating a release:

- [ ] All tests pass
- [ ] CHANGELOG updated (if maintained)
- [ ] README updated with new features
- [ ] Documentation is up to date
- [ ] Version follows semantic versioning
- [ ] No breaking changes in minor/patch releases
- [ ] Migration guide provided for breaking changes

After release:

- [ ] GitHub Release created successfully
- [ ] Docker images are public and accessible
- [ ] Installation tested with Kustomize
- [ ] Installation tested with Helm
- [ ] Release announced (if applicable)

## Rollback a Release

If a release has critical issues:

1. **Do not delete the Git tag** (breaks existing installations)
2. Instead, create a new patch release with fixes:

```bash
# Fix the issue
git commit -m "Fix critical bug from v1.0.0"

# Create new patch release
git tag -a v1.0.1 -m "Release v1.0.1 - Fix critical bug"
git push origin v1.0.1
```

3. Update documentation to recommend the newer version
4. Optionally mark the problematic release as "pre-release" in GitHub

## Versioning Strategy

We follow [Semantic Versioning 2.0.0](https://semver.org/):

- **MAJOR** version for incompatible API changes
- **MINOR** version for backwards-compatible new features
- **PATCH** version for backwards-compatible bug fixes

### CRD Versioning

- API version changes (e.g., v1alpha1 → v1beta1) require a MAJOR version bump
- New optional fields in CRDs can be MINOR version bumps
- Required new fields in CRDs require a MAJOR version bump

### Operator Compatibility Matrix

Document which operator versions support which Kubernetes versions:

| Operator Version | Kubernetes Version | Garage Version |
| ---------------- | ------------------ | -------------- |
| v1.0.x           | 1.25 - 1.31        | v2.1.0         |

This should be maintained in the main README.md.

## Additional Resources

- [Semantic Versioning](https://semver.org/)
- [GitHub Actions Documentation](https://docs.github.com/en/actions)
- [Helm Chart Best Practices](https://helm.sh/docs/chart_best_practices/)
- [Kustomize Documentation](https://kustomize.io/)
