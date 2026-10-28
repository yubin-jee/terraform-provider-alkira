terraform {
  required_providers {
    alkira = {
      source = "alkiranet/alkira"
    }
  }
}

# portal and api_key are read from ALKIRA_PORTAL and ALKIRA_API_KEY.
provider "alkira" {}

variable "billing_account_number" {
  description = "Lumen billing account number (BAN) the gateway bills to."
  type        = string
  default     = "5-LMNCX0042"
}
