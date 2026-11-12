package inventory

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
)

// headObjectAPI is the one S3 call the inventory makes, split out so tests
// can stand in for S3.
type headObjectAPI interface {
	HeadObject(ctx context.Context, in *s3.HeadObjectInput, optFns ...func(*s3.Options)) (*s3.HeadObjectOutput, error)
}

// s3LastWriter reads the LastModified time of each workspace's state object
// with HeadObject, which needs the same s3:GetObject permission as
// `terraform state pull`.
type s3LastWriter struct {
	client      headObjectAPI
	bucket, key string
	prefix      string
}

// newS3LastWriter builds an S3 client from the backend's region, profile,
// endpoints.s3 (or the older endpoint) and use_path_style (or
// force_path_style) settings, and the default AWS credential chain.
// assume_role in the backend config is not replayed: the caller's own
// credentials are used.
func newS3LastWriter(ctx context.Context, b Backend) (LastWriter, error) {
	bucket, key := b.str("bucket"), b.str("key")
	if bucket == "" || key == "" {
		return nil, errors.New("s3 backend: bucket or key missing from backend config")
	}

	var loadOpts []func(*config.LoadOptions) error
	if r := b.str("region"); r != "" {
		loadOpts = append(loadOpts, config.WithRegion(r))
	}
	if p := b.str("profile"); p != "" {
		loadOpts = append(loadOpts, config.WithSharedConfigProfile(p))
	}
	cfg, err := config.LoadDefaultConfig(ctx, loadOpts...)
	if err != nil {
		return nil, fmt.Errorf("s3 backend: load aws config: %w", err)
	}

	endpoint := b.str("endpoint")
	if eps, ok := b.Config["endpoints"].(map[string]any); ok {
		if s, _ := eps["s3"].(string); s != "" {
			endpoint = s
		}
	}
	pathStyle := b.Config["use_path_style"] == true || b.Config["force_path_style"] == true
	client := s3.NewFromConfig(cfg, func(o *s3.Options) {
		if endpoint != "" {
			o.BaseEndpoint = aws.String(endpoint)
		}
		o.UsePathStyle = pathStyle
	})
	return &s3LastWriter{client: client, bucket: bucket, key: key, prefix: b.str("workspace_key_prefix")}, nil
}

func (w *s3LastWriter) LastWrite(ctx context.Context, workspace string) (time.Time, bool, error) {
	out, err := w.client.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(w.bucket),
		Key:    aws.String(s3StateKey(w.key, w.prefix, workspace)),
	})
	var nf *types.NotFound
	if errors.As(err, &nf) {
		return time.Time{}, false, nil
	}
	if err != nil {
		return time.Time{}, false, err
	}
	if out.LastModified == nil {
		return time.Time{}, false, nil
	}
	return *out.LastModified, true, nil
}
