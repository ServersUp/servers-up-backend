package config

import (
	"context"
	"encoding/json"
	"fmt"
	"io"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/ssm"
)

// Provider handles retrieval of configuration and secrets from AWS services.
type Provider struct {
	ssmClient *ssm.Client
	s3Client  *s3.Client
}

func NewProvider(ssmClient *ssm.Client, s3Client *s3.Client) *Provider {
	return &Provider{
		ssmClient: ssmClient,
		s3Client:  s3Client,
	}
}

// GetSecret retrieves a decrypted parameter from AWS SSM.
func (p *Provider) GetSecret(ctx context.Context, path string) (string, error) {
	param, err := p.ssmClient.GetParameter(ctx, &ssm.GetParameterInput{
		Name:           aws.String(path),
		WithDecryption: aws.Bool(true),
	})
	if err != nil {
		return "", fmt.Errorf("failed to retrieve secret at %s: %w", path, err)
	}
	return *param.Parameter.Value, nil
}

// LoadJSONFromS3 downloads an object from S3 and unmarshals it into the provided target.
func (p *Provider) LoadJSONFromS3(ctx context.Context, bucket, key string, target any) error {
	obj, err := p.s3Client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return fmt.Errorf("failed to fetch s3://%s/%s: %w", bucket, key, err)
	}
	defer obj.Body.Close()

	body, err := io.ReadAll(obj.Body)
	if err != nil {
		return fmt.Errorf("failed to read S3 object body: %w", err)
	}

	if err := json.Unmarshal(body, target); err != nil {
		return fmt.Errorf("failed to decode JSON from S3: %w", err)
	}

	return nil
}
