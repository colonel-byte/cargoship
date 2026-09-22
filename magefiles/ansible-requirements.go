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

// This file holds the shared layer for the collection pins in the Ansible collection's
// requirements.yml. The mage targets that rewrite or verify those pins each live in their own
// gen-ansible-requirements.go and test-ansible-requirements.go. See
// docs/agent/choice-ansible-collection-pins.md for why this is a mage target and not Renovate.

package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/Masterminds/semver/v3"
	"github.com/goccy/go-yaml"
)

const (
	// ansibleRequirementsPath pins the Galaxy collections the collection's own role installs.
	ansibleRequirementsPath = "ansible/colonel_byte/cargoship/requirements.yml"

	// ansibleRuntimePath declares requires_ansible, the one place the supported controller
	// range is written down. Every pin in ansibleRequirementsPath is resolved against it.
	ansibleRuntimePath = "ansible/colonel_byte/cargoship/meta/runtime.yml"
)

// galaxyTimeout bounds the whole conversation with a Galaxy server, so a hung mirror fails the
// target rather than the CI job's step timeout.
const galaxyTimeout = 60 * time.Second

// ansibleRequirements is the on-disk form of requirements.yml. roles is read only so a
// non-empty list can be rejected: nothing here resolves roles.
type ansibleRequirements struct {
	Roles       []any               `yaml:"roles"`
	Collections []ansibleCollection `yaml:"collections"`
}

// ansibleCollection is one pinned Galaxy collection. source is per entry so a private automation
// hub can be pinned alongside galaxy.ansible.com.
type ansibleCollection struct {
	Name    string `yaml:"name"`
	Source  string `yaml:"source"`
	Version string `yaml:"version"`
}

// ansibleRuntime is the subset of meta/runtime.yml this code reads.
type ansibleRuntime struct {
	RequiresAnsible string `yaml:"requires_ansible"`
}

// galaxyVersionPage is one page of the Galaxy v3 collection versions endpoint. requires_ansible
// is the field this whole file exists for: Renovate's galaxy-collection datasource reads
// created_at and repository from this same response and drops it on the floor.
type galaxyVersionPage struct {
	Links struct {
		Next string `json:"next"`
	} `json:"links"`
	Data []galaxyVersion `json:"data"`
}

// galaxyVersion is one published version of a collection. RequiresAnsible is a PEP 440 specifier
// set, and is absent on versions published before Galaxy required it.
type galaxyVersion struct {
	Version         string `json:"version"`
	RequiresAnsible string `json:"requires_ansible"`
}

// readAnsibleRequirements loads and parses requirements.yml.
func readAnsibleRequirements() (ansibleRequirements, error) {
	var req ansibleRequirements

	data, err := os.ReadFile(ansibleRequirementsPath)
	if err != nil {
		return req, fmt.Errorf("reading %s: %w", ansibleRequirementsPath, err)
	}
	if err := yaml.Unmarshal(data, &req); err != nil {
		return req, fmt.Errorf("parsing %s: %w", ansibleRequirementsPath, err)
	}
	if len(req.Collections) == 0 {
		return req, fmt.Errorf("%s: no collections to resolve", ansibleRequirementsPath)
	}
	// Nothing here resolves roles, and write() renders the empty list rather than carrying
	// entries through. Fail loudly rather than dropping a role someone added.
	if len(req.Roles) != 0 {
		return req, fmt.Errorf("%s: holds %d role(s), which this resolver does not handle", ansibleRequirementsPath, len(req.Roles))
	}
	return req, nil
}

