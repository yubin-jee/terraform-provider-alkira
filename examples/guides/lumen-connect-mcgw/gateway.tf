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
