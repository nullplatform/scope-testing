# Backend override for Azure Mock testing
# This configures the azurerm backend to use the mock blob storage

terraform {
  backend "azurerm" {
    # These values are overridden at runtime via -backend-config flags
    # but we need a backend block for terraform to accept them
  }
}
