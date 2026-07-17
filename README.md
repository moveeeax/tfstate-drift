# tfstate-drift

> Spot infrastructure drift at a glance with a readable tree diff.

**Status:** 🚧 In development

## Overview

CLI that visualizes Terraform state drift as a tree/diff between actual and desired infrastructure.

## Features

- Run `terraform plan -refresh-only -json` and parse the change stream
- Group drifted resources by module in a colorized tree view
- Show attribute-level before/after diffs for each drifted resource
- Emit machine-readable JSON for CI consumption
- Non-zero exit when drift is detected (CI gate)

## Stack

Go 1.22, `cobra`, Terraform JSON plan stream, `lipgloss` for tree rendering.

## Usage

```bash
tfstate-drift scan --chdir ./infra --format tree
tfstate-drift scan --chdir ./infra --format json
```

## License

MIT
