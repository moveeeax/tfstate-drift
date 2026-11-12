# tfstate-drift

[![ci](https://github.com/moveeeax/tfstate-drift/actions/workflows/ci.yml/badge.svg)](https://github.com/moveeeax/tfstate-drift/actions/workflows/ci.yml)
[![Go](https://img.shields.io/badge/go-1.26%2B-00ADD8?logo=go)](https://go.dev/)
[![License: MIT](https://img.shields.io/badge/license-MIT-green.svg)](LICENSE)

> Spot infrastructure drift at a glance with a readable tree diff.

`tfstate-drift` runs a Terraform **refresh-only** plan, extracts the resources whose
real-world state no longer matches what Terraform recorded, and prints them as a
colorized tree grouped by module — with attribute-level before/after diffs. It also
emits schema-stable JSON for CI and **exits non-zero when drift exists**, so you can
gate a pipeline on it.

## How it works

1. `tfstate-drift scan` runs `terraform plan -refresh-only -out <tmp>` in the working
   directory, then `terraform show -json <tmp>` to get the full plan document.
2. The `resource_drift` section is parsed into typed structs.
3. Each drifted resource is grouped by module; before/after attribute maps are flattened
   to dotted paths (`tags.Env`, `ingress[0].from_port`) and diffed.
4. Output is rendered as a tree (humans) or JSON (CI). Exit code is `2` when any drift is
   found, `0` when clean, `1` on an operational error.

All Terraform access sits behind a `PlanProvider` interface, so the detection and
rendering logic is unit-tested against JSON fixtures with no cloud or network access.

## Install

Requires Go 1.26 or newer.

```bash
go install github.com/moveeeax/tfstate-drift@latest
```

Or build from source:

```bash
git clone https://github.com/moveeeax/tfstate-drift
cd tfstate-drift
make build
```

## Usage

```bash
# Human-readable tree (default)
tfstate-drift scan --chdir ./infra --format tree

# Machine-readable JSON for CI
tfstate-drift scan --chdir ./infra --format json

# Report against a pre-generated plan (no terraform needed — great for CI/demos)
tfstate-drift scan --plan-json plan.json --format tree
```

`--plan-json` accepts the output of `terraform show -json <planfile>`, letting you
separate the (privileged) plan step from the (safe) reporting step.

OpenTofu or a pinned binary: `--terraform-bin tofu` (or a path) picks the executable
for both `scan` and `workspaces`.

### Tree output

```text
tfstate-drift: 3 resource(s) drifted

root
└─ aws_instance.web (update)
    ~ instance_type: "t3.micro" → "t3.small"
    ~ monitoring: false → true
    ~ tags.Env: "dev" → "prod"

module.network
├─ module.network.aws_eip.nat (delete)
│   ~ id: "eipalloc-0aa11bb" → null
│   ~ public_ip: "203.0.113.10" → null
└─ module.network.aws_security_group.web (update)
    ~ ingress[0].from_port: 22 → 2222
    ~ ingress[0].to_port: 22 → 2222
```

### JSON output

```json
{
  "drift_detected": true,
  "resource_count": 3,
  "ignored_count": 0,
  "modules": [
    {
      "module": "root",
      "resources": [
        {
          "address": "aws_instance.web",
          "type": "aws_instance",
          "name": "web",
          "action": "update",
          "attributes": [
            { "path": "instance_type", "before": "t3.micro", "after": "t3.small" }
          ]
        }
      ]
    }
  ]
}
```

The JSON field names are a stable contract: fields are only ever added, never renamed
or removed.

### Sensitive values

Attributes that Terraform marks sensitive (`before_sensitive` / `after_sensitive` in
the plan JSON) are masked in both outputs. The change is still reported, so drift on a
password still fails the gate, but the values print as `(sensitive)` and the JSON entry
gains `"sensitive": true`:

```json
{ "path": "password", "before": "(sensitive)", "after": "(sensitive)", "sensitive": true }
```

A `null` side stays `null`, since it only says the value appeared or disappeared.
`--show-sensitive` prints the real values (still flagged `"sensitive": true`); keep it
out of CI jobs whose logs or artifacts other people can read.

### Ignoring known-noisy drift

Some attributes drift by design: an autoscaler moves `desired_capacity`, a tool stamps
`tags.LastModified`. Ignore them so the gate stays trustworthy:

```bash
tfstate-drift scan --chdir ./infra \
  --ignore 'aws_autoscaling_group.*:desired_capacity' \
  --ignore '*:tags.LastModified'
```

Rules can also live in a `.tfdriftignore` file in `--chdir` (it is read on every run,
`--plan-json` included; `--ignore` flags add to it):

```text
# one rule per line; blank lines and # comments are skipped
aws_autoscaling_group.*:desired_capacity
*:tags.LastModified
module.legacy.*
```

Each rule is `<address glob>[:<attr path glob>]`:

- The address glob matches the full resource address. The attribute glob matches the
  flattened path (`tags.Env`, `ingress[0].from_port`) or any parent of it, so `tags`
  covers every tag.
- `*` matches any run of characters, dots included, and `?` matches one character.
  Everything else is literal, so `aws_instance.web[0]` and `aws_s3_bucket.b["k"]` need
  no escaping. `aws_instance.*` does not match `module.app.aws_instance.web`; use
  `*aws_instance.*` for every module.
- The rule splits at the first `:` outside `[...]`, so colons inside for_each keys and
  tag names (`tags.aws:cloudformation:stack-name`) are fine.
- A rule without an attribute glob ignores the whole resource. A resource left with no
  attribute changes after filtering is dropped too. Dropped resources are counted in
  `ignored_count` and in the tree header, and they do not trigger exit code `2`.

### Slack notifications

`--notify slack=<webhook url>` posts a summary to a Slack
[incoming webhook](https://api.slack.com/messaging/webhooks) when drift is found.
Clean runs post nothing, and drift dropped by ignore rules does not count.
`slack=env:NAME` reads the URL from the environment variable `NAME`, which keeps it
out of shell history and `ps` output:

```bash
SLACK_WEBHOOK_URL=https://hooks.slack.com/services/... \
  tfstate-drift scan --chdir ./infra --notify slack=env:SLACK_WEBHOOK_URL
```

The message lists each drifted resource with its action and the attribute paths that
changed (up to 50 resources and 10 paths each). Attribute values are never sent, so
nothing from the state reaches the channel. The webhook must be `https`. A failed post
exits `1` and the error never contains the webhook URL.

### Scheduled drift check

Drift happens between applies, so run the check on a timer instead of waiting for the
next PR. The job needs read access to the backend and the providers' APIs:

```yaml
name: drift
on:
  schedule:
    - cron: '0 6 * * *'   # daily
  workflow_dispatch:
jobs:
  drift:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v7
      - uses: actions/setup-go@v7
        with:
          go-version: stable
      - uses: hashicorp/setup-terraform@v4
        with:
          terraform_wrapper: false
      - run: terraform -chdir=env/prod init -input=false
      - run: |
          go install github.com/moveeeax/tfstate-drift@latest
          tfstate-drift scan --chdir env/prod --no-color --notify slack=env:SLACK_WEBHOOK_URL
        env:
          SLACK_WEBHOOK_URL: ${{ secrets.SLACK_WEBHOOK_URL }}
```

The job fails (exit `2`) and posts to Slack when drift exists, and stays quiet otherwise.

## Workspace inventory

`tfstate-drift workspaces` lists every workspace of the backend configured in each
`--chdir`, oldest state first, so environments nobody has touched in months show up
before the invoice does:

```bash
tfstate-drift workspaces --chdir ./infra --chdir ./platform --stale-after 90d --refresh --format csv
```

```text
tfstate-drift: 3 workspace(s), 1 stale, 1 drifted, 0 unreachable

WORKSPACE  BACKEND  LAST WRITE  AGE         SERIAL  RESOURCES  PROVIDERS      STATUS
demo       s3       2026-03-01  256d stale  7       3          hashicorp/aws  clean
staging    s3       2026-10-30  13d         41      12         hashicorp/aws  drifted (2)
prod       s3       2026-11-11  1d          310     57         hashicorp/aws  clean

demo
      2  aws_instance
      1  aws_eip
...
```

Workspaces are listed with `terraform workspace list` and read with
`terraform state pull`, so every backend Terraform supports works (s3, gcs, azurerm,
remote, cloud, local) with the credentials Terraform already uses. Each directory must
be initialized. The tool sets `TF_WORKSPACE` per command, so the workspace selected in
the directory does not change, and it never writes state.

Per workspace:

- `last_write`: when the state object was last written (apply, import, `state mv`,
  refresh-only apply). Read from the object's `LastModified` for **s3** (one
  `HeadObject` call, the same `s3:GetObject` permission as `state pull`, using the
  backend's `region`, `profile`, `endpoints.s3` and `use_path_style`; `assume_role` is
  not replayed) and from the file's modification time for **local**. Other backends
  report `null` and sort after the dated ones. A failed lookup is reported in
  `last_write_error`.
- `stale`: last write older than `--stale-after` (default `90d`; accepts `36h` style
  durations, `0` disables).
- `serial`, `lineage`, `terraform_version`, `providers` from the state.
- `resource_count` and `resources_by_type`: managed resource instances (one per
  `count`/`for_each` key); data sources are not counted.
- `status`: `unchecked` without `--refresh`; with it, a refresh-only plan per workspace
  sets `clean`, `drifted` (with `drift_count`, after ignore rules) or `unreachable`
  (state pull or plan failed, reason in `error`).

`--format` is `table` (default), `json` (stable field names, like `scan`) or `csv` with
columns `dir,backend,workspace,last_write,age_days,stale,serial,lineage,terraform_version,resource_count,resources_by_type,providers,status,drift_count,error`.
In CSV, `resources_by_type` is `type=count` pairs and `providers` is a list, both split
by `;`, and cells starting with `=`, `+`, `-` or `@` get a leading `'` so spreadsheets
do not run them as formulas.

Exit code is `2` when `--refresh` finds a drifted workspace, `1` when a directory's
workspaces cannot be listed, `0` otherwise (stale and unreachable workspaces are
reported, not failed).

## Flags

### `scan`

| Flag          | Default  | Description                                              |
| ------------- | -------- | -------------------------------------------------------- |
| `--chdir`     | `.`      | Terraform working directory                              |
| `--format`    | `tree`   | Output format: `tree` or `json`                          |
| `--plan-json` | *(none)* | Read a `terraform show -json` file instead of running TF |
| `--no-color`  | `false`  | Disable ANSI colors in tree output                       |
| `--ignore`    | *(none)* | Ignore drift matching `<address glob>[:<attr glob>]`; repeatable, adds to `.tfdriftignore` |
| `--show-sensitive` | `false` | Print values Terraform marks sensitive instead of `(sensitive)` |
| `--notify`    | *(none)* | `slack=<webhook url>` or `slack=env:NAME`; posts only when drift is found; repeatable |
| `--terraform-bin` | `terraform` | Executable to run, e.g. `tofu`                        |

### `workspaces`

| Flag              | Default     | Description                                                  |
| ----------------- | ----------- | ------------------------------------------------------------ |
| `--chdir`         | `.`         | Initialized working directory; repeatable                    |
| `--format`        | `table`     | `table`, `json` or `csv`                                     |
| `--refresh`       | `false`     | Run a refresh-only plan per workspace                        |
| `--stale-after`   | `90d`       | Age after which a workspace is stale; `0` disables           |
| `--ignore`        | *(none)*    | Ignore rules for `--refresh`; adds to `.tfdriftignore` in each `--chdir` |
| `--terraform-bin` | `terraform` | Executable to run, e.g. `tofu`                               |

## Exit codes

For `scan` (and `workspaces --refresh`):

| Code | Meaning                     |
| ---- | --------------------------- |
| `0`  | No drift                    |
| `2`  | Drift detected (CI gate)    |
| `1`  | Operational error           |

## CI gate example

```yaml
- name: Fail on infrastructure drift
  run: tfstate-drift scan --chdir ./infra --format json
```

## Development

```bash
make fmt-check vet race build   # everything CI runs
make smoke                      # run against the bundled fixtures
```

## License

MIT — see [LICENSE](LICENSE).
