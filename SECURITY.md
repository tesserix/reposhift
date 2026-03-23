# Security Policy

## Reporting a Vulnerability

If you discover a security vulnerability, please report it responsibly.

**Do NOT open a public issue for security vulnerabilities.**

Instead, email: **samyak.rout@gmail.com**

Please include:
- Description of the vulnerability
- Steps to reproduce
- Potential impact
- Suggested fix (if any)

We will acknowledge receipt within 48 hours and provide a timeline for a fix.

## Supported Versions

| Version | Supported |
|---------|-----------|
| 0.x     | Yes       |

## Security Best Practices

When self-hosting Reposhift:
- Always use TLS for ingress
- Rotate admin tokens and JWT secrets regularly
- Use Kubernetes RBAC to limit service account permissions
- Enable PostgreSQL SSL (`sslmode: require`)
- Store secrets in Kubernetes Secrets or use encrypted DB storage
- Keep container images updated
