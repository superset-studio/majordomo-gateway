FROM golang:1.25-alpine AS builder

WORKDIR /app

# Install ca-certificates for HTTPS requests
RUN apk add --no-cache ca-certificates

# Copy go mod files first for better caching
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build the binary
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-w -s" -o /app/majordomo-proxy ./cmd/majordomo

# Final stage
FROM alpine:3.19

WORKDIR /app

# Install ca-certificates for HTTPS requests to LLM providers
RUN apk add --no-cache ca-certificates tzdata

# Copy binary from builder
COPY --from=builder /app/majordomo-proxy /app/majordomo-proxy

# Copy default config files (majordomo.yaml is mounted at runtime via /etc/majordomo/)
COPY pricing.json /app/pricing.json
COPY model_aliases.json /app/model_aliases.json

# Create non-root user
RUN adduser -D -u 1000 majordomo
USER majordomo

EXPOSE 7680

ENTRYPOINT ["/app/majordomo-proxy"]
CMD ["serve"]