// controllerRange is the ansible-core range meta/runtime.yml declares support for.
//
// A requires_ansible that does not parse is an error rather than a skip. ansible-core swallows
// that case -- plugins/loader.py hands the string to SpecifierSet and downgrades the raise to a
// warning -- so a malformed value there enforces nothing and says so only in playbook output.
func controllerRange() (versionRange, error) {
	var runtime ansibleRuntime

	data, err := os.ReadFile(ansibleRuntimePath)
	if err != nil {
		return versionRange{}, fmt.Errorf("reading %s: %w", ansibleRuntimePath, err)
	}
	if err := yaml.Unmarshal(data, &runtime); err != nil {
		return versionRange{}, fmt.Errorf("parsing %s: %w", ansibleRuntimePath, err)
	}
	if runtime.RequiresAnsible == "" {
		return versionRange{}, fmt.Errorf("%s: requires_ansible is empty, so there is no controller range to resolve against", ansibleRuntimePath)
	}

	rng, err := parseSpecifier(runtime.RequiresAnsible)
	if err != nil {
		return versionRange{}, fmt.Errorf("%s: requires_ansible: %w", ansibleRuntimePath, err)
	}
	if rng.lo == nil || rng.hi == nil {
		return versionRange{}, fmt.Errorf(
			"%s: requires_ansible %q is not bounded on both sides: an unbounded range admits every release and defeats the pin",
			ansibleRuntimePath, runtime.RequiresAnsible)
	}
	return rng, nil
}

// versionRange is a PEP 440 specifier set reduced to a single interval. A nil bound is unbounded
// on that side. Every specifier Galaxy publishes for requires_ansible is a conjunction of simple
// comparisons, which reduces exactly; parseSpecifier rejects anything that does not.
type versionRange struct {
	lo, hi                   *semver.Version
	loInclusive, hiInclusive bool
}

// String renders the interval back as a specifier set, for error messages that have to show what
// was actually compared.
func (r versionRange) String() string {
	var clauses []string
	if r.lo != nil {
		clauses = append(clauses, cmpOp(">", r.loInclusive)+r.lo.String())
	}
	if r.hi != nil {
		clauses = append(clauses, cmpOp("<", r.hiInclusive)+r.hi.String())
	}
	if len(clauses) == 0 {
		return "*"
	}
	return strings.Join(clauses, ",")
}

// cmpOp renders a comparison operator with or without its equals.
func cmpOp(op string, inclusive bool) string {
	if inclusive {
		return op + "="
	}
	return op
}

// parseSpecifier reduces a PEP 440 specifier set to an interval.
//
// An empty string is the unbounded range, which is how Galaxy reports a version published before
// requires_ansible was mandatory. Operators outside the comparison set are an error rather than a
// guess: silently ignoring one would widen the interval and admit a release that cannot run.
func parseSpecifier(spec string) (versionRange, error) {
	rng := versionRange{}

	spec = strings.TrimSpace(spec)
	if spec == "" {
		return rng, nil
	}

	for clause := range strings.SplitSeq(spec, ",") {
		clause = strings.TrimSpace(clause)
		if clause == "" {
			continue
		}

		op := clause[:len(clause)-len(strings.TrimLeft(clause, "<>=!~"))]
		raw := strings.TrimSpace(clause[len(op):])
		if raw == "" {
			return versionRange{}, fmt.Errorf("clause %q has an operator but no version", clause)
		}

		// A bare version is the most likely malformed value to reach here -- it is what an
		// ansible-core release number looks like when copied straight in -- so name the fix.
		if op == "" {
			return versionRange{}, fmt.Errorf(
				"clause %q is a bare version, which is not a PEP 440 specifier: write an operator, for example %q",
				clause, ">="+clause)
		}

		ver, err := semver.NewVersion(raw)
		if err != nil {
			return versionRange{}, fmt.Errorf("clause %q: parsing version %q: %w", clause, raw, err)
		}

		switch op {
		case ">=", ">":
			rng.raiseFloor(ver, op == ">=")
		case "<=", "<":
			rng.lowerCeiling(ver, op == "<=")
		case "==":
			rng.raiseFloor(ver, true)
			rng.lowerCeiling(ver, true)
		default:
			return versionRange{}, fmt.Errorf(
				"clause %q uses operator %q, which this resolver does not reduce to an interval", clause, op)
		}
	}
	return rng, nil
}

