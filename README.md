# tfstate-drift

[![ci](https://github.com/moveeeax/tfstate-drift/actions/workflows/ci.yml/badge.svg)](https://github.com/moveeeax/tfstate-drift/actions/workflows/ci.yml)
[![Go](https://img.shields.io/badge/go-1.22%2B-00ADD8?logo=go)](https://go.dev/)
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

## Flags

| Flag          | Default  | Description                                              |
| ------------- | -------- | -------------------------------------------------------- |
| `--chdir`     | `.`      | Terraform working directory                              |
| `--format`    | `tree`   | Output format: `tree` or `json`                          |
| `--plan-json` | *(none)* | Read a `terraform show -json` file instead of running TF |
| `--no-color`  | `false`  | Disable ANSI colors in tree output                       |

## Exit codes

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
