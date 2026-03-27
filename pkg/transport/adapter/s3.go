package adapter

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

const (
	envEndpoint = "MINIO_ENDPOINT"
	envProfile  = "MINIO_PROFILE"
	envRegion   = "MINIO_REGION"

	defaultEndpoint = "http://localhost:9000"
	defaultProfile  = "minio"
	defaultRegion   = "us-east-1"
)

type S3Adapter struct {
	ctx    context.Context
	cancel context.CancelFunc
	client *s3.Client
}

type s3AdapterOpts func(*S3Adapter)

// NewS3Adapter builds an S3 client. Context defaults to context.Background();
// use WithContext to replace it. Endpoint, AWS profile, and region default to
// MinIO-friendly values and can be overridden with MINIO_ENDPOINT, MINIO_PROFILE,
// MINIO_REGION.
func NewS3Adapter(opts ...s3AdapterOpts) (*S3Adapter, error) {
	ctx, cancel := context.WithCancel(context.Background())
	adapter := &S3Adapter{
		ctx:    ctx,
		cancel: cancel,
	}

	for _, opt := range opts {
		opt(adapter)
	}

	endpoint := getEnvOrDefault(envEndpoint, defaultEndpoint)
	profile := getEnvOrDefault(envProfile, defaultProfile)
	region := getEnvOrDefault(envRegion, defaultRegion)

	cfg, err := config.LoadDefaultConfig(adapter.ctx,
		config.WithSharedConfigProfile(profile),
		config.WithRegion(region),
	)

	if err != nil {
		return nil, fmt.Errorf("unable to load aws config: %w", err)
	}

	adapter.client = s3.NewFromConfig(cfg, func(o *s3.Options) {
		o.BaseEndpoint = aws.String(strings.TrimSpace(endpoint))
		o.UsePathStyle = true
	})

	return adapter, nil
}

func getEnvOrDefault(key, fallback string) string {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return fallback
	}
	return v
}

// Close releases resources associated with the adapter context.
func (adapter *S3Adapter) Close() {
	if adapter.cancel != nil {
		adapter.cancel()
		adapter.cancel = nil
	}
}

func WithContext(ctx context.Context) s3AdapterOpts {
	return func(adapter *S3Adapter) {
		if adapter.cancel != nil {
			adapter.cancel()
		}
		adapter.ctx, adapter.cancel = context.WithCancel(ctx)
	}
}
