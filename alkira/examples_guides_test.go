package alkira

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclsyntax"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const lumenConnectGuideDir = "../examples/guides/lumen-connect-mcgw"

var terraformMetaArguments = map[string]bool{
	"count": true, "for_each": true, "provider": true, "depends_on": true, "lifecycle": true,
}

func parseExampleDir(t *testing.T, dir string) map[string]*hclsyntax.Body {
	t.Helper()
	files, err := filepath.Glob(filepath.Join(dir, "*.tf"))
	require.NoError(t, err)
	require.NotEmpty(t, files, "no .tf files in %s", dir)

	bodies := map[string]*hclsyntax.Body{}
	for _, f := range files {
		src, err := os.ReadFile(f)
		require.NoError(t, err)
		file, diags := hclsyntax.ParseConfig(src, f, hcl.InitialPos)
		require.False(t, diags.HasErrors(), "%s: %s", f, diags.Error())
		bodies[filepath.Base(f)] = file.Body.(*hclsyntax.Body)
	}
	return bodies
}

// checkBodyAgainstSchema reports attributes and blocks the schema does not
// define, and required attributes or blocks that are missing.
func checkBodyAgainstSchema(path string, body *hclsyntax.Body, s map[string]*schema.Schema, topLevel bool) []string {
	var problems []string
	seen := map[string]bool{}

	for name := range body.Attributes {
		seen[name] = true
		if topLevel && terraformMetaArguments[name] {
			continue
		}
		field, ok := s[name]
		if !ok {
			problems = append(problems, fmt.Sprintf("%s: unknown attribute %q", path, name))
			continue
		}
		if field.Computed && !field.Optional && !field.Required {
			problems = append(problems, fmt.Sprintf("%s: %q is read-only", path, name))
		}
	}
	for _, block := range body.Blocks {
		seen[block.Type] = true
		if topLevel && terraformMetaArguments[block.Type] {
			continue
		}
		field, ok := s[block.Type]
		if !ok {
			problems = append(problems, fmt.Sprintf("%s: unknown block %q", path, block.Type))
			continue
		}
		elem, ok := field.Elem.(*schema.Resource)
		if !ok {
			problems = append(problems, fmt.Sprintf("%s: %q is not a block", path, block.Type))
			continue
		}
		problems = append(problems, checkBodyAgainstSchema(path+"."+block.Type, block.Body, elem.Schema, false)...)
	}
	for name, field := range s {
		if field.Required && !seen[name] {
			problems = append(problems, fmt.Sprintf("%s: missing required %q", path, name))
		}
	}
	sort.Strings(problems)
	return problems
}

func checkExampleAgainstProvider(p *schema.Provider, bodies map[string]*hclsyntax.Body) []string {
	var problems []string
	for file, body := range bodies {
		for _, block := range body.Blocks {
			if len(block.Labels) < 2 || !strings.HasPrefix(block.Labels[0], "alkira_") {
				continue
			}
			var r *schema.Resource
			switch block.Type {
			case "resource":
				r = p.ResourcesMap[block.Labels[0]]
			case "data":
				r = p.DataSourcesMap[block.Labels[0]]
			default:
				continue
			}
			path := fmt.Sprintf("%s: %s.%s.%s", file, block.Type, block.Labels[0], block.Labels[1])
			if r == nil {
				problems = append(problems, path+": not defined by the provider")
				continue
			}
			problems = append(problems, checkBodyAgainstSchema(path, block.Body, r.Schema, true)...)
		}
	}
	return problems
}

func literalString(t *testing.T, attr *hclsyntax.Attribute) string {
	t.Helper()
	v, diags := attr.Expr.Value(nil)
	require.False(t, diags.HasErrors(), "%s is not a literal", attr.Name)
	return v.AsString()
}

func TestExamplesMatchProviderSchema(t *testing.T) {
	p := Provider()
	for _, dir := range []string{
		lumenConnectGuideDir,
		"../examples/data-sources/alkira_multi_cloud_gateway",
	} {
		t.Run(filepath.Base(dir), func(t *testing.T) {
			assert.Empty(t, checkExampleAgainstProvider(p, parseExampleDir(t, dir)))
		})
	}
}

