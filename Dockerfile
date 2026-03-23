# Build the operator binary
FROM golang:1.26-alpine AS builder

WORKDIR /workspace

ARG VERSION=dev
ARG BUILD_DATE=unknown
ARG GIT_COMMIT=unknown

RUN apk add --no-cache git

# Cache dependencies
COPY go.mod go.sum ./
RUN go mod download

# Copy source
COPY . .

# Build the operator binary
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
    -ldflags="-w -s -X main.version=${VERSION} -X main.buildDate=${BUILD_DATE} -X main.gitCommit=${GIT_COMMIT}" \
    -a -installsuffix cgo \
    -o manager \
    cmd/main.go

# Runtime
FROM alpine:3.21

RUN apk add --no-cache ca-certificates tzdata git

COPY --from=builder /workspace/manager /manager

EXPOSE 8080 8081 8082

ENTRYPOINT ["/manager"]
