#!/bin/bash
# trigger-reconciliation.sh
# Manually trigger reconciliation for an AdoToGitMigration resource
# This adds the reconcile-trigger annotation to force the operator to reprocess the migration

set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Default values
NAMESPACE="ado-migration-operator"
MIGRATION_NAME=""
TRIGGER_VALUE=""

# Usage function
usage() {
    cat <<EOF
${BLUE}Usage:${NC} $0 -n <migration-name> [-ns <namespace>] [-t <trigger-value>]

${YELLOW}Description:${NC}
  Manually triggers reconciliation for an AdoToGitMigration resource by adding
  the 'migration.ado-to-git-migration.io/reconcile-trigger' annotation.

${YELLOW}Options:${NC}
  -n, --name <migration-name>    Name of the migration resource (required)
  -ns, --namespace <namespace>   Namespace (default: ado-migration-operator)
  -t, --trigger <value>          Custom trigger value (default: auto-generated)
  -h, --help                     Show this help message

${YELLOW}Examples:${NC}
  # Trigger reconciliation for localgov-altitude-repo migration
  $0 -n localgov-altitude-repo

  # Trigger with custom namespace
  $0 -n my-migration -ns custom-namespace

  # Trigger with custom trigger value
  $0 -n my-migration -t "manual-trigger-$(date +%s)"

${YELLOW}What this does:${NC}
  1. Checks if the migration exists
  2. Adds the reconcile-trigger annotation with a unique value
  3. The operator detects the annotation and immediately starts reconciliation
  4. If the migration is in a terminal phase (Completed/Failed), it restarts the migration

${YELLOW}Production-Ready Features:${NC}
  ✓ Validates migration exists before applying changes
  ✓ Shows current migration status before triggering
  ✓ Generates unique trigger values to ensure operator detection
  ✓ Provides detailed output and error handling
  ✓ Safe for terminal migrations (Completed/Failed/Cancelled)

EOF
    exit 1
}

# Parse arguments
while [[ $# -gt 0 ]]; do
    case $1 in
        -n|--name)
            MIGRATION_NAME="$2"
            shift 2
            ;;
        -ns|--namespace)
            NAMESPACE="$2"
            shift 2
            ;;
        -t|--trigger)
            TRIGGER_VALUE="$2"
            shift 2
            ;;
        -h|--help)
            usage
            ;;
        *)
            echo -e "${RED}Error: Unknown option $1${NC}"
            usage
            ;;
    esac
done

# Validate required arguments
if [ -z "$MIGRATION_NAME" ]; then
    echo -e "${RED}Error: Migration name is required${NC}"
    usage
fi

# Generate trigger value if not provided
if [ -z "$TRIGGER_VALUE" ]; then
    TRIGGER_VALUE="manual-trigger-$(date +%s)"
fi

echo -e "${BLUE}═══════════════════════════════════════════════════════${NC}"
echo -e "${BLUE}  AdoToGitMigration Reconciliation Trigger${NC}"
echo -e "${BLUE}═══════════════════════════════════════════════════════${NC}"
echo ""
echo -e "${YELLOW}Migration:${NC}     $MIGRATION_NAME"
echo -e "${YELLOW}Namespace:${NC}     $NAMESPACE"
echo -e "${YELLOW}Trigger Value:${NC} $TRIGGER_VALUE"
echo ""

# Check if migration exists
echo -e "${BLUE}Checking if migration exists...${NC}"
if ! kubectl get adotogitmigration "$MIGRATION_NAME" -n "$NAMESPACE" &>/dev/null; then
    echo -e "${RED}✗ Migration '$MIGRATION_NAME' not found in namespace '$NAMESPACE'${NC}"
    echo ""
    echo -e "${YELLOW}Available migrations:${NC}"
    kubectl get adotogitmigrations -n "$NAMESPACE" 2>/dev/null || echo "  No migrations found"
    exit 1
fi

echo -e "${GREEN}✓ Migration found${NC}"
echo ""

# Show current status
echo -e "${BLUE}Current Migration Status:${NC}"
kubectl get adotogitmigration "$MIGRATION_NAME" -n "$NAMESPACE" -o json | \
    jq -r '"  Phase:       " + (.status.phase // "Unknown") + "\n" +
           "  Progress:    " + (.status.progress.progressSummary // "N/A") + "\n" +
           "  Last Update: " + (.status.lastReconcileTime // "Never")'
echo ""

# Ask for confirmation
read -p "$(echo -e ${YELLOW}Do you want to trigger reconciliation? [y/N]:${NC} )" -n 1 -r
echo ""
if [[ ! $REPLY =~ ^[Yy]$ ]]; then
    echo -e "${YELLOW}Cancelled by user${NC}"
    exit 0
fi

# Add the annotation
echo -e "${BLUE}Adding reconcile-trigger annotation...${NC}"
if kubectl annotate adotogitmigration "$MIGRATION_NAME" -n "$NAMESPACE" \
    "migration.ado-to-git-migration.io/reconcile-trigger=$TRIGGER_VALUE" \
    --overwrite; then
    echo -e "${GREEN}✓ Annotation added successfully${NC}"
else
    echo -e "${RED}✗ Failed to add annotation${NC}"
    exit 1
fi

echo ""
echo -e "${BLUE}═══════════════════════════════════════════════════════${NC}"
echo -e "${GREEN}✓ Reconciliation triggered successfully!${NC}"
echo -e "${BLUE}═══════════════════════════════════════════════════════${NC}"
echo ""
echo -e "${YELLOW}What happens next:${NC}"
echo "  1. Operator detects the annotation change"
echo "  2. Reconciliation loop is triggered immediately"
echo "  3. If migration is in terminal phase, it restarts from scratch"
echo "  4. New repositories are processed, status is updated"
echo ""
echo -e "${YELLOW}Monitor progress with:${NC}"
echo "  kubectl get adotogitmigration $MIGRATION_NAME -n $NAMESPACE -w"
echo ""
echo -e "${YELLOW}View detailed status with:${NC}"
echo "  kubectl get adotogitmigration $MIGRATION_NAME -n $NAMESPACE -o yaml"
echo ""
echo -e "${YELLOW}Check operator logs with:${NC}"
echo "  kubectl logs -f -n $NAMESPACE -l app.kubernetes.io/name=ado-git-migration --tail=50"
echo ""
