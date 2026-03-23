#!/bin/bash

# Script to deploy ADO to Git Migration Operator to AKS
# Usage: ./deploy-to-aks.sh [environment]
# Environment can be: dev, staging, prod (default: dev)

set -e

# Default values
ENVIRONMENT=${1:-dev}
NAMESPACE="ado-git-migration-system"
MIGRATION_NAMESPACE="ado-git-migration"

# Color codes for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Log functions
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

# Check if required tools are installed
check_requirements() {
    log "Checking requirements..."
    
    if ! command -v az &> /dev/null; then
        error "Azure CLI is not installed. Please install it first."
        exit 1
    fi
    
    if ! command -v kubectl &> /dev/null; then
        error "kubectl is not installed. Please install it first."
        exit 1
    fi
    
    if ! command -v docker &> /dev/null; then
        error "Docker is not installed. Please install it first."
        exit 1
    fi
    
    success "All required tools are installed."
}

# Login to Azure and set up environment
setup_azure() {
    log "Setting up Azure environment..."
    
    # Check if already logged in
    if ! az account show &> /dev/null; then
        log "Logging in to Azure..."
        az login
    fi
    
    # Prompt for subscription
    echo "Available subscriptions:"
    az account list --query "[].{Name:name, ID:id}" -o table
    
    read -p "Enter subscription ID or name to use: " SUBSCRIPTION
    az account set --subscription "$SUBSCRIPTION"
    
    success "Using subscription: $(az account show --query name -o tsv)"
    
    # Prompt for resource group and AKS cluster
    read -p "Enter resource group name: " RESOURCE_GROUP
    read -p "Enter AKS cluster name: " AKS_CLUSTER_NAME
    read -p "Enter Azure Container Registry name: " ACR_NAME
    
    # Get AKS credentials
    log "Getting AKS credentials..."
    az aks get-credentials --resource-group "$RESOURCE_GROUP" --name "$AKS_CLUSTER_NAME" --overwrite-existing
    
    # Verify connection
    if ! kubectl cluster-info &> /dev/null; then
        error "Failed to connect to AKS cluster."
        exit 1
    fi
    
    success "Connected to AKS cluster: $AKS_CLUSTER_NAME"
    
    # Login to ACR
    log "Logging in to Azure Container Registry..."
    az acr login --name "$ACR_NAME"
    
    # Set environment variables
    REGISTRY_URL="$ACR_NAME.azurecr.io"
    IMAGE_NAME="ado-git-migration-operator"
    
    # Set image tag based on environment
    case "$ENVIRONMENT" in
        dev)
            IMAGE_TAG="dev-$(date +%Y%m%d%H%M)"
            ;;
        staging)
            IMAGE_TAG="staging-$(date +%Y%m%d)"
            ;;
        prod)
            # For production, prompt for a specific version
            read -p "Enter production version (e.g., v1.0.0): " VERSION
            IMAGE_TAG="$VERSION"
            ;;
        *)
            error "Unknown environment: $ENVIRONMENT"
            exit 1
            ;;
    esac
    
    export REGISTRY_URL IMAGE_NAME IMAGE_TAG
    
    success "Environment setup complete."
    log "Registry: $REGISTRY_URL"
    log "Image: $IMAGE_NAME:$IMAGE_TAG"
}

# Build and push the operator image
build_and_push_image() {
    log "Building and pushing operator image..."
    
    # Navigate to project root
    cd "$(dirname "$0")/.."
    
    # Build the image
    log "Building Docker image: $REGISTRY_URL/$IMAGE_NAME:$IMAGE_TAG"
    make docker-build IMG="$REGISTRY_URL/$IMAGE_NAME:$IMAGE_TAG"
    
    # Push the image
    log "Pushing Docker image to ACR..."
    make docker-push IMG="$REGISTRY_URL/$IMAGE_NAME:$IMAGE_TAG"
    
    success "Image built and pushed successfully."
}

# Install CRDs
install_crds() {
    log "Installing Custom Resource Definitions..."
    
    # Generate and install CRDs
    make manifests
    make install
    
    # Verify CRDs
    if kubectl get crd adotogitmigrations.migration.ado-to-git-migration.io &> /dev/null; then
        success "CRDs installed successfully."
    else
        error "Failed to install CRDs."
        exit 1
    fi
}

