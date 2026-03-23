#!/bin/bash

# Simple install script for Migration UI Dashboard

set -e

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Configuration
NAMESPACE=${NAMESPACE:-ado-migration-operator}
RELEASE_NAME=${RELEASE_NAME:-migration-dashboard}
VALUES_FILE=${VALUES_FILE:-values.yaml}

echo -e "${GREEN}=== Installing Migration UI Dashboard ===${NC}"
echo ""

# Check if helm is installed
if ! command -v helm &> /dev/null; then
    echo -e "${RED}Error: helm is not installed${NC}"
    echo "Install helm: https://helm.sh/docs/intro/install/"
    exit 1
fi

# Check if kubectl is installed
if ! command -v kubectl &> /dev/null; then
    echo -e "${RED}Error: kubectl is not installed${NC}"
    exit 1
fi

# Check if namespace exists
if ! kubectl get namespace "$NAMESPACE" &> /dev/null; then
    echo -e "${YELLOW}Namespace $NAMESPACE does not exist. Creating...${NC}"
    kubectl create namespace "$NAMESPACE"
fi

# Check if values file exists
if [ ! -f "$VALUES_FILE" ]; then
    echo -e "${RED}Error: Values file $VALUES_FILE not found${NC}"
    echo "Use values.yaml or create a custom values file"
    exit 1
fi

# Show what we're doing
echo "Release Name: $RELEASE_NAME"
echo "Namespace: $NAMESPACE"
echo "Values File: $VALUES_FILE"
echo ""

# Ask for confirmation
read -p "Continue with installation? (y/n) " -n 1 -r
echo
if [[ ! $REPLY =~ ^[Yy]$ ]]; then
    echo "Installation cancelled"
    exit 0
fi

# Install or upgrade
if helm list -n "$NAMESPACE" | grep -q "$RELEASE_NAME"; then
    echo -e "${YELLOW}Release $RELEASE_NAME already exists. Upgrading...${NC}"
    helm upgrade "$RELEASE_NAME" . \
        --namespace "$NAMESPACE" \
        --values "$VALUES_FILE" \
        --wait
else
    echo -e "${GREEN}Installing new release...${NC}"
    helm install "$RELEASE_NAME" . \
        --namespace "$NAMESPACE" \
        --values "$VALUES_FILE" \
        --wait
fi

# Show status
echo ""
echo -e "${GREEN}=== Installation Complete! ===${NC}"
echo ""
echo "Check status:"
echo "  kubectl get pods -n $NAMESPACE -l app.kubernetes.io/name=migration-ui-dashboard"
echo ""
echo "View logs:"
echo "  kubectl logs -n $NAMESPACE -l app.kubernetes.io/name=migration-ui-dashboard -f"
echo ""
echo "Port forward (for testing):"
echo "  kubectl port-forward -n $NAMESPACE svc/$RELEASE_NAME 8080:80"
echo "  Then open: http://localhost:8080"
echo ""
echo "Get service details:"
echo "  kubectl get svc -n $NAMESPACE $RELEASE_NAME"
echo ""
