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