# Create namespaces
create_namespaces() {
    log "Creating namespaces..."
    
    # Create operator namespace
    if ! kubectl get namespace "$NAMESPACE" &> /dev/null; then
        kubectl create namespace "$NAMESPACE"
        success "Created namespace: $NAMESPACE"
    else
        warning "Namespace already exists: $NAMESPACE"
    fi
    
    # Create migration resources namespace
    if ! kubectl get namespace "$MIGRATION_NAMESPACE" &> /dev/null; then
        kubectl create namespace "$MIGRATION_NAMESPACE"
        success "Created namespace: $MIGRATION_NAMESPACE"
    else
        warning "Namespace already exists: $MIGRATION_NAMESPACE"
    fi
}

# Create authentication secrets
create_secrets() {
    log "Creating authentication secrets..."
    
    # Prompt for Azure Service Principal details
    read -p "Enter Azure Service Principal Client ID: " CLIENT_ID
    read -s -p "Enter Azure Service Principal Client Secret: " CLIENT_SECRET
    echo
    read -p "Enter Azure Service Principal Tenant ID: " TENANT_ID
    
    # Create Azure Service Principal secret
    kubectl create secret generic azure-sp-secret \
        --namespace="$MIGRATION_NAMESPACE" \
        --from-literal=client-id="$CLIENT_ID" \
        --from-literal=client-secret="$CLIENT_SECRET" \
        --from-literal=tenant-id="$TENANT_ID" \
        --dry-run=client -o yaml | kubectl apply -f -
    
    success "Created Azure Service Principal secret."
    
    # Prompt for GitHub token
    read -s -p "Enter GitHub Personal Access Token: " GITHUB_TOKEN
    echo
    
    # Create GitHub token secret
    kubectl create secret generic github-token-secret \
        --namespace="$MIGRATION_NAMESPACE" \
        --from-literal=token="$GITHUB_TOKEN" \
        --dry-run=client -o yaml | kubectl apply -f -
    
    success "Created GitHub token secret."
}

# Update and apply Kubernetes manifests
deploy_operator() {
    log "Deploying operator to AKS..."
    
    # Navigate to manifests directory
    cd "$(dirname "$0")/../manifests/kubernetes"
    
    # Update image references
    log "Updating image references..."
    sed -i "s|\${REGISTRY_URL}|$REGISTRY_URL|g" deployment.yaml
    sed -i "s|\${IMAGE_TAG}|$IMAGE_TAG|g" deployment.yaml
    
    # Apply manifests based on environment
    if [ "$ENVIRONMENT" == "prod" ]; then
        log "Deploying production configuration..."
        cd production
        sed -i "s|\${REGISTRY_URL}|$REGISTRY_URL|g" kustomization.yaml
        sed -i "s|\${IMAGE_TAG}|$IMAGE_TAG|g" kustomization.yaml
        kubectl apply -k .
    else
        log "Deploying standard configuration..."
        kubectl apply -k .
    fi
    
    # Wait for deployment to be ready
    log "Waiting for deployment to be ready..."
    kubectl rollout status deployment/ado-git-migration-controller-manager -n "$NAMESPACE" --timeout=300s
    
    success "Operator deployed successfully."
}

