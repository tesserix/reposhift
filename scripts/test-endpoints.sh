#!/bin/bash

# Test script for Azure DevOps to GitHub Migration Operator API endpoints
# Usage: ./test-endpoints.sh [base-url] [namespace]

set -e

# Configuration
BASE_URL="${1:-http://localhost:8080/api/v1}"
NAMESPACE="${2:-ado-git-migration}"
TEST_OUTPUT_DIR="./test-results"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Create output directory
mkdir -p "$TEST_OUTPUT_DIR"

# Logging function
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

# Test function
test_endpoint() {
    local method="$1"
    local endpoint="$2"
    local description="$3"
    local data="$4"
    local expected_status="${5:-200}"
    
    log "Testing: $description"
    
    local curl_cmd="curl -s -w '%{http_code}' -X $method"
    
    if [[ -n "$data" ]]; then
        curl_cmd="$curl_cmd -H 'Content-Type: application/json' -d '$data'"
    fi
    
    local response
    local status_code
    
    if [[ -n "$data" ]]; then
        response=$(curl -s -w '\n%{http_code}' -X "$method" -H 'Content-Type: application/json' -d "$data" "$BASE_URL$endpoint")
    else
        response=$(curl -s -w '\n%{http_code}' -X "$method" "$BASE_URL$endpoint")
    fi
    
    status_code=$(echo "$response" | tail -n1)
    response_body=$(echo "$response" | head -n -1)
    
    # Save response to file
    local test_name=$(echo "$description" | tr ' ' '_' | tr '[:upper:]' '[:lower:]')
    echo "$response_body" > "$TEST_OUTPUT_DIR/${test_name}_response.json"
    
    if [[ "$status_code" == "$expected_status" ]]; then
        success "$description - Status: $status_code"
        if command -v jq &> /dev/null && [[ -n "$response_body" ]]; then
            echo "$response_body" | jq . > "$TEST_OUTPUT_DIR/${test_name}_formatted.json" 2>/dev/null || true
        fi
    else
        error "$description - Expected: $expected_status, Got: $status_code"
        echo "Response: $response_body"
    fi
    
    echo ""
}

# Test health endpoints
test_health_endpoints() {
    log "=== Testing Health Endpoints ==="
    
    test_endpoint "GET" "/utils/health" "Health Check"
    test_endpoint "GET" "/utils/ready" "Readiness Check"
    test_endpoint "GET" "/utils/version" "Version Info"
}

# Test discovery endpoints (with mock credentials)
test_discovery_endpoints() {
    log "=== Testing Discovery Endpoints ==="
    
    local mock_params="client_id=test-client&client_secret=test-secret&tenant_id=test-tenant"
    
    test_endpoint "GET" "/discovery/organizations?$mock_params" "Discover Organizations" "" "200"
    test_endpoint "GET" "/discovery/projects?organization=TestOrg&$mock_params" "Discover Projects" "" "200"
    test_endpoint "GET" "/discovery/repositories?organization=TestOrg&project=TestProject&$mock_params" "Discover Repositories" "" "200"
    test_endpoint "GET" "/discovery/workitems?organization=TestOrg&project=TestProject&$mock_params" "Discover Work Items" "" "200"
    test_endpoint "GET" "/discovery/pipelines?organization=TestOrg&project=TestProject&$mock_params" "Discover Pipelines" "" "200"
    test_endpoint "GET" "/discovery/builds?organization=TestOrg&project=TestProject&$mock_params" "Discover Builds" "" "200"
    test_endpoint "GET" "/discovery/releases?organization=TestOrg&project=TestProject&$mock_params" "Discover Releases" "" "200"
    test_endpoint "GET" "/discovery/teams?organization=TestOrg&project=TestProject&$mock_params" "Discover Teams" "" "200"
    test_endpoint "GET" "/discovery/users?organization=TestOrg&$mock_params" "Discover Users" "" "200"
}

