#!/usr/bin/env bash
# Test script for release preparation
# This simulates what the GitHub Actions workflow will do

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Helper functions
log_info() {
    echo -e "${GREEN}==>${NC} $1"
}

log_warn() {
    echo -e "${YELLOW}==>${NC} $1"
}

log_error() {
    echo -e "${RED}ERROR:${NC} $1"
}

# Check prerequisites
check_prerequisites() {
    log_info "Checking prerequisites..."
    
    local missing_tools=()
    
    if ! command -v kubectl &> /dev/null; then
        missing_tools+=("kubectl")
    fi
    
    if ! command -v helm &> /dev/null; then
        missing_tools+=("helm")
    fi
    
    if ! command -v docker &> /dev/null; then
        missing_tools+=("docker")
    fi
    
    if [ ${#missing_tools[@]} -ne 0 ]; then
        log_error "Missing required tools: ${missing_tools[*]}"
        log_error "Please install them before continuing"
        return 1
    fi
    
    log_info "All prerequisites met"
}

# Parse version
parse_version() {
    local version=$1
    
    if [[ ! $version =~ ^v[0-9]+\.[0-9]+\.[0-9]+(-[a-zA-Z0-9.]+)?$ ]]; then
        log_error "Invalid version format: $version"
        log_error "Expected format: vX.Y.Z or vX.Y.Z-suffix (e.g., v1.0.0, v1.0.0-alpha.1)"
        return 1
    fi
    
    echo "$version"
}

# Build and test
main() {
    cd "$PROJECT_ROOT"
    
    if [ $# -eq 0 ]; then
        log_error "Usage: $0 <version>"
        log_error "Example: $0 v1.0.0"
        exit 1
    fi
    
    local VERSION
    VERSION=$(parse_version "$1") || exit 1
    local VERSION_NO_V="${VERSION#v}"
    
    log_info "Testing release for version: $VERSION"
    
    # Check prerequisites
    check_prerequisites || exit 1
    
    # Clean previous builds
    log_info "Cleaning previous builds..."
    rm -rf dist/*.yaml dist/*.tgz dist/index.yaml
    
    # Run tests
    log_info "Running tests..."
    if ! make test; then
        log_error "Tests failed"
        exit 1
    fi
    
    # Generate manifests
    log_info "Generating manifests..."
    if ! make manifests; then
        log_error "Manifest generation failed"
        exit 1
    fi
    
    # Build Docker image
    log_info "Building Docker image..."
    export IMG="ghcr.io/dimedis-gmbh/garage-operator:${VERSION}"
    if ! make docker-build; then
        log_error "Docker build failed"
        exit 1
    fi
    
    # Build Kustomize installer
    log_info "Building Kustomize installer..."
    if ! make build-installer; then
        log_error "Kustomize build failed"
        exit 1
    fi
    
    # Validate Kustomize manifests
    log_info "Validating Kustomize manifests..."
    if ! kubectl apply -f dist/install.yaml --dry-run=client > /dev/null; then
        log_error "Kustomize manifest validation failed"
        exit 1
    fi
    log_info "Kustomize manifests valid"
    
    # Update Helm chart
    log_info "Updating Helm chart version to ${VERSION_NO_V}..."
    sed -i "s/^version: .*/version: ${VERSION_NO_V}/" dist/chart/Chart.yaml
    sed -i "s/^appVersion: .*/appVersion: \"${VERSION_NO_V}\"/" dist/chart/Chart.yaml
    sed -i "s|repository: .*|repository: ghcr.io/dimedis-gmbh/garage-operator|" dist/chart/values.yaml
    sed -i "s|tag: .*|tag: ${VERSION}|" dist/chart/values.yaml
    
    # Lint Helm chart
    log_info "Linting Helm chart..."
    if ! helm lint dist/chart; then
        log_error "Helm lint failed"
        exit 1
    fi
    
    # Package Helm chart
    log_info "Packaging Helm chart..."
    if ! helm package dist/chart -d dist/; then
        log_error "Helm package failed"
        exit 1
    fi
    
    # Validate Helm chart
    log_info "Validating Helm chart..."
    if ! helm install garage-operator-test dist/garage-operator-${VERSION_NO_V}.tgz --dry-run --debug > /dev/null; then
        log_error "Helm chart validation failed"
        exit 1
    fi
    log_info "Helm chart valid"
    
    # Create Helm repository index
    log_info "Creating Helm repository index..."
    helm repo index dist/ --url "https://github.com/dimedis-gmbh/garage-operator/releases/download/${VERSION}"
    
    # Summary
    echo
    log_info "Release preparation complete!"
    echo
    echo "Generated artifacts in dist/:"
    ls -lh dist/*.yaml dist/*.tgz 2>/dev/null || true
    echo
    echo "Next steps:"
    echo "  1. Review the generated files"
    echo "  2. Create and push a Git tag:"
    echo "     git tag -a ${VERSION} -m 'Release ${VERSION}'"
    echo "     git push origin ${VERSION}"
    echo "  3. Monitor the GitHub Actions workflow:"
    echo "     https://github.com/dimedis-gmbh/garage-operator/actions"
    echo
    echo "Installation commands for this release:"
    echo
    echo "  Kustomize:"
    echo "    kubectl apply -f https://github.com/dimedis-gmbh/garage-operator/releases/download/${VERSION}/install.yaml"
    echo
    echo "  Helm:"
    echo "    helm install garage-operator \\"
    echo "      https://github.com/dimedis-gmbh/garage-operator/releases/download/${VERSION}/garage-operator-${VERSION_NO_V}.tgz"
    echo
}

main "$@"
