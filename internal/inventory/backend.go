package inventory

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"time"
)

// DefaultWorkspace is the name Terraform gives the workspace that always
// exists.
const DefaultWorkspace = "default"

// Backend is the backend Terraform recorded at `terraform init`, read from
// the working directory's data dir.
type Backend struct {
	Type   string         `json:"type"`
	Config map[string]any `json:"config"`
}

// ReadBackend loads the backend recorded in <dir>/.terraform/terraform.tfstate
// (or $TF_DATA_DIR/terraform.tfstate, which Terraform resolves relative to
// dir). A directory without that file uses the implicit local backend.
func ReadBackend(dir string) (Backend, error) {
	dataDir := os.Getenv("TF_DATA_DIR")
	if dataDir == "" {
		dataDir = ".terraform"
	}
	if !filepath.IsAbs(dataDir) {
		dataDir = filepath.Join(dir, dataDir)
	}
	data, err := os.ReadFile(filepath.Join(dataDir, "terraform.tfstate"))
	if errors.Is(err, fs.ErrNotExist) {
		return Backend{Type: "local"}, nil
	}
	if err != nil {
		return Backend{}, err
	}
	var doc struct {
		Backend *Backend `json:"backend"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		return Backend{}, fmt.Errorf("decode backend state: %w", err)
	}
	if doc.Backend == nil || doc.Backend.Type == "" {
		return Backend{Type: "local"}, nil
	}
	return *doc.Backend, nil
}

// str returns a string config attribute, or "" when it is unset or null.
func (b Backend) str(key string) string {
	s, _ := b.Config[key].(string)
	return s
}

// LastWriter reports when a workspace's state object was last written. That
// happens on apply, import, `state mv/rm` and refresh-only apply, so it is
// the last time anyone changed the workspace through Terraform.
type LastWriter interface {
	// LastWrite returns ok=false when the backend holds no state object for
	// the workspace.
	LastWrite(ctx context.Context, workspace string) (t time.Time, ok bool, err error)
}

// NewLastWriter returns the LastWriter for backend b of the working directory
// dir, or nil when the backend type exposes no timestamp the tool can read.
// Supported: local (file modification time) and s3 (object LastModified).
func NewLastWriter(ctx context.Context, dir string, b Backend) (LastWriter, error) {
	switch b.Type {
	case "local":
		return localLastWriter{dir: dir, path: b.str("path"), workspaceDir: b.str("workspace_dir")}, nil
	case "s3":
		return newS3LastWriter(ctx, b)
	default:
		return nil, nil
	}
}

// localLastWriter follows the local backend's layout: the default workspace
// lives at path (terraform.tfstate), others at
// <workspace_dir>/<name>/terraform.tfstate (workspace_dir defaults to
// terraform.tfstate.d). Relative paths are relative to the working directory.
type localLastWriter struct {
	dir, path, workspaceDir string
}

func (l localLastWriter) statePath(workspace string) string {
	p := filepath.Join(l.workspaceOrDefault(), workspace, "terraform.tfstate")
	if workspace == DefaultWorkspace {
		p = l.path
		if p == "" {
			p = "terraform.tfstate"
		}
	}
	if !filepath.IsAbs(p) {
		p = filepath.Join(l.dir, p)
	}
	return p
}

func (l localLastWriter) workspaceOrDefault() string {
	if l.workspaceDir != "" {
		return l.workspaceDir
	}
	return "terraform.tfstate.d"
}

func (l localLastWriter) LastWrite(_ context.Context, workspace string) (time.Time, bool, error) {
	fi, err := os.Stat(l.statePath(workspace))
	if errors.Is(err, fs.ErrNotExist) {
		return time.Time{}, false, nil
	}
	if err != nil {
		return time.Time{}, false, err
	}
	return fi.ModTime(), true, nil
}

// s3StateKey mirrors the s3 backend: the default workspace is stored at key,
// any other at <workspace_key_prefix>/<name>/<key>, the prefix defaulting to
// "env:".
func s3StateKey(key, prefix, workspace string) string {
	if workspace == DefaultWorkspace {
		return key
	}
	if prefix == "" {
		prefix = "env:"
	}
	return path.Join(prefix, workspace, key)
}
