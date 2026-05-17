package config

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sync"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/ssm"
)

// maxS3JSONBytes caps S3 config object size to avoid unbounded memory use in Lambda.
const maxS3JSONBytes = 8 << 20 // 8 MiB

// Provider handles retrieval of configuration and secrets from AWS services.
// GetSecret caches decrypted SSM SecureString values for the process lifetime so warm
// Lambdas do not invoke KMS Decrypt on every call (see bnet poller schedule).
// After SSM rotation, recycle the Lambda execution environment or redeploy.
type Provider struct {
	ssmClient ssmAPI
	s3Client  s3API

	secretMu sync.RWMutex
	secrets  map[string]string
}

func NewProvider(ssmClient *ssm.Client, s3Client *s3.Client) *Provider {
	return &Provider{
		ssmClient: ssmClient,
		s3Client:  s3Client,
		secrets:   make(map[string]string),
	}
}

// GetSecret retrieves a decrypted parameter from AWS SSM, with in-process caching.
func (p *Provider) GetSecret(ctx context.Context, path string) (string, error) {
	if path == "" {
		return "", fmt.Errorf("empty SSM parameter path")
	}

	p.secretMu.RLock()
	if v, ok := p.secrets[path]; ok {
		p.secretMu.RUnlock()
		return v, nil
	}
	p.secretMu.RUnlock()

	param, err := p.ssmClient.GetParameter(ctx, &ssm.GetParameterInput{
		Name:           aws.String(path),
		WithDecryption: aws.Bool(true),
	})
	if err != nil {
		return "", fmt.Errorf("failed to retrieve secret at %s: %w", path, err)
	}
	if param.Parameter == nil || param.Parameter.Value == nil {
		return "", fmt.Errorf("empty SSM parameter at %s", path)
	}
	value := *param.Parameter.Value

	p.secretMu.Lock()
	p.secrets[path] = value
	p.secretMu.Unlock()

	return value, nil
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

	body, err := io.ReadAll(io.LimitReader(obj.Body, maxS3JSONBytes))
	if err != nil {
		return fmt.Errorf("failed to read S3 object body: %w", err)
	}

	if err := json.Unmarshal(body, target); err != nil {
		return fmt.Errorf("failed to decode JSON from S3: %w", err)
	}

	return nil
}
