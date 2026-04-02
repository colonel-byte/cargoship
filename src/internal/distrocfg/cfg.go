package distrocfg

import (
	"context"

	"github.com/colonel-byte/zarf-distro/src/api/v1alpha1"
	goyaml "github.com/goccy/go-yaml"
)

// Parse parses the yaml passed as a byte slice and applies schema migrations.
func Parse(ctx context.Context, b []byte) (v1alpha1.ZarfDistroPackage, error) {
	var dis v1alpha1.ZarfDistroPackage
	err := goyaml.Unmarshal(b, &dis)
	if err != nil {
		return v1alpha1.ZarfDistroPackage{}, err
	}
	return dis, nil
}
