package storage

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

type S3Storage struct {
	Client *s3.Client
	Bucket string
}

func NewS3Storage() (*S3Storage, error) {
	region := os.Getenv("AWS_REGION")
	bucket := os.Getenv("AWS_BUCKET")

	cfg, err := config.LoadDefaultConfig(context.Background(), config.WithRegion(region))
	if err != nil {
		return nil, err
	}

	client := s3.NewFromConfig(cfg)

	return &S3Storage{
		Client: client,
		Bucket: bucket,
	}, nil
}

// Save now takes ctx and forwards it to PutObject.
func (s *S3Storage) Save(ctx context.Context, id string, data io.Reader, uploadType string) (string, error) {
	var key string

	switch uploadType {
	case "document":
		key = fmt.Sprintf("documents/%s", id)
	case "resume_analysis":
		key = fmt.Sprintf("resume_analysis/%s", id)
	default:
		return "", fmt.Errorf("invalid type: %s", uploadType)
	}

	_, err := s.Client.PutObject(ctx, &s3.PutObjectInput{
		Bucket: aws.String(s.Bucket),
		Key:    aws.String(key),
		Body:   data,
	})

	if err != nil {
		return "", err
	}

	return key, nil
}

func (s *S3Storage) AwsS3DeleteDocumt(ctx context.Context, s3Path string) error {
	_, err := s.Client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(s.Bucket),
		Key:    aws.String(s3Path),
	})
	if err != nil {
		return err
	}
	return nil
}
