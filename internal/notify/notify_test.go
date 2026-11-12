package notify

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/moveeeax/tfstate-drift/internal/drift"
)

func driftReport() *drift.Report {
	return &drift.Report{
		DriftDetected: true,
		ResourceCount: 2,
		IgnoredCount:  1,
		Modules: []drift.ModuleDrift{
			{Module: "root", Resources: []drift.ResourceDrift{{
				Address: "aws_db_instance.main",
				Action:  "update",
				Attributes: []drift.AttrChange{
					{Path: "password", Before: drift.SensitiveMask, After: drift.SensitiveMask, Sensitive: true},
					{Path: "instance_class", Before: "db.t3.micro", After: "db.t3.small"},
				},
			}}},
			{Module: "module.net", Resources: []drift.ResourceDrift{{
				Address: `module.net.aws_eip.nat["a<b>&c"]`,
				Action:  "delete",
			}}},
		},
	}
}

func TestSlackText(t *testing.T) {
	got := SlackText(driftReport())
	want := ":rotating_light: *Terraform drift: 2 resource(s)* (1 ignored)\n" +
		"• `aws_db_instance.main` (update): password, instance_class\n" +
		"• `module.net.aws_eip.nat[\"a&lt;b&gt;&amp;c\"]` (delete)"
	if got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
	if strings.Contains(got, "db.t3") {
		t.Error("message must not carry attribute values")
	}
}

func TestSlackTextCaps(t *testing.T) {
	r := &drift.Report{DriftDetected: true, ResourceCount: slackMaxResources + 3}
	var res []drift.ResourceDrift
	for i := range r.ResourceCount {
		res = append(res, drift.ResourceDrift{Address: fmt.Sprintf("null_resource.r%d", i), Action: "update"})
	}
	var attrs []drift.AttrChange
	for i := range slackMaxPaths + 2 {
		attrs = append(attrs, drift.AttrChange{Path: fmt.Sprintf("a%d", i)})
	}
	res[0].Attributes = attrs
	r.Modules = []drift.ModuleDrift{{Module: "root", Resources: res}}

	got := SlackText(r)
	if !strings.HasSuffix(got, "\n…and 3 more") {
		t.Errorf("resource list not capped: %q", got[len(got)-40:])
	}
	if strings.Count(got, "\n• ") != slackMaxResources {
		t.Errorf("listed %d resources, want %d", strings.Count(got, "\n• "), slackMaxResources)
	}
	if !strings.Contains(got, "a9, +2 more\n") {
		t.Errorf("path list not capped: %q", strings.SplitN(got, "\n", 3)[1])
	}
}

func TestSlackNotifyPosts(t *testing.T) {
	var body map[string]string
	var ctype string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctype = r.Header.Get("Content-Type")
		data, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(data, &body)
	}))
	defer srv.Close()

	s, err := NewSlack(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Notify(context.Background(), driftReport()); err != nil {
		t.Fatalf("notify: %v", err)
	}
	if ctype != "application/json" {
		t.Errorf("content type = %q", ctype)
	}
	if !strings.HasPrefix(body["text"], ":rotating_light:") {
		t.Errorf("unexpected payload %v", body)
	}
}

func TestSlackNotifyNon2xx(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = io.WriteString(w, "invalid_token")
	}))
	defer srv.Close()

	s, _ := NewSlack(srv.URL)
	err := s.Notify(context.Background(), driftReport())
	if err == nil || !strings.Contains(err.Error(), "403") || !strings.Contains(err.Error(), "invalid_token") {
		t.Fatalf("expected 403 error, got %v", err)
	}
}

func TestSlackErrorHidesWebhook(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	hook := srv.URL + "/services/T000/B000/secret-token"
	srv.Close() // connection refused from here on

	s, err := NewSlack(hook)
	if err != nil {
		t.Fatal(err)
	}
	err = s.Notify(context.Background(), driftReport())
	if err == nil {
		t.Fatal("expected a connection error")
	}
	if strings.Contains(err.Error(), "secret-token") {
		t.Fatalf("error leaks the webhook: %v", err)
	}
}

func TestNewSlackRejectsPlainHTTP(t *testing.T) {
	for _, hook := range []string{"http://hooks.slack.com/services/x", "hooks.slack.com/services/x", "ftp://x/y", ""} {
		if _, err := NewSlack(hook); err == nil {
			t.Errorf("NewSlack(%q) accepted", hook)
		}
	}
	for _, hook := range []string{"https://hooks.slack.com/services/x", "http://127.0.0.1:8080/x", "http://localhost/x", "http://[::1]/x"} {
		if _, err := NewSlack(hook); err != nil {
			t.Errorf("NewSlack(%q): %v", hook, err)
		}
	}
}

func TestParseSpec(t *testing.T) {
	if _, err := ParseSpec("slack=https://hooks.slack.com/services/x"); err != nil {
		t.Errorf("slack url: %v", err)
	}

	t.Setenv("TEST_SLACK_HOOK", "https://hooks.slack.com/services/y")
	s, err := ParseSpec("slack=env:TEST_SLACK_HOOK")
	if err != nil {
		t.Fatalf("slack env: %v", err)
	}
	if s.(*Slack).webhook != "https://hooks.slack.com/services/y" {
		t.Errorf("env webhook not used")
	}

	for _, spec := range []string{"slack", "slack=", "teams=https://x", "slack=env:TEST_UNSET_HOOK_XYZ"} {
		if _, err := ParseSpec(spec); err == nil {
			t.Errorf("ParseSpec(%q) accepted", spec)
		}
	}
	_, err = ParseSpec("teams=https://example.com/secret-token")
	if err == nil || strings.Contains(err.Error(), "secret-token") {
		t.Errorf("error should not echo the target: %v", err)
	}
}

type recordSink struct {
	calls int
	err   error
}

func (r *recordSink) Name() string { return "rec" }
func (r *recordSink) Notify(context.Context, *drift.Report) error {
	r.calls++
	return r.err
}

func TestSendQuietOnCleanRun(t *testing.T) {
	sink := &recordSink{}
	clean := &drift.Report{IgnoredCount: 2}
	if err := Send(context.Background(), []Sink{sink}, clean); err != nil {
		t.Fatal(err)
	}
	if sink.calls != 0 {
		t.Fatalf("sink called %d time(s) on a clean run", sink.calls)
	}
}

func TestSendTriesEverySink(t *testing.T) {
	bad := &recordSink{err: errors.New("boom")}
	good := &recordSink{}
	err := Send(context.Background(), []Sink{bad, good}, driftReport())
	if err == nil || !strings.Contains(err.Error(), "notify rec: boom") {
		t.Fatalf("got %v", err)
	}
	if good.calls != 1 {
		t.Fatal("second sink was skipped after the first failed")
	}
}
