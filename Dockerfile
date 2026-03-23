# Build the manager binary
FROM golang:1.26-alpine AS builder

WORKDIR /workspace

# Set build arguments
ARG VERSION=dev
ARG BUILD_DATE=unknown
ARG GIT_COMMIT=unknown

# Install necessary build tools
RUN apk add --no-cache git make

# Copy go-common package to preserve directory structure
COPY packages/go-common ./packages/go-common

# Copy ado-to-git-migration service to preserve directory structure
COPY micro-services/ado-to-git-migration ./micro-services/ado-to-git-migration

# Set working directory to the service
WORKDIR /workspace/micro-services/ado-to-git-migration

# Download dependencies (replace directive now works: ../../packages/go-common)
RUN go mod download

# Build the binary with version information
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
    -ldflags="-w -s -X main.version=${VERSION} -X main.buildDate=${BUILD_DATE} -X main.gitCommit=${GIT_COMMIT}" \
    -a -installsuffix cgo \
    -o manager \
    ./cmd/main.go

# Use Alpine as minimal base image with git support
FROM alpine:3.21

# Re-declare build arguments for labels
ARG VERSION=dev
ARG BUILD_DATE=unknown
ARG GIT_COMMIT=unknown

# Set metadata
LABEL org.opencontainers.image.title="ADO to Git Migration Operator"
LABEL org.opencontainers.image.description="Kubernetes operator for migrating Azure DevOps resources to GitHub"
LABEL org.opencontainers.image.version="${VERSION}"
LABEL org.opencontainers.image.created="${BUILD_DATE}"
LABEL org.opencontainers.image.revision="${GIT_COMMIT}"
LABEL org.opencontainers.image.licenses="Apache-2.0"

# Install git, ca-certificates, python3 (for git-filter-repo), and git-filter-repo
RUN apk add --no-cache git ca-certificates python3 && \
    wget -O /usr/local/bin/git-filter-repo \
    https://raw.githubusercontent.com/newren/git-filter-repo/main/git-filter-repo && \
    chmod +x /usr/local/bin/git-filter-repo

# Create nonroot user
RUN addgroup -g 65532 -S nonroot && adduser -u 65532 -S nonroot -G nonroot

WORKDIR /

# Copy the binary from builder
COPY --from=builder /workspace/micro-services/ado-to-git-migration/manager .

# Use nonroot user
USER 65532:65532

# Set environment variables
ENV LOG_LEVEL=info
ENV ENABLE_METRICS=true

# Expose ports
EXPOSE 8080 8081 9443

ENTRYPOINT ["/manager"]