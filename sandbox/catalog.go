package main

// Lumen on-ramp regions the sandbox accepts for connector instances. The
// format mirrors the API's "<cloud region> - <cloud name> - <Lumen POP>" string.
var onRampRegions = []onRampRegion{
	{Cloud: "AWS", Name: "us-east-1 - US East (N. Virginia) - Reston"},
	{Cloud: "AWS", Name: "us-east-1 - US East (N. Virginia) - Ashburn"},
	{Cloud: "AWS", Name: "us-east-2 - US East (Ohio) - Columbus"},
	{Cloud: "AWS", Name: "us-west-2 - US West (Oregon) - Hillsboro"},
	{Cloud: "AZURE", Name: "eastus - East US - Ashburn"},
	{Cloud: "AZURE", Name: "eastus - East US - Washington DC"},
	{Cloud: "AZURE", Name: "westus2 - West US 2 - Quincy"},
	{Cloud: "GCP", Name: "us-east4 - N. Virginia - Ashburn"},
}

type onRampRegion struct {
	Cloud string `json:"cloud"`
	Name  string `json:"name"`
}

var cxps = []string{"US-EAST", "US-EAST-2", "US-WEST", "US-WEST-1", "EU-CENTRAL"}

var mcgwTiers = []string{"10GBPS", "50GBPS", "UNLIMITED"}

var connectorSizeMbps = map[string]int{
	"SMALL": 1000, "MEDIUM": 2000, "LARGE": 5000, "2LARGE": 10000, "5LARGE": 20000, "10LARGE": 50000,
}

func isOnRampRegion(name string) bool {
	for _, r := range onRampRegions {
		if r.Name == name {
			return true
		}
	}
	return false
}

func contains(list []string, v string) bool {
	for _, s := range list {
		if s == v {
			return true
		}
	}
	return false
}