# Configure ingress (optional)
configure_ingress() {
    log "Configuring ingress..."
    
    # Prompt for domain name
    read -p "Enter domain name for the API (leave empty to skip ingress): " DOMAIN_NAME
    
    if [ -z "$DOMAIN_NAME" ]; then
        warning "Skipping ingress configuration."
        return
    fi
    
    # Update ingress manifest
    sed -i "s|ado-migration.example.com|$DOMAIN_NAME|g" ingress.yaml
    
    # Check if NGINX ingress controller is installed
    if ! kubectl get deployment -n ingress-nginx -l app.kubernetes.io/name=ingress-nginx &> /dev/null; then
        warning "NGINX ingress controller not found. Installing..."
        
        # Install NGINX ingress controller
        helm repo add ingress-nginx https://kubernetes.github.io/ingress-nginx
        helm repo update
        helm install nginx-ingress ingress-nginx/ingress-nginx \
            --namespace ingress-nginx \
            --create-namespace
    fi
    
    # Apply ingress
    kubectl apply -f ingress.yaml
    
    # Get ingress IP
    log "Waiting for ingress to get an IP address..."
    sleep 10
    INGRESS_IP=$(kubectl get ingress ado-git-migration-api -n "$NAMESPACE" -o jsonpath='{.status.loadBalancer.ingress[0].ip}')
    
    if [ -n "$INGRESS_IP" ]; then
        success "Ingress configured successfully."
        log "API will be available at: https://$DOMAIN_NAME"
        log "Add the following DNS record: $DOMAIN_NAME -> $INGRESS_IP"
    else
        warning "Ingress IP not yet available. Check status with: kubectl get ingress -n $NAMESPACE"
    fi
}

# Verify deployment
verify_deployment() {
    log "Verifying deployment..."
    
    # Check if pods are running
    PODS_RUNNING=$(kubectl get pods -n "$NAMESPACE" -l app.kubernetes.io/name=ado-git-migration -o jsonpath='{.items[*].status.phase}' | tr ' ' '\n' | grep -c "Running" || echo "0")
    
    if [ "$PODS_RUNNING" -gt 0 ]; then
        success "Operator pods are running."
    else
        error "No running operator pods found."
        exit 1
    fi
    
    # Check if service is available
    if kubectl get service ado-git-migration-controller-manager -n "$NAMESPACE" &> /dev/null; then
        success "Operator service is available."
    else
        error "Operator service not found."
        exit 1
    fi
    
    # Port-forward to test API
    log "Testing API health endpoint..."
    kubectl port-forward svc/ado-git-migration-controller-manager -n "$NAMESPACE" 8080:8080 &
    PORT_FORWARD_PID=$!
    
    # Wait for port-forward to establish
    sleep 5
    
    # Test health endpoint
    if curl -s http://localhost:8080/api/v1/utils/health | grep -q "healthy"; then
        success "API health check passed."
    else
        warning "API health check failed. Check logs for details."
    fi
    
    # Kill port-forward
    kill $PORT_FORWARD_PID
    
    log "Deployment verification complete."
}

# Display next steps
show_next_steps() {
    echo
    log "=== Deployment Complete! ==="
    echo
    echo "The ADO to Git Migration Operator has been deployed to your AKS cluster."
    echo
    echo "Next steps:"
    echo "1. Create migration resources:"
    echo "   kubectl apply -f examples/migration-example.yaml"
    echo
    echo "2. Monitor migrations:"
    echo "   kubectl get adotogitmigration -n $MIGRATION_NAMESPACE"
    echo "   kubectl describe adotogitmigration <name> -n $MIGRATION_NAMESPACE"
    echo
    echo "3. Access the API:"
    if [ -n "$DOMAIN_NAME" ]; then
        echo "   https://$DOMAIN_NAME/api/v1/utils/health"
    else
        echo "   kubectl port-forward svc/ado-git-migration-controller-manager -n $NAMESPACE 8080:8080"
        echo "   Then visit: http://localhost:8080/api/v1/utils/health"
    fi
    echo
    echo "4. View operator logs:"
    echo "   kubectl logs -f deployment/ado-git-migration-controller-manager -n $NAMESPACE"
    echo
    echo "For more information, refer to the documentation:"
    echo "- API Reference: docs/API_ENDPOINTS_REFERENCE.md"
    echo "- Discovery API: docs/AZURE_DEVOPS_DISCOVERY_API.md"
    echo "- Local Testing: docs/LOCAL_TESTING_GUIDE.md"
    echo
}

# Main function
main() {
    log "Starting deployment of ADO to Git Migration Operator to AKS..."
    log "Environment: $ENVIRONMENT"
    
    check_requirements
    setup_azure
    build_and_push_image
    install_crds
    create_namespaces
    create_secrets
    deploy_operator
    configure_ingress
    verify_deployment
    show_next_steps
}

# Run main function
main