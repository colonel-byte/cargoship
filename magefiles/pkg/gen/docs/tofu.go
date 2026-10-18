// Copyright 2026 colonel-byte
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package docs

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/colonel-byte/cargoship/internal/tofuprovider"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	dschema "github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	pschema "github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	rschema "github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/nao1215/markdown"
)

const (
	tofuDocsDir = "docs/tofu"
)

type docAttribute struct {
	Name        string
	Type        string
	Required    bool
	Optional    bool
	Computed    bool
	Sensitive   bool
	Description string
}

type docBlock struct {
	Name        string
	NestingMode string
	Description string
	Attributes  []docAttribute
	// Attribute marks a nested attribute rather than a nested block. The two are spelled
	// differently in a configuration -- `hosts = { k = { ... } }` against `host { ... }` -- so
	// they are headed differently in the reference.
	Attribute bool
}

type docSection struct {
	Required   []docAttribute
	Optional   []docAttribute
	ReadOnly   []docAttribute
	Blocks     []docBlock
	SubObjects map[string][]docAttribute
}

func generateTofuDocs() error {
	resDir := filepath.Join(tofuDocsDir, "resources")
	dsDir := filepath.Join(tofuDocsDir, "data-sources")
	for _, dir := range []string{tofuDocsDir, resDir, dsDir} {
		if err := os.MkdirAll(dir, 0o775); err != nil {
			return err
		}
	}

	p := tofuprovider.New("dev")()
	ctx := context.Background()

	var pResp provider.SchemaResponse
	p.Schema(ctx, provider.SchemaRequest{}, &pResp)

	resources := p.Resources(ctx)
	dataSources := p.DataSources(ctx)

	var rSchema rschema.Schema
	var rName string
	for _, rFunc := range resources {
		r := rFunc()
		var mResp resource.MetadataResponse
		r.Metadata(ctx, resource.MetadataRequest{ProviderTypeName: "cargoship"}, &mResp)
		var sResp resource.SchemaResponse
		r.Schema(ctx, resource.SchemaRequest{}, &sResp)
		rName = mResp.TypeName
		rSchema = sResp.Schema
	}

	var dSchema dschema.Schema
	var dName string
	for _, dFunc := range dataSources {
		d := dFunc()
		var mResp datasource.MetadataResponse
		d.Metadata(ctx, datasource.MetadataRequest{ProviderTypeName: "cargoship"}, &mResp)
		var sResp datasource.SchemaResponse
		d.Schema(ctx, datasource.SchemaRequest{}, &sResp)
		dName = mResp.TypeName
		dSchema = sResp.Schema
	}

	if err := writeProviderIndex(pResp.Schema); err != nil {
		return fmt.Errorf("writing provider index: %w", err)
	}
	if err := writeResourceDoc(rName, rSchema); err != nil {
		return fmt.Errorf("writing resource %s doc: %w", rName, err)
	}
	if err := writeDataSourceDoc(dName, dSchema); err != nil {
		return fmt.Errorf("writing data source %s doc: %w", dName, err)
	}

	return nil
}

func writeProviderIndex(s pschema.Schema) error {
	return writeGeneratedPage(tofuDocsDir, "index.md", func(doc *markdown.Markdown) error {
		doc.H1("cargoship Provider")
		doc.PlainText("")
		doc.PlainText(s.MarkdownDescription)
		doc.PlainText("")

		doc.H2("Example Usage")
		doc.PlainText("")
		doc.CodeBlocks("terraform", `# ~/.tofurc or provider configuration
provider "cargoship" {
  concurrency     = 0
  connect_timeout = "1m"
}`)
		doc.PlainText("")

		doc.H2("Schema")
		doc.PlainText("")

		attrs := extractDocAttributes(s.Attributes)
		writeAttributeTables(doc, attrs)

		return nil
	})
}

func writeResourceDoc(name string, s rschema.Schema) error {
	cleanName := strings.TrimPrefix(name, "cargoship_")
	filename := fmt.Sprintf("resources/%s.md", cleanName)

	return writeGeneratedPage(tofuDocsDir, filename, func(doc *markdown.Markdown) error {
		doc.H1(name)
		doc.PlainText("")
		doc.PlainText(s.MarkdownDescription)
		doc.PlainText("")

		doc.H2("Example Usage")
		doc.PlainText("")
		doc.CodeBlocks("terraform", `resource "cargoship_cluster" "prod" {
  name          = "prod-cluster"
  load_balancer = "10.0.0.10"
  package       = "/srv/staging/k3s-v1.33.4.tar.zst"

  profiles = {
    control = {
      node_labels = { "purpose-control" = "true" }
      ports       = [{ port = "6443" }]
      concurrency = "1"
    }
    worker = {
      node_labels = { "purpose-worker" = "true" }
    }
  }

  values = yamlencode({
    addons = {
      disabled = ["traefik"]
    }
  })

  hosts = {
    controller1 = {
      address  = "10.0.0.11"
      role     = "controller"
      key_path = "~/.ssh/id_ed25519"
      profile  = "control"
    }

    worker1 = {
      address  = "10.0.0.21"
      role     = "worker"
      key_path = "~/.ssh/id_ed25519"
      profile  = "worker"
    }
  }
}`)
		doc.PlainText("")

		doc.H2("Schema")
		doc.PlainText("")

		sec := extractResourceSection(s)
		renderSchemaSection(doc, sec)

		return nil
	})
}