# Test migration endpoints
test_migration_endpoints() {
    log "=== Testing Migration Endpoints ==="
    
    test_endpoint "GET" "/migrations?namespace=$NAMESPACE" "List Migrations"
    
    # Create a test migration
    local migration_data='{
        "metadata": {
            "name": "test-migration-api",
            "namespace": "'$NAMESPACE'"
        },
        "spec": {
            "type": "repository",
            "source": {
                "organization": "TestOrg",
                "project": "TestProject",
                "auth": {
                    "servicePrincipal": {
                        "clientId": "test-client",
                        "tenantId": "test-tenant",
                        "clientSecretRef": {
                            "name": "azure-sp-secret",
                            "key": "client-secret"
                        }
                    }
                }
            },
            "target": {
                "owner": "test-github-org",
                "auth": {
                    "tokenRef": {
                        "name": "github-token-secret",
                        "key": "token"
                    }
                }
            },
            "settings": {
                "maxHistoryDays": 500,
                "maxCommitCount": 2000
            },
            "resources": [
                {
                    "type": "repository",
                    "sourceId": "test-repo-id",
                    "sourceName": "test-repo",
                    "targetName": "migrated-test-repo"
                }
            ]
        }
    }'
    
    test_endpoint "POST" "/migrations" "Create Migration" "$migration_data" "201"
    test_endpoint "GET" "/migrations/test-migration-api?namespace=$NAMESPACE" "Get Migration" "" "200"
    test_endpoint "GET" "/migrations/test-migration-api/status?namespace=$NAMESPACE" "Get Migration Status" "" "200"
    test_endpoint "GET" "/migrations/test-migration-api/progress?namespace=$NAMESPACE" "Get Migration Progress" "" "200"
    test_endpoint "POST" "/migrations/test-migration-api/validate?namespace=$NAMESPACE" "Validate Migration" "" "200"
}

# Test validation endpoints
test_validation_endpoints() {
    log "=== Testing Validation Endpoints ==="
    
    local ado_creds='{
        "clientId": "test-client-id",
        "clientSecret": "test-client-secret",
        "tenantId": "test-tenant-id",
        "organization": "TestOrg"
    }'
    
    local github_creds='{
        "token": "test-github-token",
        "owner": "test-github-org"
    }'
    
    test_endpoint "POST" "/validation/credentials/ado" "Validate Azure DevOps Credentials" "$ado_creds" "200"
    test_endpoint "POST" "/validation/credentials/github" "Validate GitHub Credentials" "$github_creds" "200"
}

# Test statistics endpoints
test_statistics_endpoints() {
    log "=== Testing Statistics Endpoints ==="
    
    test_endpoint "GET" "/statistics/migrations?namespace=$NAMESPACE" "Get Migration Statistics"
    test_endpoint "GET" "/statistics/performance" "Get Performance Metrics"
    test_endpoint "GET" "/statistics/usage" "Get Usage Statistics"
}

# Test pipeline endpoints
test_pipeline_endpoints() {
    log "=== Testing Pipeline Endpoints ==="
    
    test_endpoint "GET" "/pipelines?namespace=$NAMESPACE" "List Pipeline Conversions"
    
    local pipeline_data='{
        "metadata": {
            "name": "test-pipeline-conversion",
            "namespace": "'$NAMESPACE'"
        },
        "spec": {
            "type": "build",
            "source": {
                "organization": "TestOrg",
                "project": "TestProject",
                "auth": {
                    "servicePrincipal": {
                        "clientId": "test-client",
                        "tenantId": "test-tenant",
                        "clientSecretRef": {
                            "name": "azure-sp-secret",
                            "key": "client-secret"
                        }
                    }
                }
            },
            "target": {
                "owner": "test-github-org",
                "repository": "test-repo",
                "auth": {
                    "tokenRef": {
                        "name": "github-token-secret",
                        "key": "token"
                    }
                }
            },
            "pipelines": [
                {
                    "id": "1",
                    "name": "Test Pipeline",
                    "type": "build",
                    "targetWorkflowName": "test.yml"
                }
            ]
        }
    }'
    
    test_endpoint "POST" "/pipelines" "Create Pipeline Conversion" "$pipeline_data" "201"
    test_endpoint "GET" "/pipelines/test-pipeline-conversion?namespace=$NAMESPACE" "Get Pipeline Conversion" "" "200"
    test_endpoint "GET" "/pipelines/test-pipeline-conversion/preview?namespace=$NAMESPACE" "Preview Pipeline Conversion" "" "200"
}

# Test WebSocket endpoint (basic connectivity test)
test_websocket_endpoint() {
    log "=== Testing WebSocket Endpoint ==="
    
    if command -v wscat &> /dev/null; then
        log "Testing WebSocket connection with wscat..."
        timeout 5s wscat -c "ws://localhost:8080/ws" -x '{"type":"ping"}' > "$TEST_OUTPUT_DIR/websocket_test.log" 2>&1 || true
        if [[ -s "$TEST_OUTPUT_DIR/websocket_test.log" ]]; then
            success "WebSocket connection test completed"
        else
            warning "WebSocket test may have failed - check logs"
        fi
    else
        warning "wscat not found - skipping WebSocket test (install with: npm install -g wscat)"
    fi
}

