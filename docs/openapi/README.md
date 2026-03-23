# OpenAPI Documentation

This directory contains the OpenAPI specification for the ADO to Git Migration Operator API.

## Overview

The OpenAPI specification describes all available API endpoints, request/response formats, and authentication requirements for the operator's HTTP API.

## Files

- `swagger.yaml`: The OpenAPI 3.0 specification file
- `swagger-ui/`: Directory containing the Swagger UI for interactive API documentation

## Accessing the API Documentation

When the operator is running, you can access the API documentation at:

```
http://<operator-address>/swagger/
```

This will display an interactive Swagger UI where you can:

1. Browse all available endpoints
2. See request and response schemas
3. Try out API calls directly from the browser
4. View detailed descriptions and examples

## Generating Client Libraries

You can use the OpenAPI specification to generate client libraries for various programming languages:

### Using OpenAPI Generator

```bash
# Install OpenAPI Generator
npm install @openapitools/openapi-generator-cli -g

# Generate a TypeScript client
openapi-generator-cli generate -i swagger.yaml -g typescript-fetch -o ./client-typescript

# Generate a Python client
openapi-generator-cli generate -i swagger.yaml -g python -o ./client-python

# Generate a Go client
openapi-generator-cli generate -i swagger.yaml -g go -o ./client-go
```

### Using Swagger Codegen

```bash
# Install Swagger Codegen
npm install swagger-codegen-cli -g

# Generate a Java client
swagger-codegen generate -i swagger.yaml -l java -o ./client-java
```

## Validating the Specification

You can validate the OpenAPI specification using various tools:

```bash
# Using Swagger CLI
npm install -g swagger-cli
swagger-cli validate swagger.yaml

# Using Spectral
npm install -g @stoplight/spectral
spectral lint swagger.yaml
```

## Contributing

When adding new API endpoints or modifying existing ones, please update the OpenAPI specification accordingly to keep the documentation in sync with the implementation.