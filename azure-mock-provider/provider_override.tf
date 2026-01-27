# Override file for Azure Mock testing
# This file is copied into the module directory during integration tests
# to configure the Azure provider to use mock endpoints
#
# This is analogous to the LocalStack provider override for AWS tests.
#
# Azure Mock (port 8080): ARM APIs (CDN, DNS, Storage) + Blob Storage API

provider "azurerm" {
  features {}

  # Test subscription ID (mock doesn't validate this)
  subscription_id = "mock-subscription-id"

  # Skip provider registration (not needed for mock)
  skip_provider_registration = true

  # Use client credentials with mock values
  # The mock server accepts any credentials
  client_id       = "mock-client-id"
  client_secret   = "mock-client-secret"
  tenant_id       = "mock-tenant-id"

  # Disable all authentication methods except client credentials
  use_msi  = false
  use_cli  = false
  use_oidc = false

  default_tags {
    tags = var.resource_tags
  }
}