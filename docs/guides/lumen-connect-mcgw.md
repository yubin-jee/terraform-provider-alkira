---
subcategory: "Guides"
page_title: "Lumen Connect - Multi-Cloud Gateway with cloud on-ramps"
description: |-
    End-to-end guide for deploying a Lumen Multi-Cloud Gateway with AWS and Azure on-ramp connectors
---

This guide builds a complete Lumen Connect deployment: a Lumen Multi-Cloud Gateway (MCGW) that
bills to a Lumen billing account, and two cloud on-ramp connectors, AWS in `us-east-1` and Azure
in `eastus`, attached to a segment.

## Concepts

Lumen Connect brings Lumen Multi-Cloud Gateway, cloud on-ramps, and on-net and off-net
connectivity together under the Alkira network model.

* **Multi-Cloud Gateway (MCGW)**: a Lumen-hosted gateway that cloud traffic enters and leaves
  through. It has a bandwidth `tier` and is billed to a Lumen billing account number (BAN),
  managed with [alkira_multi_cloud_gateway](https://registry.terraform.io/providers/alkiranet/alkira/latest/docs/resources/multi_cloud_gateway).
* **Cloud on-ramp**: a connection from the gateway into a cloud provider region at a Lumen
  site. Each `instance` of an
  [alkira_connector_multi_cloud_gateway](https://registry.terraform.io/providers/alkiranet/alkira/latest/docs/resources/connector_multi_cloud_gateway) picks one with
  `on_ramp_region`, written as `<cloud region> - <region name> - <Lumen site>`, for example
  `us-east-1 - US East (N. Virginia) - Reston`. The connector places its on-ramps in a segment,
  a group and a CXP, like any other connector.

## Provider configuration

The example reads `portal` and `api_key` from the `ALKIRA_PORTAL` and `ALKIRA_API_KEY`
environment variables, so it carries no credentials.

```terraform
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
```

## Segment, group and billing tag

On-ramp traffic lands in an [alkira_segment](https://registry.terraform.io/providers/alkiranet/alkira/latest/docs/resources/segment). The
[alkira_group](https://registry.terraform.io/providers/alkiranet/alkira/latest/docs/resources/group) collects the on-ramp connectors so policies can target them
together, and the [alkira_billing_tag](https://registry.terraform.io/providers/alkiranet/alkira/latest/docs/resources/billing_tag) tags their spend. If these
already exist, look them up with the matching data sources instead.

```terraform
resource "alkira_segment" "corp" {
  name  = "lumen-connect-corp"
  asn   = 65514
  cidrs = ["10.60.0.0/16"]
}

resource "alkira_group" "on_ramps" {
  name        = "lumen-connect-on-ramps"
  description = "Cloud on-ramp connectors behind the Lumen MCGW"
}

resource "alkira_billing_tag" "cloud" {
  name        = "lumen-connect-on-ramp-spend"
  description = "Cloud on-ramp spend"
}
```

## Multi-Cloud Gateway

`billing_account_number` is the Lumen BAN the gateway bills to. Changing it, or `name`, replaces
the gateway. Creation waits until the gateway reaches `PROVISIONED`.

The [alkira_multi_cloud_gateway](https://registry.terraform.io/providers/alkiranet/alkira/latest/docs/data-sources/multi_cloud_gateway) data source looks a gateway
up by its exact name. Here it reads back the gateway this configuration created. To attach
on-ramps to a gateway that already exists, for example one created in Lumen Connect, remove the
resource and the `depends_on` and set `name` to that gateway's name.

```terraform
resource "alkira_multi_cloud_gateway" "east" {
  name                   = "lumen-connect-mcgw-east"
  tier                   = "10GBPS"
  description            = "Lumen Multi-Cloud Gateway for US East cloud on-ramps"
  billing_account_number = var.billing_account_number
}

# Look the gateway up by name. Drop the resource above and the depends_on to
# attach on-ramps to a gateway that already exists in Lumen Connect.
data "alkira_multi_cloud_gateway" "east" {
  name = alkira_multi_cloud_gateway.east.name

  depends_on = [alkira_multi_cloud_gateway.east]
}
```

## AWS and Azure on-ramps

Each connector attaches to the gateway through `multi_cloud_gateway_id`. Its `instance` blocks
choose the on-ramp with `on_ramp_region`, and `loopback_prefix` must not overlap any other
connector's loopback prefix.

```terraform
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
```

## Outputs

```terraform
output "gateway" {
  value = {
    id                     = data.alkira_multi_cloud_gateway.east.id
    native_id              = data.alkira_multi_cloud_gateway.east.native_id
    state                  = data.alkira_multi_cloud_gateway.east.state
    billing_account_number = data.alkira_multi_cloud_gateway.east.billing_account_number
  }
}

output "on_ramp_connector_ids" {
  value = {
    aws   = alkira_connector_multi_cloud_gateway.aws.id
    azure = alkira_connector_multi_cloud_gateway.azure.id
  }
}
```

## Apply

```shell
export ALKIRA_PORTAL="your_tenant_name.portal.alkira.com"
export ALKIRA_API_KEY="your_api_key"
terraform init
terraform apply
```

A second `terraform plan` reports no changes. The full example is in
[examples/guides/lumen-connect-mcgw](https://github.com/alkiranet/terraform-provider-alkira/tree/dev/examples/guides/lumen-connect-mcgw).
