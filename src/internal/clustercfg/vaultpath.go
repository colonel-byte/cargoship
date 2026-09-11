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

package clustercfg

import (
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/colonel-byte/cargoship/src/api/zarf.dev/v1alpha1/cluster"
	goyaml "github.com/goccy/go-yaml"
	"github.com/goccy/go-yaml/ast"
	"github.com/goccy/go-yaml/parser"
)

// ErrAlreadyEncrypted reports that the value at the requested path is Ansible Vault ciphertext
// already, so encrypting it again would bury the plaintext under a second layer that nothing
// unwraps.
var ErrAlreadyEncrypted = errors.New("value is already Ansible Vault-encrypted")

// registriesPath is where a cluster configuration keeps the registries whose credentials are
// vaulted, and vaultedFields are the fields of one of them that DecryptRegistryAuth reads at apply
// time.
//
// These two have to be kept in step with that function: a field added there and not here is one
// PathIsDecryptable warns about, and a field added here and not there promises a decryption that
// never happens. Everything else in this package derives the paths it cares about from these, so
// that keeping them in step is the only obligation.
const registriesPath = "$.spec.config.registries"

var vaultedFields = []string{"auth.user", "auth.pass", "auth.token", "tls.ca"}

// decryptablePath matches the paths whose values DecryptRegistryAuth reads at apply time.
var decryptablePath = regexp.MustCompile(`^` + regexp.QuoteMeta(registriesPath) + `\[\d+]\.(` + fieldAlternation(vaultedFields) + `)$`)

// fieldAlternation renders fields as a regexp alternation, so that decryptablePath is built from
// the list above rather than restating it.
func fieldAlternation(fields []string) string {
	quoted := make([]string, len(fields))
	for i, field := range fields {
		quoted[i] = regexp.QuoteMeta(field)
	}
	return strings.Join(quoted, "|")
}

// PathIsDecryptable reports whether yamlPath names a field cargoship decrypts at apply time. A
// value encrypted anywhere else reaches the host as ciphertext, because nothing unwraps it.
func PathIsDecryptable(yamlPath string) bool {
	path, err := parseYAMLPath(yamlPath)
	if err != nil {
		return false
	}
	return decryptablePath.MatchString(path.String())
}

// EncryptAtPath returns src with the scalar at yamlPath replaced by its Ansible Vault ciphertext,
// written as a literal block scalar. Every other byte of src is preserved -- comments, key order,
// quoting and indentation elsewhere in the document all survive, so the result is still the
// operator's file rather than a re-rendering of it.
//
// That byte-level splice is deliberate. Replacing the node in the parsed document and printing it
// back would be shorter, but go-yaml re-indents a multi-line literal by slicing each continuation
// line, which silently truncates ciphertext nested inside a sequence -- which is where every
// registry credential lives.
//
// If the value is ciphertext already, it returns ErrAlreadyEncrypted unless reencrypt is set.
func EncryptAtPath(src []byte, yamlPath, password string, reencrypt bool) ([]byte, error) {
	path, err := parseYAMLPath(yamlPath)
	if err != nil {
		return nil, err
	}

	file, err := parser.ParseBytes(src, parser.ParseComments)
	if err != nil {
		return nil, fmt.Errorf("parsing YAML: %w", err)
	}

	node, err := path.FilterFile(file)
	if err != nil {
		return nil, fmt.Errorf("no value found at %s: %w", path.String(), err)
	}

	plain, err := scalarValue(node, path.String())
	if err != nil {
		return nil, err
	}
	if cluster.IsVaultEncrypted(plain) && !reencrypt {
		return nil, fmt.Errorf("%s: %w", path.String(), ErrAlreadyEncrypted)
	}

	encrypted, err := EncryptValue(plain, password)
	if err != nil {
		return nil, err
	}

	// Strip rather than clip, so the stored ciphertext does not depend on whether the vault
	// library left a trailing newline on it.
	return spliceScalar(src, node, "|-", strings.Split(strings.TrimRight(encrypted, "\n"), "\n"))
}

// parseYAMLPath accepts a path with or without the "$" root that go-yaml requires, so that the
// dotted form an operator reads off a config -- ".spec.config.registries[0].auth.pass" -- works as
// typed.
func parseYAMLPath(yamlPath string) (*goyaml.Path, error) {
	normalized := yamlPath
	switch {
	case strings.HasPrefix(normalized, "$"):
	case strings.HasPrefix(normalized, "."):
		normalized = "$" + normalized
	default:
		normalized = "$." + normalized
	}
	path, err := goyaml.PathString(normalized)
	if err != nil {
		return nil, fmt.Errorf("invalid YAML path %q: %w", yamlPath, err)
	}
	return path, nil
}

// scalarValue returns the value held by node, rejecting anything that is not a single scalar. A
// mapping or a sequence has no one value to rewrite, and a null has none at all. A tagged node --
// Ansible's own "!vault" spelling, most often -- is rejected for the same reason: the tag is part
// of how the value is written, and replacing the scalar underneath it would strand the tag.
func scalarValue(node ast.Node, path string) (string, error) {
	switch n := node.(type) {
	case *ast.LiteralNode:
		return n.Value.GetToken().Value, nil
	case *ast.StringNode, *ast.IntegerNode, *ast.FloatNode, *ast.BoolNode:
		return node.GetToken().Value, nil
	default:
		return "", fmt.Errorf("%s holds a %s, not a single scalar value", path, strings.ToLower(node.Type().String()))
	}
}

