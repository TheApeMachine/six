package adapter

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/feature/s3/transfermanager"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/theapemachine/six/pkg/core/validate"
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
	io.ReadWriteCloser
	ctx    context.Context
	cancel context.CancelFunc
	client *s3.Client
	tm     *transfermanager.Client
}

type s3AdapterOpts func(*S3Adapter)

// NewS3Adapter builds an S3 client. Context defaults to context.Background();
// use WithContext to replace it. Endpoint, AWS profile, and region default to
// MinIO-friendly values and can be overridden with MINIO_ENDPOINT, MINIO_PROFILE,
// MINIO_REGION. Options in opts are applied after env defaults and client
// construction so callers can replace ctx used for subsequent operations.
func NewS3Adapter(ctx context.Context, opts ...s3AdapterOpts) (*S3Adapter, error) {
	ctx, cancel := context.WithCancel(ctx)

	adapter := &S3Adapter{
		ctx:    ctx,
		cancel: cancel,
	}

	endpoint := GetEnvOrDefault(envEndpoint, defaultEndpoint)
	profile := GetEnvOrDefault(envProfile, defaultProfile)
	region := GetEnvOrDefault(envRegion, defaultRegion)

	cfg, err := config.LoadDefaultConfig(
		adapter.ctx,
		config.WithSharedConfigProfile(profile),
		config.WithRegion(region),
	)

	if err != nil {
		cancel()
		return nil, fmt.Errorf("unable to load aws config: %w", err)
	}

	adapter.client = s3.NewFromConfig(cfg, func(o *s3.Options) {
		o.BaseEndpoint = aws.String(strings.TrimSpace(endpoint))
		o.UsePathStyle = true
	})

	adapter.tm = transfermanager.New(adapter.client)

	for _, opt := range opts {
		opt(adapter)
	}

	if err = validate.Require(map[string]any{
		"adapter": adapter,
		"client":  adapter.client,
		"tm":      adapter.tm,
	}); err != nil {
		cancel()
		return nil, err
	}

	return adapter, nil
}

/*
Read implements io.Reader and is used to store the current state
of the system on S3 (LakeFS).
*/
func (adapter *S3Adapter) Read(p []byte) (n int, err error) {
	return len(p), nil
}

/*
Write implements io.Writer and is used to retrieve the current state
of the system on S3 (LakeFS).
*/
func (adapter *S3Adapter) Write(p []byte) (n int, err error) {
	return len(p), nil
}

/*
Close releases resources associated with the adapter context.
*/
func (adapter *S3Adapter) Close() error {
	if adapter.cancel != nil {
		adapter.cancel()
		adapter.cancel = nil
	}
	return nil
}

// GetEnvOrDefault returns os.Getenv(key) trimmed, or fallback when empty.
func GetEnvOrDefault(key, fallback string) string {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return fallback
	}
	return v
}
