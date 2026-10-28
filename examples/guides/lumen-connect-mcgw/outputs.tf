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
