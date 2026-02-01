package storage

import (
	"context"

	"github.com/superset-studio/majordomo-gateway/internal/models"
)

type Storage interface {
	WriteRequestLog(ctx context.Context, log *models.RequestLog)
	Close() error
}
