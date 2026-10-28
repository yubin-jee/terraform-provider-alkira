# AGENTS.md

Notes for agents working on `terraform-provider-alkira`, focused on the Lumen Connect
(Multi-Cloud Gateway) work. Base branch is `dev`; release fixes are cherry-picked onto
`release/*` branches.

## Build the provider

```sh
go build -o bin/terraform-provider-alkira .     # or: sandbox/sandbox.sh build
```

Go version comes from `go.mod`; dependencies are vendored, so no network access is needed.

## Unit tests

```sh
go test ./alkira/...        # ~2 min, must be green before opening a PR
go test ./sandbox/          # sandbox's own tests, <1 s
```

`make test` also runs `go fmt alkira/*.go` first. Run `gofmt -l alkira sandbox` before committing.

## Local Alkira API sandbox

`sandbox/` is a small, in-memory stand-in for the Alkira REST API (Go stdlib plus the vendored
`alkira-client-go` structs). It covers what the provider calls for `alkira_multi_cloud_gateway`,
`alkira_connector_multi_cloud_gateway`, `alkira_segment`, `alkira_group` and `alkira_billing_tag`,
and returns real 400s for invalid input (bad tier, invalid BAN, unknown on-ramp region, duplicate
names, overlapping loopback prefixes, deleting a gateway that still has connectors, and so on).
It is not the Alkira portal and holds no real data.

```sh
sandbox/sandbox.sh up                    # API https://127.0.0.1:8443, page http://localhost:8090
sandbox/sandbox.sh up -seed-external     # also seed a read-only gateway "created in Lumen Connect"
sandbox/sandbox.sh reset                 # wipe state without restarting
```

- Run `up` in its own terminal; it stays in the foreground. State lives only in memory.
- A self-signed cert is generated once into `sandbox/.data/` (git-ignored). Terraform trusts it via
  `SSL_CERT_FILE`, which the helper sets.
- New gateways stay `PROVISIONING` for 3 s (`-provision-delay`), so the provider's wait loop runs.
- Accepted on-ramp regions: `GET https://127.0.0.1:8443/sandbox/on-ramp-regions` (see `sandbox/catalog.go`).
- http://localhost:8090 is a read-only Lumen Connect page that lists every object and field live,
  plus the API calls the provider made. Use it for before/after screen recordings.

## terraform plan / apply against the sandbox

```sh
sandbox/sandbox.sh build
sandbox/sandbox.sh tf init        # with dev_overrides, init only warns; plan/apply work without it
sandbox/sandbox.sh tf plan
sandbox/sandbox.sh tf apply -auto-approve
sandbox/sandbox.sh tf destroy -auto-approve
```

`tf` runs Terraform in `sandbox/examples/lumen-mcgw/` (one MCGW with a billing account number,
plus AWS `us-east-1` and Azure `eastus` on-ramp connectors). It writes `sandbox/.data/dev.tfrc` with a
`dev_overrides` entry pointing `alkiranet/alkira` at `./bin`, and sets `TF_CLI_CONFIG_FILE`,
`SSL_CERT_FILE`, `ALKIRA_PORTAL=127.0.0.1:8443` and `ALKIRA_API_KEY`. To use another directory, run
`SANDBOX_EXAMPLE=/path/to/dir sandbox/sandbox.sh tf plan`, or `eval "$(sandbox/sandbox.sh env)"`.
Rebuild the provider after every code change; Terraform loads `bin/terraform-provider-alkira` directly.

Import round-trip example:

```sh
sandbox/sandbox.sh tf import alkira_multi_cloud_gateway.lumen <id>
sandbox/sandbox.sh tf plan -detailed-exitcode   # exit 0 = no diff
```

## Commits and secrets

The repo has a gitleaks pre-commit hook (`.pre-commit-config.yaml`). Install it with
`pip install pre-commit && pre-commit install`. Never bypass it with `--no-verify`. Never commit
real Alkira credentials, tfstate, or anything under `sandbox/.data/`. The sandbox accepts any API
key, so the helper uses a dummy one.

## Docs

Resource and data source docs in `docs/` are generated from `templates/` and `examples/`:

```sh
go install github.com/hashicorp/terraform-plugin-docs/cmd/tfplugindocs@latest
tfplugindocs generate
```
