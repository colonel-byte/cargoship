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

package examples

import (
	"fmt"
	"slices"
	"sort"
)

// VerifyAssets re-fetches every image manifest example/assets.json has cached and compares it
// against what upstream serves now.
//
// The committed cache is what makes the examples renderable offline, and it is also a pin with
// nothing watching it: a release whose assets Rancher or k3s re-cut in place keeps rendering
// from the copy taken the day it was first fetched, and the rendered examples go on naming
// images that may no longer exist. Nothing fails, which is the problem. This target is the
// human-run owner of that pin -- it reads no cache, so it costs one request per manifest, and
// it reports rather than rewrites. `CARGOSHIP_EXAMPLES_NO_CACHE=1 mage generate:examples` is
// what applies the fix once it has told you there is one.
func VerifyAssets() error {
	assets := committedAssets()

	urls := make([]string, 0, len(assets.assets))
	for u := range assets.assets {
		urls = append(urls, u)
	}
	sort.Strings(urls)

	if len(urls) == 0 {
		fmt.Printf("%s holds no image manifests; nothing to verify\n", exampleAssetsPath)
		return nil
	}
	fmt.Printf("Checking %d image manifests in %s against upstream\n\n", len(urls), exampleAssetsPath)

	var drifted, unreachable int
	for _, url := range urls {
		cached := assets.assets[url]

		upstream, err := downloadLines(url)
		if err != nil {
			// A release upstream has pulled is worth reporting but is not drift: the example
			// for it is removed on the next render by the probe writeExample already runs.
			unreachable++
			fmt.Printf("unreachable  %s\n             %v\n\n", url, err)
			continue
		}

		if slices.Equal(cached, upstream) {
			continue
		}

		drifted++
		gone := missingFrom(upstream, cached)
		added := missingFrom(cached, upstream)

		fmt.Printf("drift  %s\n", url)
		for _, image := range gone {
			fmt.Printf("  - %s\n", image)
		}
		for _, image := range added {
			fmt.Printf("  + %s\n", image)
		}
		// The comparison is order-sensitive, since the rendered example lists images in the
		// manifest's order. A reordering with no membership change has nothing to print above.
		if len(gone) == 0 && len(added) == 0 {
			fmt.Println("  (same images, reordered upstream)")
		}
		fmt.Println()
	}

	fmt.Printf("%d checked, %d drifted, %d unreachable\n", len(urls), drifted, unreachable)
	if drifted > 0 {
		return fmt.Errorf("%d of %d image manifests in %s no longer match upstream; refresh them with CARGOSHIP_EXAMPLES_NO_CACHE=1 mage generate:examples",
			drifted, len(urls), exampleAssetsPath)
	}
	return nil
}

// missingFrom returns the lines of from that do not appear in have, in from's own order so a
// report reads in the same order as the manifest it came from.
func missingFrom(have, from []string) []string {
	present := make(map[string]struct{}, len(have))
	for _, line := range have {
		present[line] = struct{}{}
	}

	var missing []string
	for _, line := range from {
		if _, ok := present[line]; !ok {
			missing = append(missing, line)
		}
	}
	return missing
}
