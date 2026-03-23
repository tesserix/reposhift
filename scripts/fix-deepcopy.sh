#!/bin/bash

# Fix the DeepCopy methods for Kubernetes Custom Resources

echo "Fixing DeepCopy methods for Kubernetes Custom Resources..."

# First, make sure controller-gen is installed
if ! command -v controller-gen &> /dev/null; then
    echo "Installing controller-gen..."
    go install sigs.k8s.io/controller-tools/cmd/controller-gen@latest
fi

# Generate the deepcopy methods
echo "Generating DeepCopy methods..."
controller-gen object:headerFile="hack/boilerplate.go.txt" paths="./api/..."

# If the boilerplate file doesn't exist, create it
if [ ! -f "hack/boilerplate.go.txt" ]; then
    echo "Creating boilerplate file..."
    mkdir -p hack
    cat > hack/boilerplate.go.txt << 'EOF'
/*
Copyright 2024.

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
EOF
    
    # Run controller-gen again with the boilerplate file
    controller-gen object:headerFile="hack/boilerplate.go.txt" paths="./api/..."
fi

# Also generate CRDs while we're at it
echo "Generating CRDs..."
controller-gen crd:trivialVersions=true rbac:roleName=manager-role webhook paths="./..." output:crd:artifacts:config=config/crd/bases

echo "Done! DeepCopy methods have been generated."
echo "Now try running 'go run cmd/main.go' again."