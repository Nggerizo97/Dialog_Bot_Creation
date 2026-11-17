# Sets up this terminal for local Cognito (docs/cognito-setup.md), in one step:
# checks the AWS Emulator is running, points the AWS settings at it, creates the user
# pool and demo users, and sets studio-api's sign-in settings.
#
# Run it with "source", from bot_dialog_generator, then start studio-api in the same terminal:
#   source infra/local/cognito-env.sh
#   go run ./services/studio-api
#
# The emulator must already be running in a terminal of its own:
#   EMULATOR_TELEMETRY=off aws-emulator --telemetry off

if [ "${BASH_SOURCE[0]}" = "$0" ]; then
  echo "Run this with: source infra/local/cognito-env.sh (otherwise the settings vanish when it ends)." >&2
  exit 1
fi

_bdg_cognito_env() {
  local port="${COGNITO_EMULATOR_PORT:-9297}"
  local dir="$HOME/.aws/emulator/$port"

  if [ ! -f go.mod ] || [ ! -d tools/cognito-local ]; then
    echo "Run this from the bot_dialog_generator folder." >&2
    return 1
  fi
  if ! command -v go >/dev/null 2>&1; then
    echo "Go is not installed in this terminal. Install Go 1.25 or later (for example: sudo snap install go --classic)." >&2
    return 1
  fi
  if [ ! -f "$dir/ca.pem" ] || ! curl -sf --cacert "$dir/ca.pem" "https://127.0.0.1:$port/__admin/health" >/dev/null; then
    echo "The AWS Emulator is not running on port $port." >&2
    echo "Start it in another terminal and leave that terminal open:" >&2
    echo "  EMULATOR_TELEMETRY=off aws-emulator --telemetry off" >&2
    return 1
  fi

  # Real credentials in the environment would win over the emulator's, and be rejected.
  unset AWS_ACCESS_KEY_ID AWS_SECRET_ACCESS_KEY AWS_SESSION_TOKEN AWS_PROFILE
  export AWS_ENDPOINT_URL="https://127.0.0.1:$port"
  export AWS_SHARED_CREDENTIALS_FILE="$dir/emulator_credentials"
  export AWS_CONFIG_FILE="$dir/config"
  export AWS_CA_BUNDLE="$dir/ca.pem"
  export AWS_EC2_METADATA_DISABLED=true
  export AWS_REGION="${AWS_REGION:-us-east-1}"

  local settings
  settings="$(go run ./tools/cognito-local -write-env -exports)" || return 1
  eval "$settings"
  echo "This terminal is set for Cognito (AUTH_MODE=$AUTH_MODE). Next:"
  echo "  go run ./services/studio-api        # here; its startup line should say auth: cognito"
  echo "  npm --prefix apps/studio-web run dev:cognito    # in another terminal (restart it if it is already running)"
}

_bdg_cognito_env
