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

package gen

import (
	"bytes"
	"go/format"
	"text/template"

	"github.com/colonel-byte/cargoship/src/pkg/engineconfig/extract"
)

// ComponentsOptions configures one generated packaged-component file.
type ComponentsOptions struct {
	PackageName string // must already be a valid Go package identifier
	Distro      string
	Version     string
	Components  extract.Components
}

type componentsData struct {
	ComponentsOptions
	// Target is only here to satisfy codeGeneratedComment, which is shared with Generate.
	Target string
}

var componentsTmpl = template.Must(template.New("components").Parse(`// Copyright 2026 colonel-byte
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

` + codeGeneratedComment + `

package {{.PackageName}}

// Addons is the set of packaged components {{.Distro}} {{.Version}} accepts in ` + "`disable:`" + `.
{{with .Components.Disable}}var Addons = []string{
{{range .}}	{{printf "%q" .}},
{{end}}}{{else}}var Addons []string{{end}}

// CNIs is the set of values {{.Distro}} {{.Version}} accepts in ` + "`cni:`" + `. Empty for distros
// that do not declare a CNI selection flag.
{{with .Components.CNI}}var CNIs = []string{
{{range .}}	{{printf "%q" .}},
{{end}}}{{else}}var CNIs []string{{end}}

// IngressControllers is the set of values {{.Distro}} {{.Version}} accepts in
// ` + "`ingress-controller:`" + `. Empty for distros that do not declare one.
{{with .Components.Ingress}}var IngressControllers = []string{
{{range .}}	{{printf "%q" .}},
{{end}}}{{else}}var IngressControllers []string{{end}}
`))

// GenerateComponents renders and gofmt's the packaged-component source for opts. It is always
// written, even when every list is empty: the generated registry references these vars in every
// distro/version package, so a package missing them fails to build.
func GenerateComponents(opts ComponentsOptions) ([]byte, error) {
	var buf bytes.Buffer
	if err := componentsTmpl.Execute(&buf, componentsData{ComponentsOptions: opts, Target: "components"}); err != nil {
		return nil, err
	}
	return format.Source(buf.Bytes())
}
