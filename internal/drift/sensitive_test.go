package drift

import "testing"

const sensitivePlan = `{
  "format_version": "1.2",
  "resource_drift": [
    {
      "address": "aws_db_instance.main",
      "type": "aws_db_instance",
      "name": "main",
      "change": {
        "actions": ["update"],
        "before": {
          "password": "hunter2",
          "instance_class": "db.t3.micro",
          "secrets": {"api": "a1", "web": "w1"},
          "users": [{"name": "app", "token": "t1"}]
        },
        "after": {
          "password": "hunter3",
          "instance_class": "db.t3.small",
          "secrets": {"api": "a2", "web": "w2"},
          "users": [{"name": "app", "token": "t2"}]
        },
        "before_sensitive": {"password": true, "secrets": true, "users": [{"token": true}]},
        "after_sensitive": {"password": true, "secrets": true, "users": [{"token": true}]}
      }
    }
  ]
}`

func parseString(t *testing.T, s string) *Plan {
	t.Helper()
	plan, err := Parse([]byte(s))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	return plan
}

func attrsByPath(r *Report) map[string]AttrChange {
	out := map[string]AttrChange{}
	for _, m := range r.Modules {
		for _, res := range m.Resources {
			for _, a := range res.Attributes {
				out[res.Address+":"+a.Path] = a
			}
		}
	}
	return out
}

func TestDetectMasksSensitive(t *testing.T) {
	got := attrsByPath(Detect(parseString(t, sensitivePlan), Options{}))

	for _, path := range []string{"password", "secrets.api", "secrets.web", "users[0].token"} {
		a, ok := got["aws_db_instance.main:"+path]
		if !ok {
			t.Fatalf("expected change at %q", path)
		}
		if !a.Sensitive || a.Before != SensitiveMask || a.After != SensitiveMask {
			t.Errorf("%s: want masked sensitive change, got %+v", path, a)
		}
	}
	a := got["aws_db_instance.main:instance_class"]
	if a.Sensitive || a.Before != "db.t3.micro" || a.After != "db.t3.small" {
		t.Errorf("instance_class should stay visible, got %+v", a)
	}
}

func TestDetectShowSensitive(t *testing.T) {
	got := attrsByPath(Detect(parseString(t, sensitivePlan), Options{ShowSensitive: true}))
	a := got["aws_db_instance.main:password"]
	if !a.Sensitive || a.Before != "hunter2" || a.After != "hunter3" {
		t.Fatalf("--show-sensitive should keep values but flag them, got %+v", a)
	}
}

func TestMaskSensitiveKeepsNull(t *testing.T) {
	changes := []AttrChange{{Path: "password", Before: "x", After: nil}}
	maskSensitive(changes, Change{BeforeSensitive: map[string]any{"password": true}}, false)
	if changes[0].Before != SensitiveMask || changes[0].After != nil {
		t.Fatalf("got %+v", changes[0])
	}
}

func TestMaskSensitiveWholeResource(t *testing.T) {
	changes := []AttrChange{{Path: "a", Before: "1", After: "2"}}
	maskSensitive(changes, Change{AfterSensitive: true}, false)
	if !changes[0].Sensitive || changes[0].After != SensitiveMask {
		t.Fatalf("a bare true should mask every attribute, got %+v", changes[0])
	}
}

func TestMaskSensitiveFalseMarks(t *testing.T) {
	changes := []AttrChange{{Path: "a", Before: "1", After: "2"}}
	maskSensitive(changes, Change{BeforeSensitive: false, AfterSensitive: map[string]any{"a": false}}, false)
	if changes[0].Sensitive || changes[0].After != "2" {
		t.Fatalf("false marks must not mask, got %+v", changes[0])
	}
}
