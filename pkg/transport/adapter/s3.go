package adapter

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

type S3Adapter struct {
	ctx    context.Context
	cancel context.CancelFunc
	client *s3.Client
}

type s3AdapterOpts func(*S3Adapter)

func NewS3Adapter(opts ...s3AdapterOpts) (*S3Adapter, error) {
	adapter := &S3Adapter{}

	for _, opt := range opts {
		opt(adapter)
	}

	cfg, err := config.LoadDefaultConfig(adapter.ctx,
		config.WithSharedConfigProfile("minio"),
		config.WithRegion("us-east-1"),
	)

	if err != nil {
		return nil, fmt.Errorf("unable to load aws config: %w", err)
	}

	adapter.client = s3.NewFromConfig(cfg, func(o *s3.Options) {
		o.BaseEndpoint = aws.String("http://localhost:9000")
		o.UsePathStyle = true
	})

	return adapter, nil
}

func WithContext(ctx context.Context) s3AdapterOpts {
	return func(adapter *S3Adapter) {
		adapter.ctx, adapter.cancel = context.WithCancel(ctx)
	}
}