func TestExampleSchemaCheckCatchesDrift(t *testing.T) {
	src := []byte(`
resource "alkira_multi_cloud_gateway" "bad" {
  name      = "x"
  tier      = "10GBPS"
  bogus     = true
  native_id = "read-only"
  depends_on = []
}

resource "alkira_connector_multi_cloud_gateway" "bad" {
  name = "x"
  instance {
    name   = "i"
    region = "wrong"
  }
}
`)
	file, diags := hclsyntax.ParseConfig(src, "drift.tf", hcl.InitialPos)
	require.False(t, diags.HasErrors())

	problems := strings.Join(checkExampleAgainstProvider(Provider(), map[string]*hclsyntax.Body{
		"drift.tf": file.Body.(*hclsyntax.Body),
	}), "\n")

	assert.Contains(t, problems, `alkira_multi_cloud_gateway.bad: unknown attribute "bogus"`)
	assert.Contains(t, problems, `alkira_multi_cloud_gateway.bad: "native_id" is read-only`)
	assert.Contains(t, problems, `alkira_connector_multi_cloud_gateway.bad: missing required "multi_cloud_gateway_id"`)
	assert.Contains(t, problems, `alkira_connector_multi_cloud_gateway.bad.instance: unknown attribute "region"`)
	assert.Contains(t, problems, `alkira_connector_multi_cloud_gateway.bad.instance: missing required "on_ramp_region"`)
	assert.NotContains(t, problems, "depends_on")
}

func TestLumenConnectGuideExampleCoversDeployment(t *testing.T) {
	bodies := parseExampleDir(t, lumenConnectGuideDir)

	resources := map[string][]*hclsyntax.Block{}
	var dataSources []string
	for _, body := range bodies {
		for _, block := range body.Blocks {
			switch block.Type {
			case "resource":
				resources[block.Labels[0]] = append(resources[block.Labels[0]], block)
			case "data":
				dataSources = append(dataSources, block.Labels[0])
			case "provider":
				for _, cred := range []string{"portal", "api_key", "username", "password"} {
					assert.NotContains(t, block.Body.Attributes, cred, "provider block must not carry %s", cred)
				}
			}
		}
	}

	for _, rt := range []string{"alkira_segment", "alkira_group", "alkira_billing_tag", "alkira_multi_cloud_gateway"} {
		assert.Len(t, resources[rt], 1, rt)
	}
	assert.Contains(t, dataSources, "alkira_multi_cloud_gateway")
	if gw := resources["alkira_multi_cloud_gateway"]; len(gw) == 1 {
		assert.Contains(t, gw[0].Body.Attributes, "billing_account_number")
	}

	var regions []string
	for _, c := range resources["alkira_connector_multi_cloud_gateway"] {
		gwRef := c.Body.Attributes["multi_cloud_gateway_id"]
		require.NotNil(t, gwRef)
		assert.Equal(t, "data.alkira_multi_cloud_gateway", hclTraversalPrefix(gwRef.Expr), "connector %s should use the data source", c.Labels[1])
		for _, inst := range c.Body.Blocks {
			if inst.Type == "instance" {
				regions = append(regions, strings.SplitN(literalString(t, inst.Body.Attributes["on_ramp_region"]), " - ", 2)[0])
			}
		}
	}
	sort.Strings(regions)
	assert.Equal(t, []string{"eastus", "us-east-1"}, regions)
}

func hclTraversalPrefix(expr hclsyntax.Expression) string {
	traversal, ok := expr.(*hclsyntax.ScopeTraversalExpr)
	if !ok || len(traversal.Traversal) < 2 {
		return ""
	}
	root := traversal.Traversal.RootName()
	if attr, ok := traversal.Traversal[1].(hcl.TraverseAttr); ok {
		return root + "." + attr.Name
	}
	return root
}

func TestLumenConnectGuideTemplateIncludesEveryExampleFile(t *testing.T) {
	tmpl, err := os.ReadFile("../templates/guides/lumen-connect-mcgw.md.tmpl")
	require.NoError(t, err)

	for file := range parseExampleDir(t, lumenConnectGuideDir) {
		assert.Contains(t, string(tmpl), fmt.Sprintf(`{{ tffile "examples/guides/lumen-connect-mcgw/%s" }}`, file))
	}
}
