# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added
- Initial release of Majordomo Gateway
- Multi-provider support (OpenAI, Anthropic, Google Gemini)
- Automatic cost calculation with pricing from llm-prices.com
- PostgreSQL storage for request logs
- S3 storage option for request/response bodies
- Custom metadata via `X-Majordomo-*` headers
- HyperLogLog-based cardinality estimation for metadata keys
- Automatic provider detection from request path
- Gzip compression support for responses
- Model alias mapping for pricing lookup
