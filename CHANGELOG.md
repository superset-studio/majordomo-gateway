# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added
- Google Cloud Storage (GCS) as a body storage backend (`logging.body_storage: "gcs"`)
- `BodyStorage` interface for pluggable body storage backends
- `body_storage_key` column in `llm_requests` (backend-agnostic, replaces `body_s3_key` for new integrations)
- GCS config section (`gcs.enabled`, `gcs.bucket`, `gcs.credentials_file`, `gcs.endpoint`)
- Guard preventing both S3 and GCS from being enabled simultaneously

### Changed
- Body storage types in `internal/storage` renamed from `S3BodyContent`/`S3RequestContent`/`S3ResponseContent` to `BodyContent`/`RequestContent`/`ResponseContent`
- Proxy handler now accepts a `BodyStorage` interface instead of a concrete `*S3BodyStorage`
- S3 body storage continues to write `body_s3_key` for backward compatibility

## [0.1.0]

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