// raiseFloor narrows the interval's lower bound, keeping the tighter of the two.
func (r *versionRange) raiseFloor(ver *semver.Version, inclusive bool) {
	if r.lo == nil || ver.GreaterThan(r.lo) || (ver.Equal(r.lo) && !inclusive) {
		r.lo, r.loInclusive = ver, inclusive
	}
}

// lowerCeiling narrows the interval's upper bound, keeping the tighter of the two.
func (r *versionRange) lowerCeiling(ver *semver.Version, inclusive bool) {
	if r.hi == nil || ver.LessThan(r.hi) || (ver.Equal(r.hi) && !inclusive) {
		r.hi, r.hiInclusive = ver, inclusive
	}
}

// admits reports whether every ansible-core version in controller is one that release supports --
// that is, whether controller is a subset of release.
//
// Containment, not intersection. A release supporting only 2.18 and up overlaps nothing in a
// 2.16 controller range and is rejected, but so is a release capped below the controller's own
// ceiling: an operator anywhere in the declared range has to be able to run the pin.
func (r versionRange) admits(controller versionRange) bool {
	// Floor: the release must start no later than the controller's own floor.
	if r.lo != nil {
		if controller.lo == nil {
			return false
		}
		switch cmp := r.lo.Compare(controller.lo); {
		case cmp > 0:
			return false
		case cmp == 0 && !r.loInclusive && controller.loInclusive:
			return false
		}
	}

	// Ceiling: the release must end no earlier than the controller's own ceiling.
	if r.hi != nil {
		if controller.hi == nil {
			return false
		}
		switch cmp := r.hi.Compare(controller.hi); {
		case cmp < 0:
			return false
		case cmp == 0 && !r.hiInclusive && controller.hiInclusive:
			return false
		}
	}
	return true
}

// resolveCollection is the newest release of one collection that the controller range admits,
// along with the newest release overall. The two differ exactly when a collection has moved past
// the controller, which is the case worth printing.
func resolveCollection(versions []galaxyVersion, controller versionRange) (best, newest galaxyVersion, err error) {
	var bestVer, newestVer *semver.Version

	for _, v := range versions {
		ver, verErr := semver.NewVersion(v.Version)
		if verErr != nil {
			// Galaxy has served malformed versions in the past. One bad entry must not
			// hide every good one, so skip it rather than failing the whole resolve.
			continue
		}
		// A prerelease is never a pin: requirements.yml is what a release installs.
		if ver.Prerelease() != "" {
			continue
		}

		if newestVer == nil || ver.GreaterThan(newestVer) {
			newest, newestVer = v, ver
		}

		rng, specErr := parseSpecifier(v.RequiresAnsible)
		if specErr != nil {
			// An upstream publisher's malformed specifier is theirs to fix. Treating it
			// as unconstrained would admit a release that cannot run, so skip it.
			continue
		}
		if !rng.admits(controller) {
			continue
		}
		if bestVer == nil || ver.GreaterThan(bestVer) {
			best, bestVer = v, ver
		}
	}

	if bestVer == nil {
		return galaxyVersion{}, newest, fmt.Errorf("no published version supports ansible-core %s", controller)
	}
	return best, newest, nil
}

// galaxyVersionsURL is the v3 versions endpoint for a collection on a given Galaxy server. This
// is the same endpoint Renovate's galaxy-collection datasource calls.
func galaxyVersionsURL(source, name string) (string, error) {
	namespace, collection, ok := strings.Cut(name, ".")
	if !ok || namespace == "" || collection == "" {
		return "", fmt.Errorf("collection %q is not in namespace.name form", name)
	}

	base, err := url.Parse(strings.TrimSuffix(source, "/") + "/")
	if err != nil {
		return "", fmt.Errorf("collection %s: parsing source %q: %w", name, source, err)
	}

	ref, err := url.Parse(fmt.Sprintf(
		"api/v3/plugin/ansible/content/published/collections/index/%s/%s/versions/?limit=100",
		url.PathEscape(namespace), url.PathEscape(collection)))
	if err != nil {
		return "", fmt.Errorf("collection %s: %w", name, err)
	}
	return base.ResolveReference(ref).String(), nil
}

