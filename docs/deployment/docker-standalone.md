# Docker (standalone)

Use this if you already have a PostgreSQL instance.

## Build the image

```bash
docker build -t majordomo-gateway .
```

## Run

```bash
docker run -p 7680:7680 \
  -e MAJORDOMO_STORAGE_POSTGRES_HOST=host.docker.internal \
  -e MAJORDOMO_STORAGE_POSTGRES_PORT=5432 \
  -e MAJORDOMO_STORAGE_POSTGRES_USER=majordomo \
  -e MAJORDOMO_STORAGE_POSTGRES_PASSWORD=secret \
  -e MAJORDOMO_STORAGE_POSTGRES_DATABASE=majordomo \
  majordomo-gateway
```

Make sure you've applied `schema.sql` to your database before starting.