func writeDataSourceDoc(name string, s dschema.Schema) error {
	cleanName := strings.TrimPrefix(name, "cargoship_")
	filename := fmt.Sprintf("data-sources/%s.md", cleanName)

	return writeGeneratedPage(tofuDocsDir, filename, func(doc *markdown.Markdown) error {
		doc.H1(name)
		doc.PlainText("")
		doc.PlainText(s.MarkdownDescription)
		doc.PlainText("")

		doc.H2("Example Usage")
		doc.PlainText("")
		doc.CodeBlocks("terraform", `data "cargoship_cluster_facts" "fleet" {
  name          = "fleet-facts"
  load_balancer = "10.0.0.10"
  distro        = "k3s"

  host {
    address  = "10.0.0.11"
    role     = "controller"
    key_path = "~/.ssh/id_ed25519"
  }
}

output "installed_versions" {
  value = { for n in data.cargoship_cluster_facts.fleet.nodes : n.hostname => n.engine_version }
}`)
		doc.PlainText("")

		doc.H2("Schema")
		doc.PlainText("")

		sec := extractDataSourceSection(s)
		renderSchemaSection(doc, sec)

		return nil
	})
}

func formatAttributeType(s string) string {
	s = strings.ReplaceAll(s, "basetypes.StringType", "String")
	s = strings.ReplaceAll(s, "basetypes.BoolType", "Boolean")
	s = strings.ReplaceAll(s, "basetypes.Int64Type", "Number")
	s = strings.ReplaceAll(s, "types.StringType", "String")
	s = strings.ReplaceAll(s, "types.BoolType", "Boolean")
	s = strings.ReplaceAll(s, "types.Int64Type", "Number")
	if strings.HasPrefix(s, "types.ListType[basetypes.StringType]") || strings.HasPrefix(s, "types.ListType[types.StringType]") {
		return "List of String"
	}
	if strings.HasPrefix(s, "types.MapType[basetypes.StringType]") || strings.HasPrefix(s, "types.MapType[types.StringType]") {
		return "Map of String"
	}
	if strings.HasPrefix(s, "types.ListType[") {
		return "List of Object"
	}
	if strings.HasPrefix(s, "types.MapType[") {
		return "Map of Object"
	}
	if strings.HasPrefix(s, "types.ObjectType[") {
		return "Object"
	}
	return s
}

func extractDocAttributes[A interface {
	GetType() attrType
	GetMarkdownDescription() string
	GetDescription() string
	IsRequired() bool
	IsOptional() bool
	IsComputed() bool
	IsSensitive() bool
}, attrType fmt.Stringer](attrs map[string]A) []docAttribute {
	var list []docAttribute
	for name, a := range attrs {
		desc := a.GetMarkdownDescription()
		if desc == "" {
			desc = a.GetDescription()
		}
		list = append(list, docAttribute{
			Name:        name,
			Type:        formatAttributeType(a.GetType().String()),
			Required:    a.IsRequired(),
			Optional:    a.IsOptional(),
			Computed:    a.IsComputed(),
			Sensitive:   a.IsSensitive(),
			Description: desc,
		})
	}
	sort.Slice(list, func(i, j int) bool {
		return list[i].Name < list[j].Name
	})
	return list
}

