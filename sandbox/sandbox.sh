#!/usr/bin/env bash
# Helper for the Lumen Connect sandbox. Run from anywhere; paths resolve to the repo.
#   sandbox/sandbox.sh build          build bin/terraform-provider-alkira and bin/sandbox
#   sandbox/sandbox.sh up             run the sandbox in the foreground (API :8443, page :8090)
#   sandbox/sandbox.sh tf <args...>   terraform in sandbox/examples/lumen-mcgw against the sandbox
#   sandbox/sandbox.sh env            print the exports to use terraform yourself in any directory
#   sandbox/sandbox.sh reset          wipe all sandbox state
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
DATA="$ROOT/sandbox/.data"
API="${SANDBOX_API_ADDR:-127.0.0.1:8443}"
EXAMPLE="${SANDBOX_EXAMPLE:-$ROOT/sandbox/examples/lumen-mcgw}"

write_env() {
  mkdir -p "$DATA"
  cat > "$DATA/dev.tfrc" <<TFRC
provider_installation {
  dev_overrides {
    "alkiranet/alkira" = "$ROOT/bin"
  }
  direct {}
}
TFRC
  export TF_CLI_CONFIG_FILE="$DATA/dev.tfrc"
  export SSL_CERT_FILE="$DATA/sandbox-cert.pem"
  export ALKIRA_PORTAL="$API"
  export ALKIRA_API_KEY="local-sandbox"
}

case "${1:-}" in
  build)
    cd "$ROOT"
    go build -o bin/terraform-provider-alkira .
    go build -o bin/sandbox ./sandbox
    echo "built bin/terraform-provider-alkira and bin/sandbox"
    ;;
  up)
    cd "$ROOT"
    [ -x bin/sandbox ] || go build -o bin/sandbox ./sandbox
    exec bin/sandbox -api-addr "$API" -data-dir "$DATA" "${@:2}"
    ;;
  tf)
    write_env
    cd "$EXAMPLE"
    exec terraform "${@:2}"
    ;;
  env)
    write_env
    echo "export TF_CLI_CONFIG_FILE=$TF_CLI_CONFIG_FILE SSL_CERT_FILE=$SSL_CERT_FILE ALKIRA_PORTAL=$ALKIRA_PORTAL ALKIRA_API_KEY=$ALKIRA_API_KEY"
    ;;
  reset)
    write_env
    curl -fsS --cacert "$SSL_CERT_FILE" -X POST -H "Authorization: api-key $ALKIRA_API_KEY" "https://$API/sandbox/reset"
    ;;
  *)
    sed -n '2,8p' "$0"
    exit 1
    ;;
esac
