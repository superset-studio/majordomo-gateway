# Contributing to Majordomo Gateway

Thank you for your interest in contributing to Majordomo Gateway! This document provides guidelines and information for contributors.

## Getting Started

1. Fork the repository
2. Clone your fork: `git clone https://github.com/YOUR_USERNAME/majordomo-gateway.git`
3. Create a branch: `git checkout -b feature/your-feature-name`
4. Make your changes
5. Run tests: `make test`
6. Commit your changes: `git commit -m "Add your feature"`
7. Push to your fork: `git push origin feature/your-feature-name`
8. Open a Pull Request

## Development Setup

### Prerequisites

- Go 1.25+
- PostgreSQL 14+
- Docker (optional, for containerized development)

### Running Locally

```bash
# Install dependencies
go mod download

# Set up database
psql -U postgres -c "CREATE DATABASE majordomo;"
psql -U postgres -d majordomo -f schema.sql

# Copy and configure environment
cp .env.example .env
# Edit .env with your settings

# Run the server
make run
```

### Running Tests

```bash
# Run all tests
make test

# Run tests with coverage
make test-cover

# Run a specific test
go test -v ./internal/pricing -run TestCalculate
```

### Code Quality

Before submitting a PR, please ensure:

```bash
# Format code
make fmt

# Run linter
make lint

# Run vet
make vet
```

## Pull Request Guidelines

- Keep PRs focused on a single change
- Include tests for new functionality
- Update documentation if needed
- Follow existing code style
- Write clear commit messages

### Commit Message Format

Use clear, descriptive commit messages:

```
Add support for DeepSeek provider

- Implement DeepSeek API response parser
- Add pricing data for DeepSeek models
- Update provider detection logic
```

## Code Style

- Follow standard Go conventions
- Use `gofmt` for formatting
- Keep functions focused and small
- Add comments for non-obvious logic
- Use meaningful variable names

## Adding a New Provider

1. Create a new file in `internal/provider/` (e.g., `deepseek.go`)
2. Implement the response parser to extract token usage
3. Add provider detection logic in `provider.go`
4. Add pricing data to `pricing.json`
5. Add model aliases to `model_aliases.json` if needed
6. Add tests for the new provider
7. Update README.md with the new provider

## Reporting Issues

When reporting issues, please include:

- Go version (`go version`)
- Operating system
- Steps to reproduce
- Expected vs actual behavior
- Relevant logs or error messages

## Questions?

Feel free to open an issue for questions or discussions about potential changes.

## License

By contributing, you agree that your contributions will be licensed under the MIT License.
