# Security Policy

## Reporting a Vulnerability

We take security vulnerabilities seriously. If you discover a security issue, please report it responsibly.

**Please do NOT report security vulnerabilities through public GitHub issues.**

Instead, please send an email to **majordomo@superset.com** with:

- A description of the vulnerability
- Steps to reproduce the issue
- Potential impact
- Any suggested fixes (optional)

You should receive a response within 48 hours. If the issue is confirmed, we will:

1. Work on a fix
2. Release a patched version
3. Credit you in the release notes (unless you prefer to remain anonymous)

## Supported Versions

| Version | Supported          |
| ------- | ------------------ |
| latest  | :white_check_mark: |

## Security Considerations

### API Keys

- The gateway proxies your upstream provider API keys (OpenAI, Anthropic, etc.)
- API keys are passed through in the `Authorization` header and are NOT logged
- The `X-Majordomo-Key` header is hashed (SHA256) before storage

### Data Storage

- Request/response bodies are optionally stored and may contain sensitive data
- Consider using S3 with encryption at rest for body storage
- PostgreSQL should be configured with appropriate access controls

### Network Security

- Deploy behind a reverse proxy (nginx, Caddy) with TLS termination
- Use network policies to restrict database access
- Consider rate limiting at the proxy level

### Recommendations

- Rotate upstream API keys regularly
- Use unique `X-Majordomo-Key` values per application/environment
- Enable PostgreSQL SSL connections in production
- Regularly update dependencies (`go get -u ./...`)
