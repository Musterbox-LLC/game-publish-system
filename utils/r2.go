// utils/r2.go
package utils

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"mime/multipart"
	"os"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

var r2Client *s3.Client
var r2Bucket string
var cdnBaseURL string

func InitR2() error {
	accountID := os.Getenv("CLOUDFLARE_ACCOUNT_ID")
	accessKeyID := os.Getenv("R2_ACCESS_KEY_ID")
	accessKeySecret := os.Getenv("R2_ACCESS_KEY_SECRET")
	r2Bucket = os.Getenv("R2_BUCKET_NAME")
	cdnBaseURL = os.Getenv("CDN_BASE_URL")
	if cdnBaseURL == "" {
		cdnBaseURL = fmt.Sprintf("https://%s.r2.cloudflarestorage.com", accountID)
	}

	cfg, err := config.LoadDefaultConfig(context.TODO(),
		config.WithRegion("auto"),
		config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(
			accessKeyID, accessKeySecret, "",
		)),
		config.WithEndpointResolver(aws.EndpointResolverFunc(
			func(service, region string) (aws.Endpoint, error) {
				return aws.Endpoint{
					URL: fmt.Sprintf("https://%s.r2.cloudflarestorage.com", accountID),
				}, nil
			}),
		),
	)
	if err != nil {
		return fmt.Errorf("failed to load R2 config: %w", err)
	}

	r2Client = s3.NewFromConfig(cfg)
	return nil
}

// UploadFileToR2 uploads a multipart file to R2 and returns the public URL.
// key is the R2 object key (e.g., "logos/abc123.png")
func UploadFileToR2(fileHeader *multipart.FileHeader, key string) (string, error) {
	file, err := fileHeader.Open()
	if err != nil {
		return "", fmt.Errorf("failed to open file: %w", err)
	}
	defer file.Close()

	buf := new(bytes.Buffer)
	if _, err := io.Copy(buf, file); err != nil {
		return "", fmt.Errorf("failed to read file: %w", err)
	}

	_, err = r2Client.PutObject(context.TODO(), &s3.PutObjectInput{
		Bucket:      aws.String(r2Bucket),
		Key:         aws.String(key),
		Body:        buf,
		ContentType: aws.String(fileHeader.Header.Get("Content-Type")),
	})
	if err != nil {
		return "", fmt.Errorf("failed to upload to R2: %w", err)
	}

	// ✅ Return public CDN URL (prefer your custom CDN if set)
	url := fmt.Sprintf("%s/%s", cdnBaseURL, key)
	return url, nil
}