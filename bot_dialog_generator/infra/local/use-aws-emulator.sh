#!/usr/bin/env sh
# Source this file (`source infra/local/use-aws-emulator.sh [port]`) to point the AWS CLI and SDKs at AWS Emulator.
PORT="${1:-9297}"
EMULATOR_DIR="${HOME}/.aws/emulator/${PORT}"

mkdir -p "${HOME}/.aws/emulator"

export AWS_ENDPOINT_URL="https://127.0.0.1:${PORT}"
export AWS_SHARED_CREDENTIALS_FILE="${EMULATOR_DIR}/emulator_credentials"
export AWS_CONFIG_FILE="${EMULATOR_DIR}/config"
export AWS_CA_BUNDLE="${EMULATOR_DIR}/ca.pem"
export SSL_CERT_FILE="${EMULATOR_DIR}/ca.pem"
export NODE_EXTRA_CA_CERTS="${EMULATOR_DIR}/ca.pem"
export AWS_EC2_METADATA_DISABLED="true"
export AWS_DISABLE_HOST_PREFIX_INJECTION="true"
export AWS_REGION="us-east-1"
export AWS_DEFAULT_REGION="us-east-1"
export EMULATOR_TELEMETRY="off"

unset AWS_ACCESS_KEY_ID AWS_SECRET_ACCESS_KEY AWS_SESSION_TOKEN AWS_PROFILE

echo "Configured shell for AWS Emulator at ${AWS_ENDPOINT_URL} (credentials & CA in ${EMULATOR_DIR})"
