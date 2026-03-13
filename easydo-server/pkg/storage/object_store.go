package storage

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strings"

	"easydo-server/internal/config"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

type ObjectStore interface {
	EnsureBucket(ctx context.Context) error
	PutObject(ctx context.Context, objectKey string, body []byte, contentType string) (int64, error)
	GetObject(ctx context.Context, objectKey string) (io.ReadCloser, error)
	Bucket() string
}

type MinioObjectStore struct {
	client *minio.Client
	bucket string
}

func NewObjectStore() (ObjectStore, error) {
	endpoint := strings.TrimSpace(config.Config.GetString("object_storage.endpoint"))
	accessKey := config.Config.GetString("object_storage.access_key")
	secretKey := config.Config.GetString("object_storage.secret_key")
	bucket := config.Config.GetString("object_storage.bucket")
	if endpoint == "" || accessKey == "" || secretKey == "" || bucket == "" {
		return nil, fmt.Errorf("object storage configuration incomplete")
	}
	client, err := minio.New(endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(accessKey, secretKey, ""),
		Secure: config.Config.GetBool("object_storage.use_ssl"),
		Region: config.Config.GetString("object_storage.region"),
	})
	if err != nil {
		return nil, err
	}
	return &MinioObjectStore{client: client, bucket: bucket}, nil

}

func (s *MinioObjectStore) EnsureBucket(ctx context.Context) error {
	exists, err := s.client.BucketExists(ctx, s.bucket)
	if err != nil {
		return err
	}
	if exists {
		return nil
	}
	return s.client.MakeBucket(ctx, s.bucket, minio.MakeBucketOptions{Region: config.Config.GetString("object_storage.region")})
}

func (s *MinioObjectStore) PutObject(ctx context.Context, objectKey string, body []byte, contentType string) (int64, error) {
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	info, err := s.client.PutObject(ctx, s.bucket, objectKey, bytes.NewReader(body), int64(len(body)), minio.PutObjectOptions{ContentType: contentType})
	if err != nil {
		return 0, err
	}
	return info.Size, nil
}

func (s *MinioObjectStore) GetObject(ctx context.Context, objectKey string) (io.ReadCloser, error) {
	obj, err := s.client.GetObject(ctx, s.bucket, objectKey, minio.GetObjectOptions{})
	if err != nil {
		return nil, err
	}
	_, err = obj.Stat()
	if err != nil {
		_ = obj.Close()
		return nil, err
	}
	return obj, nil
}

func (s *MinioObjectStore) Bucket() string {
	return s.bucket
}
