# Lumen Connect: one Multi-Cloud Gateway with AWS and Azure cloud on-ramps.
# Runs against the local sandbox (sandbox/sandbox.sh), never a real Alkira portal.

terraform {
  required_providers {
    alkira = {
      source = "alkiranet/alkira"
    }
  }
}

# Credentials and portal come from ALKIRA_PORTAL / ALKIRA_API_KEY, which
# sandbox/sandbox.sh sets to the local sandbox.
provider "alkira" {
  provision = false
}

variable "billing_account_number" {
  description = "Lumen billing account number (BAN) the gateway bills to."
  type        = string
  default     = "5-LMNCX0042"
}

resource "alkira_segment" "corp" {
  name  = "lumen-corp"
  asn   = 65513
  cidrs = ["10.40.0.0/16"]
}

resource "alkira_group" "on_ramps" {
  name        = "lumen-cloud-on-ramps"
  description = "Cloud on-ramp connectors behind the Lumen MCGW"
}

resource "alkira_billing_tag" "cloud" {
  name        = "lumen-connect-cloud"
  description = "Cloud on-ramp spend"
}

resource "alkira_multi_cloud_gateway" "lumen" {
  name                   = "lumen-mcgw-east"
  tier                   = "10GBPS"
  description            = "Lumen Multi-Cloud Gateway for US East cloud on-ramps"
  billing_account_number = var.billing_account_number
}

resource "alkira_connector_multi_cloud_gateway" "aws" {
  name                   = "lumen-aws-on-ramp"
  description            = "AWS on-ramp, us-east-1"
  cxp                    = "US-EAST"
  size                   = "SMALL"
  group                  = alkira_group.on_ramps.name
  billing_tag_ids        = [alkira_billing_tag.cloud.id]
  multi_cloud_gateway_id = alkira_multi_cloud_gateway.lumen.id
  loopback_prefix        = "100.64.10.0/26"

  segment_options {
    segment_id = alkira_segment.corp.id
  }

  instance {
    name           = "aws-use1-reston"
    on_ramp_region = "us-east-1 - US East (N. Virginia) - Reston"
  }
}

resource "alkira_connector_multi_cloud_gateway" "azure" {
  name                   = "lumen-azure-on-ramp"
  description            = "Azure on-ramp, eastus"
  cxp                    = "US-EAST"
  size                   = "SMALL"
  group                  = alkira_group.on_ramps.name
  billing_tag_ids        = [alkira_billing_tag.cloud.id]
  multi_cloud_gateway_id = alkira_multi_cloud_gateway.lumen.id
  loopback_prefix        = "100.64.10.64/26"

  segment_options {
    segment_id = alkira_segment.corp.id
  }

  instance {
    name           = "azure-eastus-ashburn"
    on_ramp_region = "eastus - East US - Ashburn"
  }
}

# The connectors are created after the gateway, so the gateway resource's own
# connector_ids only lists them after the next refresh. Reading the gateway
# back once its connectors exist gives the attached IDs in the same apply.
data "alkira_multi_cloud_gateway" "lumen" {
  name = alkira_multi_cloud_gateway.lumen.name

  depends_on = [
    alkira_connector_multi_cloud_gateway.aws,
    alkira_connector_multi_cloud_gateway.azure,
  ]
}

output "gateway" {
  value = {
    id                     = alkira_multi_cloud_gateway.lumen.id
    native_id              = alkira_multi_cloud_gateway.lumen.native_id
    state                  = alkira_multi_cloud_gateway.lumen.state
    billing_account_number = alkira_multi_cloud_gateway.lumen.billing_account_number
    connector_ids          = data.alkira_multi_cloud_gateway.lumen.connector_ids
  }
}

output "on_ramps" {
  value = {
    aws   = alkira_connector_multi_cloud_gateway.aws.instance
    azure = alkira_connector_multi_cloud_gateway.azure.instance
  }
}