# Test error handling
test_error_handling() {
    log "=== Testing Error Handling ==="
    
    test_endpoint "GET" "/nonexistent" "Non-existent Endpoint" "" "404"
    test_endpoint "POST" "/migrations" "Invalid Migration Data" '{"invalid": "data"}' "400"
    test_endpoint "GET" "/migrations/nonexistent?namespace=$NAMESPACE" "Non-existent Migration" "" "404"
}

# Performance test
performance_test() {
    log "=== Running Performance Test ==="
    
    local start_time=$(date +%s)
    
    for i in {1..10}; do
        curl -s "$BASE_URL/utils/health" > /dev/null
    done
    
    local end_time=$(date +%s)
    local duration=$((end_time - start_time))
    
    log "Performance test completed: 10 requests in ${duration}s"
    echo "Average response time: $((duration * 100))ms" > "$TEST_OUTPUT_DIR/performance_test.log"
}

# Cleanup function
cleanup() {
    log "=== Cleaning Up Test Resources ==="
    
    # Delete test migration if it exists
    curl -s -X DELETE "$BASE_URL/migrations/test-migration-api?namespace=$NAMESPACE" > /dev/null 2>&1 || true
    
    # Delete test pipeline conversion if it exists
    curl -s -X DELETE "$BASE_URL/pipelines/test-pipeline-conversion?namespace=$NAMESPACE" > /dev/null 2>&1 || true
    
    success "Cleanup completed"
}

# Generate test report
generate_report() {
    log "=== Generating Test Report ==="
    
    local report_file="$TEST_OUTPUT_DIR/test_report.html"
    
    cat > "$report_file" << EOF
<!DOCTYPE html>
<html>
<head>
    <title>API Test Report</title>
    <style>
        body { font-family: Arial, sans-serif; margin: 20px; }
        .header { background-color: #f0f0f0; padding: 10px; border-radius: 5px; }
        .success { color: green; }
        .error { color: red; }
        .warning { color: orange; }
        pre { background-color: #f5f5f5; padding: 10px; border-radius: 3px; overflow-x: auto; }
    </style>
</head>
<body>
    <div class="header">
        <h1>Azure DevOps to GitHub Migration Operator - API Test Report</h1>
        <p>Generated on: $(date)</p>
        <p>Base URL: $BASE_URL</p>
        <p>Namespace: $NAMESPACE</p>
    </div>
    
    <h2>Test Results</h2>
    <p>Test results and response files are available in the <code>$TEST_OUTPUT_DIR</code> directory.</p>
    
    <h3>Available Response Files:</h3>
    <ul>
EOF

    for file in "$TEST_OUTPUT_DIR"/*.json; do
        if [[ -f "$file" ]]; then
            local filename=$(basename "$file")
            echo "        <li><a href=\"$filename\">$filename</a></li>" >> "$report_file"
        fi
    done

    cat >> "$report_file" << EOF
    </ul>
    
    <h3>Performance Results:</h3>
    <pre>$(cat "$TEST_OUTPUT_DIR/performance_test.log" 2>/dev/null || echo "Performance test not run")</pre>
    
    <h3>WebSocket Test:</h3>
    <pre>$(cat "$TEST_OUTPUT_DIR/websocket_test.log" 2>/dev/null || echo "WebSocket test not run")</pre>
</body>
</html>
EOF

    success "Test report generated: $report_file"
}

# Main execution
main() {
    log "Starting API endpoint tests for Azure DevOps to GitHub Migration Operator"
    log "Base URL: $BASE_URL"
    log "Namespace: $NAMESPACE"
    log "Output Directory: $TEST_OUTPUT_DIR"
    
    # Run all tests
    test_health_endpoints
    test_discovery_endpoints
    test_migration_endpoints
    test_validation_endpoints
    test_statistics_endpoints
    test_pipeline_endpoints
    test_websocket_endpoint
    test_error_handling
    performance_test
    
    # Cleanup and report
    cleanup
    generate_report
    
    success "All tests completed! Check $TEST_OUTPUT_DIR for detailed results."
}

# Handle script interruption
trap cleanup EXIT

# Check if operator is running
if ! curl -s "$BASE_URL/utils/health" > /dev/null 2>&1; then
    error "Operator is not running or not accessible at $BASE_URL"
    error "Please ensure the operator is running with: make run"
    exit 1
fi

# Run main function
main "$@"