package objectstore

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"strings"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"github.com/minio/minio-go/v7/pkg/s3utils"
)

const defaultStartupTimeout = 10 * time.Second

type Config struct {
	Endpoint        string
	Region          string
	Bucket          string
	AccessKeyID     string
	SecretAccessKey string
	StartupTimeout  time.Duration
}

func DefaultConfig() Config {
	return Config{
		Endpoint:        "http://127.0.0.1:9000",
		Region:          "us-east-1",
		Bucket:          "slipramp",
		AccessKeyID:     "slipramp",
		SecretAccessKey: "slipramp-development",
		StartupTimeout:  defaultStartupTimeout,
	}
}

func (c Config) Validate() error {
	endpoint, err := parseEndpoint(c.Endpoint)
	if err != nil {
		return err
	}

	if endpoint.Host == "" {
		return errors.New("S3 endpoint must include a host")
	}

	if endpoint.User != nil {
		return errors.New("S3 endpoint must not contain user information")
	}

	if err := s3utils.CheckValidBucketName(c.Bucket); err != nil {
		return fmt.Errorf("validate S3 bucket: %w", err)
	}

	if strings.TrimSpace(c.Region) == "" {
		return errors.New("S3 region must not be empty")
	}

	if strings.TrimSpace(c.AccessKeyID) == "" {
		return errors.New("S3 access key ID must not be empty")
	}

	if strings.TrimSpace(c.SecretAccessKey) == "" {
		return errors.New("S3 secret access key must not be empty")
	}

	if c.StartupTimeout <= 0 {
		return errors.New("S3 startup timeout must be positive")
	}

	return nil
}

type Store struct {
	client         *minio.Client
	bucket         string
	startupTimeout time.Duration
}

func New(cfg Config) (*Store, error) {
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("validate S3 configuration: %w", err)
	}

	endpoint, err := parseEndpoint(cfg.Endpoint)
	if err != nil {
		return nil, err
	}

	client, err := minio.New(endpoint.Host, &minio.Options{
		Creds:  credentials.NewStaticV4(cfg.AccessKeyID, cfg.SecretAccessKey, ""),
		Secure: endpoint.Scheme == "https",
		Region: cfg.Region,
	})
	if err != nil {
		return nil, fmt.Errorf("create S3 client: %w", err)
	}

	return &Store{
		client:         client,
		bucket:         cfg.Bucket,
		startupTimeout: cfg.StartupTimeout,
	}, nil
}

func (s *Store) Check(ctx context.Context) error {
	startupContext, cancel := context.WithTimeout(ctx, s.startupTimeout)
	defer cancel()

	exists, err := s.client.BucketExists(startupContext, s.bucket)
	if err != nil {
		return fmt.Errorf("check S3 bucket %q: %w", s.bucket, err)
	}

	if exists {
		return nil
	}

	return fmt.Errorf("S3 bucket %q does not exist", s.bucket)
}

func (s *Store) Put(ctx context.Context, key string, body io.Reader, size int64, options PutOptions) (int64, error) {
	if err := validateObjectKey(key); err != nil {
		return 0, err
	}

	if body == nil {
		return 0, errors.New("object body is required")
	}

	if size < 0 {
		return 0, errors.New("object size must not be negative")
	}

	result, err := s.client.PutObject(
		ctx,
		s.bucket,
		key,
		body,
		size,
		minio.PutObjectOptions{ContentType: options.MediaType},
	)
	if err != nil {
		return 0, fmt.Errorf("put S3 object %q: %w", key, err)
	}

	return result.Size, nil
}

func (s *Store) Open(ctx context.Context, key string) (Object, error) {
	if err := validateObjectKey(key); err != nil {
		return Object{}, err
	}

	body, err := s.client.GetObject(ctx, s.bucket, key, minio.GetObjectOptions{})
	if err != nil {
		return Object{}, fmt.Errorf("open S3 object %q: %w", key, err)
	}

	info, err := body.Stat()
	if err != nil {
		_ = body.Close()

		return Object{}, fmt.Errorf("stat S3 object %q: %w", key, err)
	}

	return Object{
		Body:      body,
		Size:      info.Size,
		MediaType: info.ContentType,
	}, nil
}

func (s *Store) Delete(ctx context.Context, key string) error {
	if err := validateObjectKey(key); err != nil {
		return err
	}

	err := s.client.RemoveObject(ctx, s.bucket, key, minio.RemoveObjectOptions{})
	if err != nil {
		return fmt.Errorf("delete S3 object %q: %w", key, err)
	}

	return nil
}

func (s *Store) List(ctx context.Context, prefix string) ([]ObjectInfo, error) {
	if err := validateObjectKey(prefix); err != nil {
		return nil, err
	}

	objects := make([]ObjectInfo, 0)
	for item := range s.client.ListObjects(ctx, s.bucket, minio.ListObjectsOptions{Prefix: prefix, Recursive: true}) {
		if item.Err != nil {
			return nil, fmt.Errorf("list S3 objects under %q: %w", prefix, item.Err)
		}

		objects = append(objects, ObjectInfo{
			Key:          item.Key,
			Size:         item.Size,
			LastModified: item.LastModified.UTC(),
		})
	}

	return objects, nil
}

func parseEndpoint(value string) (*url.URL, error) {
	endpoint, err := url.ParseRequestURI(value)
	if err != nil {
		return nil, fmt.Errorf("parse S3 endpoint: %w", err)
	}

	if endpoint.Scheme != "http" && endpoint.Scheme != "https" {
		return nil, errors.New("S3 endpoint scheme must be http or https")
	}

	if endpoint.Path != "" && endpoint.Path != "/" {
		return nil, errors.New("S3 endpoint must not include a path")
	}

	if endpoint.RawQuery != "" || endpoint.Fragment != "" {
		return nil, errors.New("S3 endpoint must not include a query or fragment")
	}

	return endpoint, nil
}

func validateObjectKey(key string) error {
	if strings.TrimSpace(key) == "" {
		return errors.New("object key must not be empty")
	}

	if strings.HasPrefix(key, "/") {
		return errors.New("object key must be relative")
	}

	return nil
}