func extractResourceSection(s rschema.Schema) docSection {
	var sec docSection
	for name, a := range s.Attributes {
		desc := a.GetMarkdownDescription()
		if desc == "" {
			desc = a.GetDescription()
		}
		da := docAttribute{
			Name:        name,
			Type:        formatAttributeType(a.GetType().String()),
			Required:    a.IsRequired(),
			Optional:    a.IsOptional(),
			Computed:    a.IsComputed(),
			Sensitive:   a.IsSensitive(),
			Description: desc,
		}
		if da.Required {
			sec.Required = append(sec.Required, da)
		} else if da.Optional {
			sec.Optional = append(sec.Optional, da)
		} else if da.Computed {
			sec.ReadOnly = append(sec.ReadOnly, da)
		}
	}
	sort.Slice(sec.Required, func(i, j int) bool { return sec.Required[i].Name < sec.Required[j].Name })
	sort.Slice(sec.Optional, func(i, j int) bool { return sec.Optional[i].Name < sec.Optional[j].Name })
	sort.Slice(sec.ReadOnly, func(i, j int) bool { return sec.ReadOnly[i].Name < sec.ReadOnly[j].Name })

	for name, a := range s.Attributes {
		mode, nested := nestedAttributesOf(a)
		if len(nested) == 0 {
			continue
		}
		desc := a.GetMarkdownDescription()
		if desc == "" {
			desc = a.GetDescription()
		}
		sec.Blocks = append(sec.Blocks, docBlock{
			Name:        name,
			NestingMode: mode,
			Description: desc,
			Attributes:  extractDocAttributes(nested),
			Attribute:   true,
		})
	}

	for name, b := range s.Blocks {
		mode := "List"
		if _, ok := b.(rschema.ListNestedBlock); ok {
			mode = "List"
		}
		nb := docBlock{
			Name:        name,
			NestingMode: mode,
			Description: b.GetMarkdownDescription(),
		}
		for attrName, a := range b.GetNestedObject().GetAttributes() {
			desc := a.GetMarkdownDescription()
			if desc == "" {
				desc = a.GetDescription()
			}
			nb.Attributes = append(nb.Attributes, docAttribute{
				Name:        attrName,
				Type:        formatAttributeType(a.GetType().String()),
				Required:    a.IsRequired(),
				Optional:    a.IsOptional(),
				Computed:    a.IsComputed(),
				Sensitive:   a.IsSensitive(),
				Description: desc,
			})
		}
		sort.Slice(nb.Attributes, func(i, j int) bool { return nb.Attributes[i].Name < nb.Attributes[j].Name })
		sec.Blocks = append(sec.Blocks, nb)
	}
	sort.Slice(sec.Blocks, func(i, j int) bool {
		if sec.Blocks[i].Attribute != sec.Blocks[j].Attribute {
			return !sec.Blocks[i].Attribute
		}
		return sec.Blocks[i].Name < sec.Blocks[j].Name
	})

	return sec
}

// nestedAttributesOf reports the attributes nested inside a, and how they are nested. An
// attribute that nests nothing returns none, which is how the caller tells them apart.
//
// The framework's own nested-attribute interface lives under internal/, so this switches on the
// four concrete types a schema can hold instead. A fifth would be invisible rather than broken:
// the attribute still appears in the Optional list, just without its table.
func nestedAttributesOf(a rschema.Attribute) (string, map[string]rschema.Attribute) {
	switch n := a.(type) {
	case rschema.MapNestedAttribute:
		return "Map", n.NestedObject.Attributes
	case rschema.ListNestedAttribute:
		return "List", n.NestedObject.Attributes
	case rschema.SetNestedAttribute:
		return "Set", n.NestedObject.Attributes
	case rschema.SingleNestedAttribute:
		return "Single", n.Attributes
	default:
		return "", nil
	}
}

func extractDataSourceSection(s dschema.Schema) docSection {
	var sec docSection
	for name, a := range s.Attributes {
		desc := a.GetMarkdownDescription()
		if desc == "" {
			desc = a.GetDescription()
		}
		da := docAttribute{
			Name:        name,
			Type:        formatAttributeType(a.GetType().String()),
			Required:    a.IsRequired(),
			Optional:    a.IsOptional(),
			Computed:    a.IsComputed(),
			Sensitive:   a.IsSensitive(),
			Description: desc,
		}
		if da.Required {
			sec.Required = append(sec.Required, da)
		} else if da.Optional {
			sec.Optional = append(sec.Optional, da)
		} else if da.Computed {
			sec.ReadOnly = append(sec.ReadOnly, da)
		}
	}
	sort.Slice(sec.Required, func(i, j int) bool { return sec.Required[i].Name < sec.Required[j].Name })
	sort.Slice(sec.Optional, func(i, j int) bool { return sec.Optional[i].Name < sec.Optional[j].Name })
	sort.Slice(sec.ReadOnly, func(i, j int) bool { return sec.ReadOnly[i].Name < sec.ReadOnly[j].Name })

	for name, b := range s.Blocks {
		mode := "List"
		nb := docBlock{
			Name:        name,
			NestingMode: mode,
			Description: b.GetMarkdownDescription(),
		}
		for attrName, a := range b.GetNestedObject().GetAttributes() {
			desc := a.GetMarkdownDescription()
			if desc == "" {
				desc = a.GetDescription()
			}
			nb.Attributes = append(nb.Attributes, docAttribute{
				Name:        attrName,
				Type:        formatAttributeType(a.GetType().String()),
				Required:    a.IsRequired(),
				Optional:    a.IsOptional(),
				Computed:    a.IsComputed(),
				Sensitive:   a.IsSensitive(),
				Description: desc,
			})
		}
		sort.Slice(nb.Attributes, func(i, j int) bool { return nb.Attributes[i].Name < nb.Attributes[j].Name })
		sec.Blocks = append(sec.Blocks, nb)
	}
	sort.Slice(sec.Blocks, func(i, j int) bool { return sec.Blocks[i].Name < sec.Blocks[j].Name })

	return sec
}

