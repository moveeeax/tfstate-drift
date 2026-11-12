// Package notify pushes a drift report to external sinks. Sinks fire only when
// drift exists, so a scheduled job stays quiet on clean runs.
package notify

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/moveeeax/tfstate-drift/internal/drift"
)

// Sink receives a drift report.
type Sink interface {
	Notify(ctx context.Context, report *drift.Report) error
	// Name identifies the sink in error messages without revealing secrets
	// such as the webhook URL.
	Name() string
}

// ParseSpec turns a `--notify` value into a Sink. The only kind today is
// "slack=<webhook url>"; "slack=env:NAME" reads the URL from the environment
// variable NAME so it stays out of shell history and process listings.
func ParseSpec(spec string) (Sink, error) {
	kind, target, ok := strings.Cut(spec, "=")
	if !ok || target == "" {
		return nil, fmt.Errorf("invalid --notify %q: want slack=<webhook url> or slack=env:NAME", redact(spec))
	}
	switch kind {
	case "slack":
		if name, ok := strings.CutPrefix(target, "env:"); ok {
			target = os.Getenv(name)
			if target == "" {
				return nil, fmt.Errorf("--notify slack=env:%s: environment variable is empty or unset", name)
			}
		}
		return NewSlack(target)
	default:
		return nil, fmt.Errorf("invalid --notify kind %q: want slack", kind)
	}
}

// Send delivers report to every sink when drift exists and does nothing on a
// clean report. It returns the first error after trying all sinks.
func Send(ctx context.Context, sinks []Sink, report *drift.Report) error {
	if report == nil || !report.DriftDetected {
		return nil
	}
	var first error
	for _, s := range sinks {
		if err := s.Notify(ctx, report); err != nil && first == nil {
			first = fmt.Errorf("notify %s: %w", s.Name(), err)
		}
	}
	return first
}

// redact hides everything after "=" so a mistyped spec never prints a webhook.
func redact(spec string) string {
	if kind, _, ok := strings.Cut(spec, "="); ok {
		return kind + "=..."
	}
	return spec
}
