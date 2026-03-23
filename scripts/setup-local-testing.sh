#!/bin/bash

# Setup script for local testing of Azure DevOps to GitHub Migration Operator
# This script sets up the complete local testing environment

set -e

# Configuration
NAMESPACE="ado-git-migration"
OPERATOR_IMAGE="ado-migration-operator:latest"
KUBECONFIG_CONTEXT=""

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Logging functions
log() {
    echo -e "${BLUE}[$(date +'%Y-%m-%d %H:%M:%S')]${NC} $1"
}

success() {
    echo -e "${GREEN}✓${NC} $1"
}

warning() {
    echo -e "${YELLOW}⚠${NC} $1"
}

error() {
    echo -e "${RED}✗${NC} $1"
}

# Check prerequisites
check_prerequisites() {
    log "Checking prerequisites..."
    
    # Check Go version
    if ! command -v go &> /dev/null; then
        error "Go is not installed. Please install Go 1.24+"
        exit 1
    fi
    
    local go_version=$(go version | grep -oE 'go[0-9]+\.[0-9]+' | sed 's/go//')
    local major_version=$(echo $go_version | cut -d. -f1)
    local minor_version=$(echo $go_version | cut -d. -f2)
    
    if [[ $major_version -lt 1 ]] || [[ $major_version -eq 1 && $minor_version -lt 24 ]]; then
        error "Go version $go_version is too old. Please install Go 1.24+"
        exit 1
    fi
    success "Go version $go_version is compatible"
    
    # Check kubectl
    if ! command -v kubectl &> /dev/null; then
        error "kubectl is not installed. Please install kubectl"
        exit 1
    fi
    success "kubectl is available"
    
    # Check Docker
    if ! command -v docker &> /dev/null; then
        error "Docker is not installed. Please install Docker"
        exit 1
    fi
    success "Docker is available"
    
    # Check if Kubernetes cluster is accessible
    if ! kubectl cluster-info &> /dev/null; then
        error "Kubernetes cluster is not accessible. Please check your kubeconfig"
        exit 1
    fi
    success "Kubernetes cluster is accessible"
    
    # Check if make is available
    if ! command -v make &> /dev/null; then
        error "make is not installed. Please install make"
        exit 1
    fi
    success "make is available"
}

# Setup project dependencies
setup_dependencies() {
    log "Setting up project dependencies..."
    
    # Download Go modules
    go mod download
    success "Go modules downloaded"
    
    # Install kubebuilder tools
    make install-tools 2>/dev/null || {
        log "Installing kubebuilder tools manually..."
        
        # Install controller-gen
        go install sigs.k8s.io/controller-tools/cmd/controller-gen@latest
        
        # Install kustomize
        go install sigs.k8s.io/kustomize/kustomize/v5@latest
        
        # Install setup-envtest
        go install sigs.k8s.io/controller-runtime/tools/setup-envtest@latest
    }
    success "Kubebuilder tools installed"
}

# Create namespace
create_namespace() {
    log "Creating namespace: $NAMESPACE"
    
    if kubectl get namespace "$NAMESPACE" &> /dev/null; then
        warning "Namespace $NAMESPACE already exists"
    else
        kubectl create namespace "$NAMESPACE"
        success "Namespace $NAMESPACE created"
    fi
    
    # Set as default namespace for convenience
    kubectl config set-context --current --namespace="$NAMESPACE"
    success "Default namespace set to $NAMESPACE"
}

# Install CRDs
install_crds() {
    log "Installing Custom Resource Definitions..."
    
    # Generate manifests
    make manifests
    success "Manifests generated"
    
    # Install CRDs
    make install
    success "CRDs installed"
    
    # Verify CRDs
    local crds=(
        "adodiscoveries.migration.ado-to-git-migration.io"
        "adotogitmigrations.migration.ado-to-git-migration.io"
        "pipelinetoworkflows.migration.ado-to-git-migration.io"
    )
    
    for crd in "${crds[@]}"; do
        if kubectl get crd "$crd" &> /dev/null; then
            success "CRD $crd is installed"
        else
            error "CRD $crd is not installed"
            exit 1
        fi
    done
}

