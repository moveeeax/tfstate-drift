package inventory

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
)

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestReadBackend(t *testing.T) {
	t.Setenv("TF_DATA_DIR", "")
	dir := t.TempDir()

	b, err := ReadBackend(dir)
	if err != nil || b.Type != "local" {
		t.Fatalf("uninitialized dir: %+v, %v", b, err)
	}

	writeFile(t, filepath.Join(dir, ".terraform", "terraform.tfstate"),
		`{"version":3,"backend":{"type":"s3","config":{"bucket":"tf","key":"app.tfstate","region":"eu-west-1","workspace_key_prefix":null}}}`)
	b, err = ReadBackend(dir)
	if err != nil {
		t.Fatal(err)
	}
	if b.Type != "s3" || b.str("bucket") != "tf" || b.str("workspace_key_prefix") != "" {
		t.Fatalf("got %+v", b)
	}
}

func TestReadBackendDataDir(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "tfdata", "terraform.tfstate"), `{"backend":{"type":"gcs","config":{}}}`)

	t.Setenv("TF_DATA_DIR", "tfdata") // relative to the working directory
	b, err := ReadBackend(dir)
	if err != nil || b.Type != "gcs" {
		t.Fatalf("relative TF_DATA_DIR: %+v, %v", b, err)
	}

	t.Setenv("TF_DATA_DIR", filepath.Join(dir, "tfdata"))
	b, err = ReadBackend(t.TempDir())
	if err != nil || b.Type != "gcs" {
		t.Fatalf("absolute TF_DATA_DIR: %+v, %v", b, err)
	}
}

func TestReadBackendInvalid(t *testing.T) {
	t.Setenv("TF_DATA_DIR", "")
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, ".terraform", "terraform.tfstate"), "{")
	if _, err := ReadBackend(dir); err == nil {
		t.Fatal("expected a decode error")
	}
}

func TestNewLastWriterUnsupported(t *testing.T) {
	for _, typ := range []string{"gcs", "azurerm", "remote", "cloud"} {
		w, err := NewLastWriter(context.Background(), ".", Backend{Type: typ})
		if w != nil || err != nil {
			t.Errorf("%s: got %v, %v; want nil, nil", typ, w, err)
		}
	}
}

func TestLocalLastWriter(t *testing.T) {
	dir := t.TempDir()
	old := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	writeFile(t, filepath.Join(dir, "terraform.tfstate"), "{}")
	writeFile(t, filepath.Join(dir, "terraform.tfstate.d", "prod", "terraform.tfstate"), "{}")
	if err := os.Chtimes(filepath.Join(dir, "terraform.tfstate"), old, old); err != nil {
		t.Fatal(err)
	}

	w, err := NewLastWriter(context.Background(), dir, Backend{Type: "local"})
	if err != nil {
		t.Fatal(err)
	}
	got, ok, err := w.LastWrite(context.Background(), "default")
	if err != nil || !ok || !got.Equal(old) {
		t.Errorf("default: %v %v %v", got, ok, err)
	}
	if _, ok, err := w.LastWrite(context.Background(), "prod"); err != nil || !ok {
		t.Errorf("prod: %v %v", ok, err)
	}
	if _, ok, err := w.LastWrite(context.Background(), "never-applied"); err != nil || ok {
		t.Errorf("missing workspace: ok=%v err=%v, want false, nil", ok, err)
	}
}

func TestLocalStatePathCustom(t *testing.T) {
	l := localLastWriter{dir: "/infra", path: "state/main.tfstate", workspaceDir: "ws"}
	if got := l.statePath("default"); got != filepath.FromSlash("/infra/state/main.tfstate") {
		t.Errorf("default = %s", got)
	}
	if got := l.statePath("qa"); got != filepath.FromSlash("/infra/ws/qa/terraform.tfstate") {
		t.Errorf("qa = %s", got)
	}
}

func TestS3StateKey(t *testing.T) {
	for _, tc := range []struct{ key, prefix, ws, want string }{
		{"app/terraform.tfstate", "", "default", "app/terraform.tfstate"},
		{"app/terraform.tfstate", "", "prod", "env:/prod/app/terraform.tfstate"},
		{"terraform.tfstate", "workspaces", "qa", "workspaces/qa/terraform.tfstate"},
	} {
		if got := s3StateKey(tc.key, tc.prefix, tc.ws); got != tc.want {
			t.Errorf("s3StateKey(%q, %q, %q) = %q, want %q", tc.key, tc.prefix, tc.ws, got, tc.want)
		}
	}
}

// fakeS3 answers HeadObject from a key -> LastModified map.
type fakeS3 struct {
	objects map[string]time.Time
	err     error
	bucket  string
}

func (f *fakeS3) HeadObject(_ context.Context, in *s3.HeadObjectInput, _ ...func(*s3.Options)) (*s3.HeadObjectOutput, error) {
	f.bucket = *in.Bucket
	if f.err != nil {
		return nil, f.err
	}
	t, ok := f.objects[*in.Key]
	if !ok {
		return nil, &types.NotFound{}
	}
	return &s3.HeadObjectOutput{LastModified: &t}, nil
}

func TestS3LastWriter(t *testing.T) {
	when := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	api := &fakeS3{objects: map[string]time.Time{"env:/prod/app.tfstate": when}}
	w := &s3LastWriter{client: api, bucket: "tf-state", key: "app.tfstate"}

	got, ok, err := w.LastWrite(context.Background(), "prod")
	if err != nil || !ok || !got.Equal(when) {
		t.Fatalf("prod: %v %v %v", got, ok, err)
	}
	if api.bucket != "tf-state" {
		t.Errorf("bucket = %q", api.bucket)
	}
	if _, ok, err := w.LastWrite(context.Background(), "default"); err != nil || ok {
		t.Errorf("missing object: ok=%v err=%v, want false, nil", ok, err)
	}

	api.err = errors.New("AccessDenied")
	if _, _, err := w.LastWrite(context.Background(), "prod"); err == nil {
		t.Error("expected the S3 error to surface")
	}
}

func TestNewS3LastWriterNeedsBucketAndKey(t *testing.T) {
	_, err := NewLastWriter(context.Background(), ".", Backend{Type: "s3", Config: map[string]any{"bucket": "b"}})
	if err == nil {
		t.Fatal("expected an error without key")
	}
}

func TestNewS3LastWriterOffline(t *testing.T) {
	// Building the client reads config but makes no request.
	t.Setenv("AWS_CONFIG_FILE", filepath.Join(t.TempDir(), "none"))
	t.Setenv("AWS_SHARED_CREDENTIALS_FILE", filepath.Join(t.TempDir(), "none"))
	t.Setenv("AWS_PROFILE", "")
	w, err := NewLastWriter(context.Background(), ".", Backend{Type: "s3", Config: map[string]any{
		"bucket": "b", "key": "k", "region": "eu-west-1",
		"endpoints": map[string]any{"s3": "http://127.0.0.1:9000"}, "use_path_style": true,
	}})
	if err != nil {
		t.Fatal(err)
	}
	if w.(*s3LastWriter).bucket != "b" {
		t.Errorf("got %+v", w)
	}
}
