package config

import (
	"context"
	"errors"
	"io"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/ssm"
	"github.com/aws/aws-sdk-go-v2/service/ssm/types"
)

type fakeSSM struct {
	calls atomic.Int32
}

func (f *fakeSSM) GetParameter(ctx context.Context, params *ssm.GetParameterInput, optFns ...func(*ssm.Options)) (*ssm.GetParameterOutput, error) {
	f.calls.Add(1)
	if params.Name == nil {
		return nil, errors.New("missing name")
	}
	return &ssm.GetParameterOutput{
		Parameter: &types.Parameter{
			Name:  params.Name,
			Value: aws.String("secret-value-" + *params.Name),
		},
	}, nil
}

type fakeS3 struct {
	body []byte
}

func (f *fakeS3) GetObject(ctx context.Context, params *s3.GetObjectInput, optFns ...func(*s3.Options)) (*s3.GetObjectOutput, error) {
	return &s3.GetObjectOutput{
		Body: io.NopCloser(strings.NewReader(string(f.body))),
	}, nil
}

func TestGetSecret_cachesPerPath(t *testing.T) {
	t.Parallel()
	ssmFake := &fakeSSM{}
	p := &Provider{
		ssmClient: ssmFake,
		s3Client:  &fakeS3{},
		secrets:   make(map[string]string),
	}

	ctx := context.Background()
	v1, err := p.GetSecret(ctx, "/path/a")
	if err != nil {
		t.Fatal(err)
	}
	v2, err := p.GetSecret(ctx, "/path/a")
	if err != nil {
		t.Fatal(err)
	}
	if v1 != v2 || v1 != "secret-value-/path/a" {
		t.Fatalf("unexpected values: %q %q", v1, v2)
	}
	if ssmFake.calls.Load() != 1 {
		t.Fatalf("expected 1 SSM call, got %d", ssmFake.calls.Load())
	}

	_, err = p.GetSecret(ctx, "/path/b")
	if err != nil {
		t.Fatal(err)
	}
	if ssmFake.calls.Load() != 2 {
		t.Fatalf("expected 2 SSM calls, got %d", ssmFake.calls.Load())
	}
}

func TestLoadJSONFromS3_rejectsOversize(t *testing.T) {
	t.Parallel()
	huge := []byte(`{"x":"` + strings.Repeat("a", maxS3JSONBytes) + `"}`)
	p := &Provider{
		ssmClient: &fakeSSM{},
		s3Client:  &fakeS3{body: huge},
		secrets:   make(map[string]string),
	}
	var out map[string]any
	err := p.LoadJSONFromS3(context.Background(), "b", "k", &out)
	if err == nil {
		t.Fatal("expected error for oversize JSON")
	}
}

func TestLoadJSONFromS3_ok(t *testing.T) {
	t.Parallel()
	p := &Provider{
		ssmClient: &fakeSSM{},
		s3Client:  &fakeS3{body: []byte(`{"game":"wow"}`)},
		secrets:   make(map[string]string),
	}
	var out struct {
		Game string `json:"game"`
	}
	if err := p.LoadJSONFromS3(context.Background(), "b", "k", &out); err != nil {
		t.Fatal(err)
	}
	if out.Game != "wow" {
		t.Fatalf("got %q", out.Game)
	}
}