# Create secrets interactively
create_secrets() {
    log "Creating authentication secrets..."
    
    # Check if secrets already exist
    if kubectl get secret azure-sp-secret -n "$NAMESPACE" &> /dev/null; then
        warning "Azure Service Principal secret already exists"
        read -p "Do you want to recreate it? (y/N): " -n 1 -r
        echo
        if [[ $REPLY =~ ^[Yy]$ ]]; then
            kubectl delete secret azure-sp-secret -n "$NAMESPACE"
        else
            log "Skipping Azure Service Principal secret creation"
        fi
    fi
    
    if ! kubectl get secret azure-sp-secret -n "$NAMESPACE" &> /dev/null; then
        echo
        log "Creating Azure Service Principal secret..."
        echo "Please provide your Azure Service Principal credentials:"
        
        read -p "Client ID: " client_id
        read -s -p "Client Secret: " client_secret
        echo
        read -p "Tenant ID: " tenant_id
        
        kubectl create secret generic azure-sp-secret \
            --namespace="$NAMESPACE" \
            --from-literal=client-id="$client_id" \
            --from-literal=client-secret="$client_secret" \
            --from-literal=tenant-id="$tenant_id"
        
        success "Azure Service Principal secret created"
    fi
    
    # GitHub PAT secret
    if kubectl get secret github-token-secret -n "$NAMESPACE" &> /dev/null; then
        warning "GitHub token secret already exists"
        read -p "Do you want to recreate it? (y/N): " -n 1 -r
        echo
        if [[ $REPLY =~ ^[Yy]$ ]]; then
            kubectl delete secret github-token-secret -n "$NAMESPACE"
        else
            log "Skipping GitHub token secret creation"
        fi
    fi
    
    if ! kubectl get secret github-token-secret -n "$NAMESPACE" &> /dev/null; then
        echo
        log "Creating GitHub Personal Access Token secret..."
        echo "Please provide your GitHub Personal Access Token:"
        echo "Required scopes: repo, admin:org, workflow"
        
        read -s -p "GitHub PAT: " github_token
        echo
        
        kubectl create secret generic github-token-secret \
            --namespace="$NAMESPACE" \
            --from-literal=token="$github_token"
        
        success "GitHub token secret created"
    fi
}

# Build operator
build_operator() {
    log "Building operator..."
    
    # Build binary
    make build
    success "Operator binary built"
    
    # Build Docker image
    make docker-build IMG="$OPERATOR_IMAGE"
    success "Docker image built: $OPERATOR_IMAGE"
    
    # Load image to cluster (for kind/minikube)
    if command -v kind &> /dev/null && kind get clusters | grep -q "kind"; then
        log "Loading image to kind cluster..."
        kind load docker-image "$OPERATOR_IMAGE"
        success "Image loaded to kind cluster"
    elif command -v minikube &> /dev/null && minikube status | grep -q "Running"; then
        log "Loading image to minikube..."
        minikube image load "$OPERATOR_IMAGE"
        success "Image loaded to minikube"
    fi
}

# Create test resources
create_test_resources() {
    log "Creating test resources..."
    
    # Create test discovery resource
    cat > /tmp/test-discovery.yaml << EOF
apiVersion: migration.ado-to-git-migration.io/v1
kind: AdoDiscovery
metadata:
  name: test-discovery
  namespace: $NAMESPACE
spec:
  source:
    organization: "test-org"
    project: "test-project"
    auth:
      servicePrincipal:
        clientId: "test-client-id"
        tenantId: "test-tenant-id"
        clientSecretRef:
          name: azure-sp-secret
          key: client-secret
  scope:
    organizations: true
    projects: true
    repositories: true
  settings:
    maxItems: 100
    parallelWorkers: 3
    timeoutMinutes: 10
EOF

    kubectl apply -f /tmp/test-discovery.yaml
    success "Test discovery resource created"
    
    # Create test migration resource
    cat > /tmp/test-migration.yaml << EOF
apiVersion: migration.ado-to-git-migration.io/v1
kind: AdoToGitMigration
metadata:
  name: test-migration
  namespace: $NAMESPACE
spec:
  type: repository
  source:
    organization: "test-org"
    project: "test-project"
    auth:
      servicePrincipal:
        clientId: "test-client-id"
        tenantId: "test-tenant-id"
        clientSecretRef:
          name: azure-sp-secret
          key: client-secret
  target:
    owner: "test-github-org"
    auth:
      tokenRef:
        name: github-token-secret
        key: token
  settings:
    maxHistoryDays: 500
    maxCommitCount: 2000
    batchSize: 10
  resources:
    - type: repository
      sourceId: "test-repo-id"
      sourceName: "test-repo"
      targetName: "migrated-test-repo"
EOF

    kubectl apply -f /tmp/test-migration.yaml
    success "Test migration resource created"
    
    # Cleanup temp files
    rm -f /tmp/test-discovery.yaml /tmp/test-migration.yaml
}

# Setup monitoring
setup_monitoring() {
    log "Setting up monitoring and debugging tools..."
    
    # Create a simple monitoring script
    cat > monitor-operator.sh << 'EOF'
#!/bin/bash

NAMESPACE="ado-git-migration"

echo "=== Operator Status ==="
kubectl get pods -n "$NAMESPACE-system" 2>/dev/null || echo "Operator not deployed to cluster"

echo -e "\n=== Custom Resources ==="
echo "Migrations:"
kubectl get adotogitmigration -n "$NAMESPACE" 2>/dev/null || echo "No migrations found"

echo -e "\nDiscoveries:"
kubectl get adodiscovery -n "$NAMESPACE" 2>/dev/null || echo "No discoveries found"

echo -e "\nPipeline Conversions:"
kubectl get pipelinetoworkflow -n "$NAMESPACE" 2>/dev/null || echo "No pipeline conversions found"

echo -e "\n=== Secrets ==="
kubectl get secrets -n "$NAMESPACE"

echo -e "\n=== Recent Events ==="
kubectl get events -n "$NAMESPACE" --sort-by='.lastTimestamp' | tail -10
EOF

    chmod +x monitor-operator.sh
    success "Monitoring script created: ./monitor-operator.sh"
    
    # Create log viewing script
    cat > view-logs.sh << 'EOF'
#!/bin/bash

NAMESPACE="ado-git-migration"

if pgrep -f "bin/manager" > /dev/null; then
    echo "Operator is running locally. Check the terminal where you ran 'make run'"
else
    echo "Checking for operator pods in cluster..."
    kubectl logs -f deployment/ado-git-migration-controller-manager -n "$NAMESPACE-system" 2>/dev/null || {
        echo "Operator not found in cluster. Run 'make run' to start locally or 'make deploy' to deploy to cluster"
    }
fi
EOF

    chmod +x view-logs.sh
    success "Log viewing script created: ./view-logs.sh"
}

