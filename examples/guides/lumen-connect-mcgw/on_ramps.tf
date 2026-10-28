resource "alkira_connector_multi_cloud_gateway" "aws" {
  name                   = "lumen-connect-aws-us-east-1"
  description            = "AWS on-ramp, us-east-1"
  cxp                    = "US-EAST"
  size                   = "SMALL"
  group                  = alkira_group.on_ramps.name
  billing_tag_ids        = [alkira_billing_tag.cloud.id]
  multi_cloud_gateway_id = data.alkira_multi_cloud_gateway.east.id
  loopback_prefix        = "100.64.20.0/26"

  segment_options {
    segment_id = alkira_segment.corp.id
  }

  instance {
    name           = "aws-use1-reston"
    on_ramp_region = "us-east-1 - US East (N. Virginia) - Reston"
  }
}

resource "alkira_connector_multi_cloud_gateway" "azure" {
  name                   = "lumen-connect-azure-eastus"
  description            = "Azure on-ramp, eastus"
  cxp                    = "US-EAST"
  size                   = "SMALL"
  group                  = alkira_group.on_ramps.name
  billing_tag_ids        = [alkira_billing_tag.cloud.id]
  multi_cloud_gateway_id = data.alkira_multi_cloud_gateway.east.id
  loopback_prefix        = "100.64.20.64/26"

  segment_options {
    segment_id = alkira_segment.corp.id
  }

  instance {
    name           = "azure-eastus-ashburn"
    on_ramp_region = "eastus - East US - Ashburn"
  }
}
