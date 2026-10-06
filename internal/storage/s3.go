package storage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	smithyhttp "github.com/aws/smithy-go/transport/http"

	"github.com/mkappworks-dev/cloudzilla-app/internal/config"
)

// S3Backend keeps objects in an S3-compatible bucket, every key under prefix.
type S3Backend struct {
	client *s3.Client
	bucket string
	prefix string
}

func NewS3(ctx context.Context, cfg config.S3StorageConfig) (*S3Backend, error) {
	if cfg.Bucket == "" {
		return nil, errors.New("storage: storage.s3.bucket is required for the s3 backend")
	}
	if cfg.Prefix != "" && !ValidKey(cfg.Prefix) {
		return nil, fmt.Errorf("storage: storage.s3.prefix %q must be [a-z0-9._-] segments joined by /", cfg.Prefix)
	}
	if (cfg.AccessKeyID == "") != (cfg.SecretAccessKey == "") {
		return nil, errors.New("storage: set both storage.s3.access_key_id and storage.s3.secret_access_key, or neither")
	}
	region := cfg.Region
	if region == "" {
		region = "us-east-1"
	}
	opts := []func(*awsconfig.LoadOptions) error{
		awsconfig.WithRegion(region),
		// Since early 2025 the SDK adds CRC32 checksums by default, which
		// several S3-compatible servers reject.
		awsconfig.WithRequestChecksumCalculation(aws.RequestChecksumCalculationWhenRequired),
		awsconfig.WithResponseChecksumValidation(aws.ResponseChecksumValidationWhenRequired),
	}
	if cfg.AccessKeyID != "" {
		opts = append(opts, awsconfig.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider(cfg.AccessKeyID, cfg.SecretAccessKey, "")))
	}
	awsCfg, err := awsconfig.LoadDefaultConfig(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("storage: load s3 config: %w", err)
	}
	client := s3.NewFromConfig(awsCfg, func(o *s3.Options) {
		if cfg.Endpoint != "" {
			o.BaseEndpoint = aws.String(cfg.Endpoint)
		}
		o.UsePathStyle = cfg.PathStyle
	})
	prefix := cfg.Prefix
	if prefix != "" {
		prefix += "/"
	}
	return &S3Backend{client: client, bucket: cfg.Bucket, prefix: prefix}, nil
}

func (b *S3Backend) Put(ctx context.Context, key string, r io.Reader, size int64) error {
	if err := checkKey(key); err != nil {
		return err
	}
	in := &s3.PutObjectInput{Bucket: &b.bucket, Key: aws.String(b.prefix + key), Body: r}
	if size >= 0 {
		in.ContentLength = aws.Int64(size)
	}
	if _, err := b.client.PutObject(ctx, in); err != nil {
		return fmt.Errorf("storage: put %s: %w", key, err)
	}
	return nil
}

func (b *S3Backend) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	if err := checkKey(key); err != nil {
		return nil, err
	}
	out, err := b.client.GetObject(ctx, &s3.GetObjectInput{Bucket: &b.bucket, Key: aws.String(b.prefix + key)})
	if isNotFound(err) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("storage: get %s: %w", key, err)
	}
	return out.Body, nil
}

func (b *S3Backend) Delete(ctx context.Context, key string) error {
	if err := checkKey(key); err != nil {
		return err
	}
	_, err := b.client.DeleteObject(ctx, &s3.DeleteObjectInput{Bucket: &b.bucket, Key: aws.String(b.prefix + key)})
	if err != nil && !isNotFound(err) {
		return fmt.Errorf("storage: delete %s: %w", key, err)
	}
	return nil
}

func (b *S3Backend) Exists(ctx context.Context, key string) (bool, error) {
	if err := checkKey(key); err != nil {
		return false, err
	}
	_, err := b.client.HeadObject(ctx, &s3.HeadObjectInput{Bucket: &b.bucket, Key: aws.String(b.prefix + key)})
	if isNotFound(err) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("storage: head %s: %w", key, err)
	}
	return true, nil
}

// isNotFound also takes a bare 404, which is all HeadObject gets: a HEAD
// response has no body to carry the NoSuchKey code.
func isNotFound(err error) bool {
	if err == nil {
		return false
	}
	var nsk *types.NoSuchKey
	var nf *types.NotFound
	if errors.As(err, &nsk) || errors.As(err, &nf) {
		return true
	}
	var re *smithyhttp.ResponseError
	return errors.As(err, &re) && re.HTTPStatusCode() == http.StatusNotFound
}