# Create helpful aliases and functions
create_helpers() {
    log "Creating helper scripts..."
    
    # Create quick test script
    cat > quick-test.sh << 'EOF'
#!/bin/bash

BASE_URL="http://localhost:8080/api/v1"
NAMESPACE="ado-git-migration"

echo "=== Quick Health Check ==="
curl -s "$BASE_URL/utils/health" | jq . || echo "Operator not responding"

echo -e "\n=== List Migrations ==="
curl -s "$BASE_URL/migrations?namespace=$NAMESPACE" | jq . || echo "Failed to get migrations"

echo -e "\n=== List Discoveries ==="
curl -s "$BASE_URL/discovery?namespace=$NAMESPACE" | jq . || echo "Failed to get discoveries"

echo -e "\n=== WebSocket Test ==="
if command -v wscat &> /dev/null; then
    timeout 3s wscat -c "ws://localhost:8080/ws" -x '{"type":"ping"}' 2>/dev/null || echo "WebSocket test failed"
else
    echo "Install wscat for WebSocket testing: npm install -g wscat"
fi
EOF

    chmod +x quick-test.sh
    success "Quick test script created: ./quick-test.sh"
    
    # Create cleanup script
    cat > cleanup.sh << 'EOF'
#!/bin/bash

NAMESPACE="ado-git-migration"

echo "Cleaning up test resources..."

# Delete test resources
kubectl delete adotogitmigration test-migration -n "$NAMESPACE" 2>/dev/null || true
kubectl delete adodiscovery test-discovery -n "$NAMESPACE" 2>/dev/null || true

# Delete secrets (optional)
read -p "Delete secrets? (y/N): " -n 1 -r
echo
if [[ $REPLY =~ ^[Yy]$ ]]; then
    kubectl delete secret azure-sp-secret github-token-secret -n "$NAMESPACE" 2>/dev/null || true
    echo "Secrets deleted"
fi

# Delete namespace (optional)
read -p "Delete namespace $NAMESPACE? (y/N): " -n 1 -r
echo
if [[ $REPLY =~ ^[Yy]$ ]]; then
    kubectl delete namespace "$NAMESPACE" 2>/dev/null || true
    echo "Namespace deleted"
fi

echo "Cleanup completed"
EOF

    chmod +x cleanup.sh
    success "Cleanup script created: ./cleanup.sh"
}

# Display next steps
show_next_steps() {
    echo
    log "=== Setup Complete! ==="
    echo
    success "Local testing environment is ready!"
    echo
    echo "Next steps:"
    echo "1. Start the operator:"
    echo "   ${BLUE}make run${NC}"
    echo
    echo "2. In another terminal, test the APIs:"
    echo "   ${BLUE}./quick-test.sh${NC}"
    echo "   ${BLUE}./scripts/test-endpoints.sh${NC}"
    echo
    echo "3. Monitor the operator:"
    echo "   ${BLUE}./monitor-operator.sh${NC}"
    echo "   ${BLUE}./view-logs.sh${NC}"
    echo
    echo "4. Test WebSocket connection:"
    echo "   ${BLUE}wscat -c ws://localhost:8080/ws${NC}"
    echo
    echo "5. View test resources:"
    echo "   ${BLUE}kubectl get adotogitmigration,adodiscovery -n $NAMESPACE${NC}"
    echo
    echo "6. Clean up when done:"
    echo "   ${BLUE}./cleanup.sh${NC}"
    echo
    echo "API Base URL: ${BLUE}http://localhost:8080/api/v1${NC}"
    echo "Namespace: ${BLUE}$NAMESPACE${NC}"
    echo
    echo "For detailed API documentation, see: ${BLUE}docs/API_ENDPOINTS_REFERENCE.md${NC}"
}

# Main execution
main() {
    log "Setting up local testing environment for Azure DevOps to GitHub Migration Operator"
    
    check_prerequisites
    setup_dependencies
    create_namespace
    install_crds
    create_secrets
    build_operator
    create_test_resources
    setup_monitoring
    create_helpers
    show_next_steps
}

# Handle script interruption
cleanup_on_exit() {
    if [[ $? -ne 0 ]]; then
        error "Setup failed. Check the error messages above."
        echo "You can run this script again to retry the setup."
    fi
}

trap cleanup_on_exit EXIT

# Run main function
main "$@"