package s3store

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/asklit/valorant-vod-coach/internal/app"
	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/feature/s3/transfermanager"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
)

type Config struct {
	Endpoint        string
	Region          string
	Bucket          string
	AccessKeyID     string
	SecretAccessKey string
	SessionToken    string
	Prefix          string
	UsePathStyle    bool
}

type Store struct {
	Bucket    string
	Prefix    string
	client    objectClient
	transfers transferClient
}

type objectClient interface {
	s3.ListObjectsV2APIClient
	DeleteObject(context.Context, *s3.DeleteObjectInput, ...func(*s3.Options)) (*s3.DeleteObjectOutput, error)
	DeleteObjects(context.Context, *s3.DeleteObjectsInput, ...func(*s3.Options)) (*s3.DeleteObjectsOutput, error)
	HeadBucket(context.Context, *s3.HeadBucketInput, ...func(*s3.Options)) (*s3.HeadBucketOutput, error)
}

type transferClient interface {
	UploadObject(context.Context, *transfermanager.UploadObjectInput, ...func(*transfermanager.Options)) (*transfermanager.UploadObjectOutput, error)
	DownloadObject(context.Context, *transfermanager.DownloadObjectInput, ...func(*transfermanager.Options)) (*transfermanager.DownloadObjectOutput, error)
}

func New(ctx context.Context, config Config) (Store, error) {
	config.Bucket = strings.TrimSpace(config.Bucket)
	if config.Bucket == "" {
		return Store{}, errors.New("S3 bucket is required")
	}
	if config.Region == "" {
		config.Region = "us-east-1"
	}
	prefix, err := normalizeOptionalKey(config.Prefix)
	if err != nil {
		return Store{}, fmt.Errorf("invalid S3 key prefix: %w", err)
	}
	loadOptions := []func(*awsconfig.LoadOptions) error{awsconfig.WithRegion(config.Region)}
	if config.AccessKeyID != "" || config.SecretAccessKey != "" || config.SessionToken != "" {
		if config.AccessKeyID == "" || config.SecretAccessKey == "" {
			return Store{}, errors.New("both S3 access key ID and secret access key are required")
		}
		loadOptions = append(loadOptions, awsconfig.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(
			config.AccessKeyID,
			config.SecretAccessKey,
			config.SessionToken,
		)))
	}
	awsConfig, err := awsconfig.LoadDefaultConfig(ctx, loadOptions...)
	if err != nil {
		return Store{}, fmt.Errorf("load AWS configuration: %w", err)
	}
	client := s3.NewFromConfig(awsConfig, func(options *s3.Options) {
		options.UsePathStyle = config.UsePathStyle
		if endpoint := strings.TrimRight(strings.TrimSpace(config.Endpoint), "/"); endpoint != "" {
			options.BaseEndpoint = aws.String(endpoint)
		}
	})
	return Store{
		Bucket:    config.Bucket,
		Prefix:    prefix,
		client:    client,
		transfers: transfermanager.New(client),
	}, nil
}

func (s Store) UploadFile(ctx context.Context, key string, localPath string, contentType string) error {
	if s.transfers == nil {
		return errors.New("S3 transfer client is required")
	}
	objectKey, err := s.objectKey(key)
	if err != nil {
		return err
	}
	file, err := os.Open(localPath)
	if err != nil {
		return fmt.Errorf("open upload source: %w", err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return errors.New("upload source must be a regular file")
	}
	input := &transfermanager.UploadObjectInput{
		Bucket:        aws.String(s.Bucket),
		Key:           aws.String(objectKey),
		Body:          file,
		ContentLength: aws.Int64(info.Size()),
	}
	if contentType = strings.TrimSpace(contentType); contentType != "" {
		input.ContentType = aws.String(contentType)
	}
	if _, err := s.transfers.UploadObject(ctx, input); err != nil {
		return fmt.Errorf("upload s3://%s/%s: %w", s.Bucket, objectKey, err)
	}
	return nil
}

func (s Store) DownloadFile(ctx context.Context, key string, localPath string) error {
	if s.transfers == nil {
		return errors.New("S3 transfer client is required")
	}
	objectKey, err := s.objectKey(key)
	if err != nil {
		return err
	}
	directory := filepath.Dir(localPath)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return err
	}
	temp, err := os.CreateTemp(directory, ".object-*.part")
	if err != nil {
		return err
	}
	tempPath := temp.Name()
	success := false
	defer func() {
		temp.Close()
		if !success {
			os.Remove(tempPath)
		}
	}()
	if _, err := s.transfers.DownloadObject(ctx, &transfermanager.DownloadObjectInput{
		Bucket:   aws.String(s.Bucket),
		Key:      aws.String(objectKey),
		WriterAt: temp,
	}); err != nil {
		return fmt.Errorf("download s3://%s/%s: %w", s.Bucket, objectKey, err)
	}
	if err := temp.Sync(); err != nil {
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tempPath, localPath); err != nil {
		return err
	}
	success = true
	return nil
}