func writeAttributeTables(doc *markdown.Markdown, attrs []docAttribute) {
	header := []string{"Name", "Type", "Required", "Description"}
	rows := make([][]string, 0, len(attrs))
	for _, a := range attrs {
		req := "Optional"
		if a.Required {
			req = "Required"
		} else if a.Computed && !a.Optional {
			req = "Read-Only"
		}
		rows = append(rows, []string{
			"`" + a.Name + "`",
			"`" + a.Type + "`",
			req,
			a.Description,
		})
	}
	for _, line := range paddedTable(header, rows) {
		doc.PlainText(line)
	}
	doc.PlainText("")
}

func renderSchemaSection(doc *markdown.Markdown, sec docSection) {
	if len(sec.Required) > 0 {
		doc.H3("Required")
		doc.PlainText("")
		for _, a := range sec.Required {
			doc.PlainText(fmt.Sprintf("- `%s` (%s) %s", a.Name, a.Type, a.Description))
		}
		doc.PlainText("")
	}

	if len(sec.Optional) > 0 {
		doc.H3("Optional")
		doc.PlainText("")
		for _, a := range sec.Optional {
			computedNote := ""
			if a.Computed {
				computedNote = " (Optional, Computed)"
			}
			doc.PlainText(fmt.Sprintf("- `%s` (%s%s) %s", a.Name, a.Type, computedNote, a.Description))
		}
		doc.PlainText("")
	}

	if len(sec.ReadOnly) > 0 {
		doc.H3("Read-Only")
		doc.PlainText("")
		for _, a := range sec.ReadOnly {
			sensNote := ""
			if a.Sensitive {
				sensNote = " (Sensitive)"
			}
			doc.PlainText(fmt.Sprintf("- `%s` (%s%s) %s", a.Name, a.Type, sensNote, a.Description))
		}
		doc.PlainText("")
	}

	if len(sec.Blocks) > 0 {
		headed := map[bool]bool{}
		for _, b := range sec.Blocks {
			if !headed[b.Attribute] {
				if b.Attribute {
					doc.H3("Nested Attributes")
				} else {
					doc.H3("Nested Blocks")
				}
				doc.PlainText("")
				headed[b.Attribute] = true
			}
			kind := "Block"
			if b.Attribute {
				kind = "of Object"
			}
			doc.H4(fmt.Sprintf("`%s` (%s %s)", b.Name, b.NestingMode, kind))
			doc.PlainText("")
			if b.Description != "" {
				doc.PlainText(b.Description)
				doc.PlainText("")
			}
			if len(b.Attributes) > 0 {
				header := []string{"Attribute", "Type", "Required", "Description"}
				rows := make([][]string, 0, len(b.Attributes))
				for _, a := range b.Attributes {
					req := "Optional"
					if a.Required {
						req = "Required"
					} else if a.Computed && !a.Optional {
						req = "Read-Only"
					}
					rows = append(rows, []string{
						"`" + a.Name + "`",
						"`" + a.Type + "`",
						req,
						a.Description,
					})
				}
				for _, line := range paddedTable(header, rows) {
					doc.PlainText(line)
				}
				doc.PlainText("")
			}
		}
	}
}

func tofuSummary() ([]string, error) {
	linkDir, err := filepath.Rel(docsDir, tofuDocsDir)
	if err != nil {
		return nil, err
	}

	lines := []string{
		fmt.Sprintf("- [provider: cargoship](%s)", filepath.Join(linkDir, "index.md")),
		fmt.Sprintf("  - [resource: cargoship_cluster](%s)", filepath.Join(linkDir, "resources/cluster.md")),
		fmt.Sprintf("  - [data source: cargoship_cluster_facts](%s)", filepath.Join(linkDir, "data-sources/cluster_facts.md")),
	}
	return lines, nil
}
