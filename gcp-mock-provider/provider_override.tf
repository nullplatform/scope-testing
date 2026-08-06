# Override file for GCP mock testing.
# Copied into the composed module directory during integration tests to point the google
# providers at the gcp-mock server instead of real Google Cloud.
#
# This is the GCP analogue of localstack-provider (AWS) and azure-mock-provider (Azure).
# Being an override file (name ends in _override.tf), it may only override blocks that the
# base configuration already declares — it must NOT declare new variables.
#
# - A static access token bypasses ADC / OAuth token exchange (the mock ignores auth).
# - Per-service *_custom_endpoint values route each API to the gcp-mock container
#   (service name "gcp-mock" on the integration docker network).
# - Cloud Storage (function source + tofu state backend) is served by fake-gcs-server via
#   the STORAGE_EMULATOR_HOST env var set on the test runner, so no storage endpoint here.
#
# NOTE on endpoint formats — the version segment lives in a different place per service in
# the google client libraries (verified end-to-end against gcp-mock with `tofu apply`):
#   compute/cloudfunctions2/cloud_run include the version in the endpoint; iam appends its
#   own "v1/"; dns appends its own "dns/v1/", so its endpoint is the bare server root.

provider "google" {
  access_token = "mock-token"

  compute_custom_endpoint         = "http://gcp-mock:8080/compute/v1/"
  cloudfunctions2_custom_endpoint = "http://gcp-mock:8080/cloudfunctions/v2/"
  cloud_run_custom_endpoint       = "http://gcp-mock:8080/run/v1/"
  iam_custom_endpoint             = "http://gcp-mock:8080/iam/"
  dns_custom_endpoint             = "http://gcp-mock:8080/"
}

provider "google-beta" {
  access_token = "mock-token"

  compute_custom_endpoint         = "http://gcp-mock:8080/compute/v1/"
  cloudfunctions2_custom_endpoint = "http://gcp-mock:8080/cloudfunctions/v2/"
  cloud_run_custom_endpoint       = "http://gcp-mock:8080/run/v1/"
  iam_custom_endpoint             = "http://gcp-mock:8080/iam/"
  dns_custom_endpoint             = "http://gcp-mock:8080/"
}
