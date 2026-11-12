package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/moveeeax/tfstate-drift/internal/drift"
)

// slackMaxResources caps the resources listed in one message. Slack truncates
// message text past 40,000 characters; the cap keeps a large drift readable
// and far below that limit.
const (
	slackMaxResources = 50
	slackMaxPaths     = 10
)

// slackEscaper escapes the three characters Slack treats as markup in message
// text, as its "Formatting text" guide requires.
var slackEscaper = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;")

// Slack posts a drift summary to a Slack incoming webhook.
type Slack struct {
	webhook string
	client  *http.Client
}

// NewSlack validates the webhook URL. It must be https; plain http is
// accepted only for loopback hosts (local relays and tests).
func NewSlack(webhook string) (*Slack, error) {
	u, err := url.Parse(webhook)
	if err != nil || u.Host == "" {
		return nil, errors.New("slack: webhook is not a valid URL")
	}
	switch {
	case u.Scheme == "https":
	case u.Scheme == "http" && isLoopback(u.Hostname()):
	default:
		return nil, errors.New("slack: webhook must be an https URL")
	}
	return &Slack{webhook: webhook, client: &http.Client{Timeout: 10 * time.Second}}, nil
}

// Name implements Sink.
func (s *Slack) Name() string { return "slack" }

// Notify implements Sink.
func (s *Slack) Notify(ctx context.Context, report *drift.Report) error {
	body, err := json.Marshal(map[string]string{"text": SlackText(report)})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.webhook, bytes.NewReader(body))
	if err != nil {
		return errors.New("build request")
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := s.client.Do(req)
	if err != nil {
		// *url.Error embeds the webhook URL, which is a secret; keep only
		// the cause.
		var ue *url.Error
		if errors.As(err, &ue) {
			err = ue.Err
		}
		return fmt.Errorf("post: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("webhook returned %d: %s", resp.StatusCode, strings.TrimSpace(string(msg)))
	}
	return nil
}

// SlackText renders the report as Slack mrkdwn: a headline and one line per
// drifted resource with the attribute paths that changed. Values are left
// out, so nothing from the state (sensitive or not) reaches the channel.
func SlackText(report *drift.Report) string {
	var b strings.Builder
	fmt.Fprintf(&b, ":rotating_light: *Terraform drift: %d resource(s)*", report.ResourceCount)
	if report.IgnoredCount > 0 {
		fmt.Fprintf(&b, " (%d ignored)", report.IgnoredCount)
	}
	listed := 0
	for _, m := range report.Modules {
		for _, r := range m.Resources {
			if listed == slackMaxResources {
				fmt.Fprintf(&b, "\n…and %d more", report.ResourceCount-listed)
				return b.String()
			}
			fmt.Fprintf(&b, "\n• `%s` (%s)", slackEscaper.Replace(r.Address), r.Action)
			if len(r.Attributes) > 0 {
				var paths []string
				for i, a := range r.Attributes {
					if i == slackMaxPaths {
						paths = append(paths, fmt.Sprintf("+%d more", len(r.Attributes)-i))
						break
					}
					paths = append(paths, slackEscaper.Replace(a.Path))
				}
				fmt.Fprintf(&b, ": %s", strings.Join(paths, ", "))
			}
			listed++
		}
	}
	return b.String()
}

func isLoopback(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