func (s Store) DeleteObject(ctx context.Context, key string) error {
	if s.client == nil {
		return errors.New("S3 client is required")
	}
	objectKey, err := s.objectKey(key)
	if err != nil {
		return err
	}
	if _, err := s.client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(s.Bucket),
		Key:    aws.String(objectKey),
	}); err != nil {
		return fmt.Errorf("delete s3://%s/%s: %w", s.Bucket, objectKey, err)
	}
	return nil
}

func (s Store) DeletePrefix(ctx context.Context, prefix string) error {
	if s.client == nil {
		return errors.New("S3 client is required")
	}
	objectPrefix, err := s.objectKey(prefix)
	if err != nil {
		return err
	}
	if !strings.HasSuffix(objectPrefix, "/") {
		objectPrefix += "/"
	}
	paginator := s3.NewListObjectsV2Paginator(s.client, &s3.ListObjectsV2Input{
		Bucket: aws.String(s.Bucket),
		Prefix: aws.String(objectPrefix),
	})
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return fmt.Errorf("list s3://%s/%s: %w", s.Bucket, objectPrefix, err)
		}
		for offset := 0; offset < len(page.Contents); offset += 1000 {
			end := min(offset+1000, len(page.Contents))
			objects := make([]s3types.ObjectIdentifier, 0, end-offset)
			for _, object := range page.Contents[offset:end] {
				objects = append(objects, s3types.ObjectIdentifier{Key: object.Key})
			}
			if len(objects) == 0 {
				continue
			}
			if _, err := s.client.DeleteObjects(ctx, &s3.DeleteObjectsInput{
				Bucket: aws.String(s.Bucket),
				Delete: &s3types.Delete{Objects: objects, Quiet: aws.Bool(true)},
			}); err != nil {
				return fmt.Errorf("delete s3://%s/%s: %w", s.Bucket, objectPrefix, err)
			}
		}
	}
	return nil
}

func (s Store) Ping(ctx context.Context) error {
	if s.client == nil {
		return errors.New("S3 client is required")
	}
	_, err := s.client.HeadBucket(ctx, &s3.HeadBucketInput{Bucket: aws.String(s.Bucket)})
	return err
}

func (s Store) objectKey(key string) (string, error) {
	key, err := normalizeRequiredKey(key)
	if err != nil {
		return "", err
	}
	if s.Prefix != "" {
		key = path.Join(s.Prefix, key)
	}
	return key, nil
}

func normalizeOptionalKey(value string) (string, error) {
	value = strings.Trim(strings.TrimSpace(value), "/")
	if value == "" {
		return "", nil
	}
	return normalizeRequiredKey(value)
}

func normalizeRequiredKey(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > 1024 || strings.HasPrefix(value, "/") || strings.HasSuffix(value, "/../") {
		return "", errors.New("object key is invalid")
	}
	cleaned := path.Clean(value)
	if cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, "../") || cleaned != value || strings.Contains(value, "\\") {
		return "", errors.New("object key is invalid")
	}
	return cleaned, nil
}

var _ app.BlobStore = Store{}
var _ app.HealthChecker = Store{}
