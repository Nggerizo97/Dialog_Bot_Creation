param(
    [int]$Port = 9297
)

$emulatorRoot = Join-Path $env:USERPROFILE ".aws\emulator"
$portDir = Join-Path $emulatorRoot "$Port"

New-Item -ItemType Directory -Force $emulatorRoot | Out-Null

$env:AWS_ENDPOINT_URL = "https://127.0.0.1:$Port"
$env:AWS_SHARED_CREDENTIALS_FILE = Join-Path $portDir "emulator_credentials"
$env:AWS_CONFIG_FILE = Join-Path $portDir "config"
$env:AWS_CA_BUNDLE = Join-Path $portDir "ca.pem"
$env:SSL_CERT_FILE = Join-Path $portDir "ca.pem"
$env:NODE_EXTRA_CA_CERTS = Join-Path $portDir "ca.pem"
$env:AWS_EC2_METADATA_DISABLED = "true"
$env:AWS_DISABLE_HOST_PREFIX_INJECTION = "true"
$env:AWS_REGION = "us-east-1"
$env:AWS_DEFAULT_REGION = "us-east-1"
$env:EMULATOR_TELEMETRY = "off"

# Unset any variables that could supply real AWS credentials (see AWS Emulator User Guide).
Remove-Item Env:AWS_ACCESS_KEY_ID, Env:AWS_SECRET_ACCESS_KEY, Env:AWS_SESSION_TOKEN, Env:AWS_PROFILE -ErrorAction SilentlyContinue

Write-Host "Configured shell for AWS Emulator at $($env:AWS_ENDPOINT_URL) (credentials & CA in $portDir)"