// galaxyVersions lists every published version of a collection, following the endpoint's paging.
// Touches the network.
func galaxyVersions(client *http.Client, source, name string) ([]galaxyVersion, error) {
	next, err := galaxyVersionsURL(source, name)
	if err != nil {
		return nil, err
	}

	base, err := url.Parse(next)
	if err != nil {
		return nil, fmt.Errorf("collection %s: %w", name, err)
	}

	var versions []galaxyVersion
	for next != "" {
		page, err := galaxyVersionPageAt(client, next)
		if err != nil {
			return nil, fmt.Errorf("collection %s: %w", name, err)
		}
		versions = append(versions, page.Data...)

		if page.Links.Next == "" {
			break
		}
		// links.next is served as a path, so resolve it against the endpoint's own URL
		// rather than assuming galaxy.ansible.com.
		ref, err := url.Parse(page.Links.Next)
		if err != nil {
			return nil, fmt.Errorf("collection %s: parsing next page %q: %w", name, page.Links.Next, err)
		}
		next = base.ResolveReference(ref).String()
	}

	if len(versions) == 0 {
		return nil, fmt.Errorf("collection %s: %s published no versions", name, source)
	}
	return versions, nil
}

// galaxyVersionPageAt fetches and decodes one page. Touches the network.
func galaxyVersionPageAt(client *http.Client, endpoint string) (galaxyVersionPage, error) {
	var page galaxyVersionPage

	resp, err := client.Get(endpoint)
	if err != nil {
		return page, fmt.Errorf("GET %s: %w", endpoint, err)
	}
	defer resp.Body.Close() //nolint:errcheck // read-only request

	if resp.StatusCode != http.StatusOK {
		return page, fmt.Errorf("GET %s: %s", endpoint, resp.Status)
	}
	if err := json.NewDecoder(resp.Body).Decode(&page); err != nil {
		return page, fmt.Errorf("decoding %s: %w", endpoint, err)
	}
	return page, nil
}

// galaxyClient is the HTTP client the targets use against Galaxy.
func galaxyClient() *http.Client {
	return &http.Client{Timeout: galaxyTimeout}
}

// requirementsHeader tops the generated file, so a reader editing a version by hand is told where
// the value comes from before they do it.
const requirementsHeader = `---
# Generated by:
#   - mage generate:ansibleRequirements
#   - go run ./magefiles/core/core.go generate:ansibleRequirements
#
# DO NOT EDIT VERSION MANUALLY, new collections can be add re-run the
# above generate command.
`

// write rewrites requirements.yml from the in-memory requirements.
//
// Rendered rather than marshaled: the output has to be byte-for-byte stable so the CI gate can
// diff it, and it has to keep both the header above and the empty roles list, neither of which
// survives a round trip through the YAML marshaler.
func (r ansibleRequirements) write() error {
	var b strings.Builder

	b.WriteString(requirementsHeader)
	b.WriteString("roles: []\n")
	b.WriteString("collections:\n")
	for _, c := range r.Collections {
		fmt.Fprintf(&b, "  - name: %s\n", c.Name)
		fmt.Fprintf(&b, "    source: %s\n", c.Source)
		fmt.Fprintf(&b, "    version: %s\n", c.Version)
	}

	if err := os.WriteFile(ansibleRequirementsPath, []byte(b.String()), 0o644); err != nil {
		return fmt.Errorf("writing %s: %w", ansibleRequirementsPath, err)
	}
	return nil
}