// spliceScalar rewrites the source text of node as head followed by tail, returning src with only
// that span replaced. head takes the value's place on its own line; each tail line is written
// below it, indented to sit under the key that owns the value.
func spliceScalar(src []byte, node ast.Node, head string, tail []string) ([]byte, error) {
	text := string(src)
	lines := lineOffsets(text)

	token := node.GetToken()
	line := token.Position.Line
	if line < 1 || line > len(lines) {
		return nil, fmt.Errorf("value at line %d is outside the document", line)
	}
	start := lines[line-1] + token.Position.Column - 1
	if start > len(text) {
		return nil, fmt.Errorf("value at line %d, column %d is outside the document", line, token.Position.Column)
	}

	// current is the part of the value's own line that is replaced: the block introducer for a
	// literal, the raw scalar text for anything else. Whatever follows it on that line -- a
	// trailing comment, most often -- is carried over rather than overwritten.
	literal, isLiteral := node.(*ast.LiteralNode)
	current := strings.Trim(token.Origin, " \t\r\n")
	if isLiteral {
		current = literal.Start.Value
	}
	if !strings.HasPrefix(text[start:], current) {
		return nil, fmt.Errorf("value at line %d, column %d does not match the parsed document", line, token.Position.Column)
	}

	lineEnd := lineEndOffset(text, lines, line)
	currentEnd := start + len(current)
	// A scalar written across several lines has nothing after it on its first line to carry over,
	// and ends where its own text does rather than where that line does.
	rest, end := "", currentEnd
	switch {
	case isLiteral:
		rest, end = text[currentEnd:lineEnd], blockEndOffset(text, lines, line)
	case !strings.Contains(current, "\n"):
		rest, end = text[currentEnd:lineEnd], lineEnd
	}

	// Content is indented two past the key that owns it, which is what the rest of a cluster
	// configuration uses. Falling back to the value's own column keeps the block more indented
	// than its parent even when the key is not where it is expected to be.
	indent := token.Position.Column - 1
	if keyColumn, ok := keyColumnOn(text[lines[line-1]:lineEnd], token.Position.Column); ok {
		indent = keyColumn + 1
	}

	var b strings.Builder
	b.WriteString(text[:start])
	b.WriteString(head)
	b.WriteString(rest)
	for _, tailLine := range tail {
		b.WriteString("\n")
		// An empty line stays empty rather than becoming a run of spaces, so that a document
		// written here survives an editor or a linter that trims trailing whitespace.
		if tailLine != "" {
			b.WriteString(strings.Repeat(" ", indent))
			b.WriteString(tailLine)
		}
	}
	b.WriteString(text[end:])
	return []byte(b.String()), nil
}

// lineOffsets returns the offset at which each line of text begins.
func lineOffsets(text string) []int {
	offsets := []int{0}
	for i, r := range text {
		if r == '\n' {
			offsets = append(offsets, i+1)
		}
	}
	return offsets
}

// lineEndOffset returns the offset of the newline ending the 1-based line, or the end of text for
// a final line that has none.
func lineEndOffset(text string, lines []int, line int) int {
	if line < len(lines) {
		// The next line starts just past this one's newline.
		return lines[line] - 1
	}
	return len(text)
}

// blockEndOffset returns the offset at which the block scalar introduced on the 1-based
// headerLine ends. The block runs to the last line indented past the header's own line, with
// blank lines inside it counted as part of it.
func blockEndOffset(text string, lines []int, headerLine int) int {
	headerIndent := indentOf(text[lines[headerLine-1]:lineEndOffset(text, lines, headerLine)])
	end := lineEndOffset(text, lines, headerLine)
	for line := headerLine + 1; line <= len(lines); line++ {
		content := text[lines[line-1]:lineEndOffset(text, lines, line)]
		if strings.TrimSpace(content) == "" {
			continue
		}
		if indentOf(content) <= headerIndent {
			break
		}
		end = lineEndOffset(text, lines, line)
	}
	return end
}

// indentOf returns the number of leading spaces on a line.
func indentOf(line string) int {
	return len(line) - len(strings.TrimLeft(line, " "))
}

// keyColumnOn returns the 1-based column of the key whose value starts at valueColumn on line,
// reporting false when the text before the value is not a key -- a value on a line of its own, for
// instance, which gives nothing to indent against.
func keyColumnOn(line string, valueColumn int) (int, bool) {
	if valueColumn-1 > len(line) {
		return 0, false
	}
	before := line[:valueColumn-1]
	if !strings.HasSuffix(strings.TrimRight(before, " \t"), ":") {
		return 0, false
	}
	// A key opening a sequence entry sits past the "- " markers, not at the line's indentation.
	rest := strings.TrimLeft(before, " ")
	for strings.HasPrefix(rest, "- ") {
		rest = strings.TrimLeft(rest[2:], " ")
	}
	return len(before) - len(rest) + 1, true
}
