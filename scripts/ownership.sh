# Shared collection ownership for mutating lab helpers. Source this file and
# call `corepulse_hold <operation> "${BASH_SOURCE[0]}" "$@"` before the first
# Kubernetes mutation.
#
# Outside ownership, the helper re-executes itself under `corepulse hold`,
# which takes the same marker as CLI and dashboard collection (including
# COREPULSE_LOCK_DIR) and refuses while another owner holds it. Inside
# ownership, the helper confirms that the configured marker carries its token.
# There is no bypass: without a CorePulse binary the helper refuses.

corepulse_binary() {
  local root
  root="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
  if [[ -n "${COREPULSE_BIN:-}" ]]; then
    printf '%s\n' "$COREPULSE_BIN"
  elif command -v corepulse >/dev/null 2>&1; then
    command -v corepulse
  elif [[ -x "$root/corepulse" ]]; then
    printf '%s\n' "$root/corepulse"
  fi
}

corepulse_hold() {
  local operation="$1" script="$2" bin
  shift 2
  bin="$(corepulse_binary)"
  if [[ -z "$bin" || ! -x "$bin" ]]; then
    echo 'This helper needs a CorePulse binary to take shared collection ownership.' >&2
    echo 'Build one with `go build -o corepulse ./cmd/corepulse`, or set COREPULSE_BIN.' >&2
    exit 1
  fi
  if [[ -z "${COREPULSE_OWNERSHIP_TOKEN:-}" ]]; then
    exec "$bin" hold --operation "$operation" -- bash "$script" "$@"
  fi
  "$bin" ownership-check || {
    echo 'Shared collection ownership is not confirmed for this helper; refusing to change the lab.' >&2
    exit 1
  }
}
