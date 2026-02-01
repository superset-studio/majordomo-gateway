package proxy

import (
	"bytes"
	"compress/gzip"
	"strings"
)

// CompressionLevel defines the gzip compression level to use
const CompressionLevel = gzip.DefaultCompression

// MinCompressionSize is the minimum response size (in bytes) to consider for compression
// Responses smaller than this won't benefit much from compression
const MinCompressionSize = 1024

// AcceptsGzip checks if the client accepts gzip encoding
func AcceptsGzip(acceptEncoding string) bool {
	for _, encoding := range strings.Split(acceptEncoding, ",") {
		encoding = strings.TrimSpace(encoding)
		// Handle quality values like "gzip;q=0.8"
		if strings.HasPrefix(encoding, "gzip") {
			return true
		}
	}
	return false
}

// GzipCompress compresses the given data using gzip
func GzipCompress(data []byte) ([]byte, error) {
	var buf bytes.Buffer
	writer, err := gzip.NewWriterLevel(&buf, CompressionLevel)
	if err != nil {
		return nil, err
	}

	if _, err := writer.Write(data); err != nil {
		writer.Close()
		return nil, err
	}

	if err := writer.Close(); err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}

// ShouldCompress determines if the response should be compressed based on:
// - Client accepts gzip
// - Response is large enough to benefit from compression
// - Content-Type is compressible (text, JSON, etc.)
func ShouldCompress(acceptEncoding string, contentType string, bodySize int) bool {
	if !AcceptsGzip(acceptEncoding) {
		return false
	}

	if bodySize < MinCompressionSize {
		return false
	}

	// Check if content type is compressible
	return isCompressibleContentType(contentType)
}

// isCompressibleContentType checks if the content type benefits from compression
func isCompressibleContentType(contentType string) bool {
	contentType = strings.ToLower(contentType)

	compressibleTypes := []string{
		"text/",
		"application/json",
		"application/xml",
		"application/javascript",
		"application/x-javascript",
		"application/ld+json",
		"application/manifest+json",
		"application/vnd.api+json",
	}

	for _, ct := range compressibleTypes {
		if strings.Contains(contentType, ct) {
			return true
		}
	}

	return false
}
