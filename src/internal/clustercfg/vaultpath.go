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
	"unicode/utf8"

	"github.com/colonel-byte/cargoship/src/api/zarf.dev/v1alpha1/cluster"
	goyaml "github.com/goccy/go-yaml"
	"github.com/goccy/go-yaml/ast"
)

// ErrAlreadyEncrypted reports that the value at the requested path is Ansible Vault ciphertext
// already, so encrypting it again would bury the plaintext under a second layer that nothing
// unwraps.
var ErrAlreadyEncrypted = errors.New("value is already Ansible Vault-encrypted")

// ErrNotEncrypted reports that the value at the requested path is plaintext, so there is nothing
// to decrypt. Rewriting it anyway would be a no-op that still rewrote the file, which is worth
// saying out loud rather than reporting as success.
var ErrNotEncrypted = errors.New("value is not Ansible Vault-encrypted")

// ErrWrappedTwice reports that the value at the requested path is ciphertext whose plaintext is
// itself ciphertext -- what encrypt-path --force produces. Rekeying it would move the outer layer
// onto the new password and leave the inner one on the old, which no single password can read
// back, so it is refused rather than half done.
var ErrWrappedTwice = errors.New("value is Ansible Vault-encrypted more than once")

// registriesPath is where a cluster configuration keeps the registries whose credentials are
// vaulted, and vaultedFields are the fields of one of them that DecryptRegistryAuth reads at apply
// time.
//
// These two have to be kept in step with that function: a field added there and not here is one
// EncryptConfig walks past and PathIsDecryptable warns about, and a field added here and not there
// promises a decryption that never happens. Everything else in this package derives the paths it
// cares about from these, so that keeping them in step is the only obligation.
const registriesPath = "$.spec.config.registries"

var vaultedFields = []string{"auth.user", "auth.pass", "auth.token", "tls.ca"}

// decryptablePath matches the paths whose values DecryptRegistryAuth reads at apply time.
var decryptablePath = regexp.MustCompile(`^` + regexp.QuoteMeta(registriesPath) + `\[\d+]\.(` + fieldAlternation(vaultedFields) + `)$`)

// fieldAlternation renders fields as a regexp alternation, so that decryptablePath is built from
// the same list EncryptConfig walks rather than restating it.
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
	node, plain, path, err := scalarNodeAtPath(src, yamlPath)
	if err != nil {
		return nil, err
	}
	if cluster.IsVaultEncrypted(plain) && !reencrypt {
		return nil, fmt.Errorf("%s: %w", path, ErrAlreadyEncrypted)
	}

	encrypted, err := EncryptValue(plain, password)
	if err != nil {
		return nil, err
	}
	return spliceCiphertext(src, node, encrypted)
}

// scalarNodeAtPath locates the scalar at yamlPath in src and returns the node, its value, and the
// path as go-yaml renders it, which is the spelling errors should quote rather than the one the
// caller passed. EncryptAtPath, DecryptAtPath and RekeyAtPath all begin here.
func scalarNodeAtPath(src []byte, yamlPath string) (ast.Node, string, string, error) {
	path, err := parseYAMLPath(yamlPath)
	if err != nil {
		return nil, "", "", err
	}

	file, err := parseYAML(src)
	if err != nil {
		return nil, "", "", err
	}

	node, err := path.FilterFile(file)
	if err != nil {
		return nil, "", "", fmt.Errorf("no value found at %s: %w", path.String(), err)
	}

	value, err := scalarValue(node, path.String())
	if err != nil {
		return nil, "", "", err
	}
	return node, value, path.String(), nil
}

// spliceCiphertext writes encrypted over the value node holds, as a literal block scalar.
//
// The ciphertext is stripped rather than clipped, so what is stored does not depend on whether the
// vault library left a trailing newline on it.
func spliceCiphertext(src []byte, node ast.Node, encrypted string) ([]byte, error) {
	return spliceScalar(src, node, "|-", strings.Split(strings.TrimRight(encrypted, "\n"), "\n"))
}

// DecryptAtPath returns src with the Ansible Vault ciphertext at yamlPath replaced by its
// plaintext, preserving every other byte of src for the same reasons, and by the same means, as
// EncryptAtPath.
//
// The plaintext is written in whichever scalar style holds it faithfully: plain where YAML allows
// it, a literal block for something multi-line such as a PEM certificate, and a quoted string for
// anything the other two would change on the way back in. If the value is plaintext already, it
// returns ErrNotEncrypted.
func DecryptAtPath(src []byte, yamlPath, password string) ([]byte, error) {
	node, encrypted, path, err := scalarNodeAtPath(src, yamlPath)
	if err != nil {
		return nil, err
	}
	if !cluster.IsVaultEncrypted(encrypted) {
		return nil, fmt.Errorf("%s: %w", path, ErrNotEncrypted)
	}

	plain, err := DecryptValue(encrypted, password)
	if err != nil {
		return nil, err
	}

	head, tail, err := renderScalar(plain)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return spliceScalar(src, node, head, tail)
}

// RekeyAtPath returns src with the Ansible Vault ciphertext at yamlPath re-wrapped under
// newPassword, preserving every other byte of src by the same splice EncryptAtPath uses.
//
// The plaintext is never rendered back into the document. The value is decrypted, encrypted again,
// and spliced in as ciphertext, so the plaintext exists only as a string in memory -- which is the
// whole point of rekeying as one operation rather than a decrypt-file followed by an encrypt-file,
// where the file holds the plaintext in between. It also means a value renderScalar would refuse
// to write back, one that is not valid UTF-8, rekeys without trouble, because nothing here has to
// express it as YAML.
//
// A value that is plaintext returns ErrNotEncrypted, and one wrapped more than once
// ErrWrappedTwice.
func RekeyAtPath(src []byte, yamlPath, oldPassword, newPassword string) ([]byte, error) {
	node, encrypted, path, err := scalarNodeAtPath(src, yamlPath)
	if err != nil {
		return nil, err
	}
	if !cluster.IsVaultEncrypted(encrypted) {
		return nil, fmt.Errorf("%s: %w", path, ErrNotEncrypted)
	}

	plain, err := DecryptValue(encrypted, oldPassword)
	if err != nil {
		return nil, err
	}
	if cluster.IsVaultEncrypted(plain) {
		return nil, fmt.Errorf("%s: %w", path, ErrWrappedTwice)
	}

	rekeyed, err := EncryptValue(plain, newPassword)
	if err != nil {
		return nil, err
	}
	return spliceCiphertext(src, node, rekeyed)
}

// renderScalar returns the YAML text for value, split into the part that replaces the value on its
// own line and any further lines, the latter given relative to the indentation the caller will add.
//
// go-yaml's encoder chooses the style and handles the quoting rules, which is work better left to
// the YAML library than reimplemented here. It is wrong in two cases, both of which produce a
// document that reads back as something other than what went in, so those are quoted explicitly
// instead: a value holding a control character, which it writes raw into a plain scalar; and a
// multi-line value with trailing whitespace on a line, which survives a block scalar only until
// something that trims trailing whitespace touches the file.
func renderScalar(value string) (string, []string, error) {
	if !utf8.ValidString(value) {
		// A YAML scalar holds text, so there is no style that can carry arbitrary bytes. Saying so
		// leaves the ciphertext in place, which is better than writing a file whose value no longer
		// decodes to what was encrypted.
		return "", nil, errors.New("the decrypted value is not valid UTF-8, so it cannot be written back as YAML")
	}

	scalar := ""
	if blockSafe(value) {
		encoded, err := goyaml.Marshal(value)
		if err != nil {
			return "", nil, fmt.Errorf("rendering the decrypted value as YAML: %w", err)
		}
		scalar = strings.TrimSuffix(string(encoded), "\n")
	} else {
		scalar = quoteScalar(value)
	}

	lines := strings.Split(scalar, "\n")
	tail := make([]string, 0, len(lines)-1)
	for _, line := range lines[1:] {
		// The encoder indents a block scalar's content by two, which the splice re-does at the
		// depth the value actually sits at.
		tail = append(tail, strings.TrimPrefix(line, "  "))
	}
	return lines[0], tail, nil
}

// blockSafe reports whether go-yaml can render value in a style that reads back unchanged.
func blockSafe(value string) bool {
	for _, r := range value {
		if r != '\n' && (r < 0x20 || r == 0x7f) {
			return false
		}
	}
	if !strings.Contains(value, "\n") {
		return true
	}
	for _, line := range strings.Split(value, "\n") {
		if line != strings.TrimRight(line, " \t") {
			return false
		}
	}
	return true
}

// quoteScalar returns value as a YAML double-quoted scalar, which is the one style that can hold
// any string on a single line.
func quoteScalar(value string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range value {
		switch r {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		default:
			if r < 0x20 || r == 0x7f {
				fmt.Fprintf(&b, `\x%02x`, r)
				continue
			}
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}

// CanonicalYAMLPath returns yamlPath as go-yaml spells it, so that the several ways of writing one
// path -- "$.spec.x", ".spec.x" and "spec.x" -- come back as the same string. A caller given a list
// of paths needs that to tell whether two of them name the same value, which is worth catching
// before the first one has been rewritten.
func CanonicalYAMLPath(yamlPath string) (string, error) {
	path, err := parseYAMLPath(yamlPath)
	if err != nil {
		return "", err
	}
	return path.String(), nil
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

// EncryptConfig encrypts every registry credential in src that cargoship decrypts at apply time --
// each registry's auth.user, auth.pass, auth.token and tls.ca -- and returns the rewritten
// document along with the paths it changed. Everything else is left exactly as it was, by the same
// byte-level splice EncryptAtPath uses.
//
// A field that is absent, empty, or encrypted already is skipped rather than treated as an error,
// so running this over a configuration that is partly vaulted finishes the job, and running it
// twice changes nothing the second time. Pass reencrypt to wrap values that are ciphertext already.
//
// A value that is encrypted already has to be one this password can read, and it is an error when
// it is not. Skipping it quietly would leave the file holding ciphertext under two different vault
// passwords, and an apply decrypts a registry's fields with one password, so the result would be a
// configuration no password can read back -- found out about at apply time, several phases in,
// rather than here.
func EncryptConfig(src []byte, password string, reencrypt bool) ([]byte, []string, error) {
	return rewriteConfig(src, func(value string) (bool, error) {
		if reencrypt || !cluster.IsVaultEncrypted(value) {
			return true, nil
		}
		if _, err := DecryptValue(value, password); err != nil {
			return false, errors.New("is Ansible Vault-encrypted with a different password; decrypt the file with the password it was vaulted under before encrypting it with this one")
		}
		return false, nil
	}, func(doc []byte, path string) ([]byte, error) {
		return EncryptAtPath(doc, path, password, true)
	})
}

// DecryptConfig is the inverse of EncryptConfig: it decrypts every vaulted registry credential in
// src and returns the rewritten document along with the paths it changed. A field that is not
// ciphertext is skipped, so what comes back is a document with no vaulted credentials left in it
// however many of them there were to begin with.
func DecryptConfig(src []byte, password string) ([]byte, []string, error) {
	return rewriteConfig(src, func(value string) (bool, error) {
		return cluster.IsVaultEncrypted(value), nil
	}, func(doc []byte, path string) ([]byte, error) {
		return DecryptAtPath(doc, path, password)
	})
}

// RekeyConfig re-wraps every vaulted registry credential in src under newPassword and returns the
// rewritten document along with the paths it changed. A field that is absent, empty, or plaintext
// is skipped, so what comes back is a document whose ciphertext is all under one password and whose
// plaintext was never written anywhere.
//
// Every value it touches has to be readable with oldPassword, and it is an error when one is not.
// There is no way to rekey a file already holding ciphertext under two passwords into a working
// state -- an apply reads a registry's fields with a single password -- so stopping is the only
// answer that does not produce a configuration no password can read back.
//
// Unlike EncryptConfig this is not idempotent, and cannot be: Ansible Vault salts every
// encryption, so a second run rewrites the same values to different ciphertext. Passing the same
// password as both old and new is that property put to use rather than a mistake: every value
// comes back under a fresh salt, readable with the password the file already carried.
func RekeyConfig(src []byte, oldPassword, newPassword string) ([]byte, []string, error) {
	return rewriteConfig(src, func(value string) (bool, error) {
		if !cluster.IsVaultEncrypted(value) {
			return false, nil
		}
		if _, err := DecryptValue(value, oldPassword); err != nil {
			return false, errors.New("cannot be read with the old vault password; is it vaulted under a different one?")
		}
		return true, nil
	}, func(doc []byte, path string) ([]byte, error) {
		return RekeyAtPath(doc, path, oldPassword, newPassword)
	})
}

// rewriteConfig applies rewrite to every registry credential path whose current value satisfies
// wanted, returning the rewritten document and the paths that were changed. An error from wanted is
// reported against the path it was asked about, which is the one thing the caller cannot say for
// itself.
//
// Each rewrite reparses the document rather than reusing the nodes found here, because splicing
// one value moves every offset after it. The paths themselves do not move, so walking them in
// order is enough, and a configuration holds few enough of them for the reparsing to be beside the
// point.
func rewriteConfig(src []byte, wanted func(string) (bool, error), rewrite func([]byte, string) ([]byte, error)) ([]byte, []string, error) {
	count, err := registryCount(src)
	if err != nil {
		return nil, nil, err
	}

	doc := src
	changed := []string{}
	for i := range count {
		for _, field := range vaultedFields {
			path := fmt.Sprintf("%s[%d].%s", registriesPath, i, field)

			value, ok, err := scalarAtPath(doc, path)
			if err != nil {
				return nil, nil, err
			}
			// An empty value is skipped alongside a missing one: a credential nobody set is not
			// worth replacing with ciphertext that decrypts to nothing.
			if !ok || value == "" {
				continue
			}

			want, err := wanted(value)
			if err != nil {
				return nil, nil, fmt.Errorf("%s %w", path, err)
			}
			if !want {
				continue
			}

			doc, err = rewrite(doc, path)
			if err != nil {
				return nil, nil, err
			}
			changed = append(changed, path)
		}
	}
	return doc, changed, nil
}

// registryCount returns how many registries the configuration holds. A document with no registries
// key at all is an error rather than a document with nothing to do, because that is what pointing
// one of these commands at the wrong file looks like.
func registryCount(src []byte) (int, error) {
	path, err := parseYAMLPath(registriesPath)
	if err != nil {
		return 0, err
	}

	file, err := parseYAML(src)
	if err != nil {
		return 0, err
	}

	node, err := path.FilterFile(file)
	if err != nil {
		return 0, fmt.Errorf("no registries found at %s; is this a cluster configuration?: %w", registriesPath, err)
	}

	sequence, ok := node.(*ast.SequenceNode)
	if !ok {
		return 0, fmt.Errorf("%s holds a %s, not a list of registries", registriesPath, strings.ToLower(node.Type().String()))
	}
	return len(sequence.Values), nil
}

// scalarAtPath returns the scalar the document holds at path, reporting false when the path is not
// there. A path that resolves to something other than a scalar is reported as absent too: a
// registry with no tls block has no tls.ca to rewrite, and neither has one whose tls.ca somebody
// wrote as a list.
//
// Only a path the document does not have is absent. Anything else the lookup fails on is returned
// as an error, because a value this package cannot reach is one a whole-file command would
// otherwise walk past and then report the file as having had nothing to do -- which reads as
// success over credentials still sitting there in the clear.
func scalarAtPath(src []byte, yamlPath string) (string, bool, error) {
	node, _, ok, err := nodeAtPath(src, yamlPath)
	if err != nil || !ok {
		return "", false, err
	}

	value, err := scalarValue(node, yamlPath)
	if err != nil {
		return "", false, nil
	}
	return value, true, nil
}

// nodeAtPath returns the node at yamlPath along with where in src it was parsed from, reporting
// false when the document does not have that path.
//
// The position is what tells two paths naming one value apart from two paths naming two: an alias
// resolves to the anchored value's own node, so both come back pointing at the same bytes.
func nodeAtPath(src []byte, yamlPath string) (ast.Node, string, bool, error) {
	path, err := parseYAMLPath(yamlPath)
	if err != nil {
		return nil, "", false, err
	}

	file, err := parseYAML(src)
	if err != nil {
		return nil, "", false, err
	}

	node, err := path.FilterFile(file)
	if err != nil {
		if errors.Is(err, goyaml.ErrNotFoundNode) {
			return nil, "", false, nil
		}
		return nil, "", false, fmt.Errorf("no value found at %s: %w", path.String(), err)
	}

	position := node.GetToken().Position
	return node, fmt.Sprintf("%d:%d", position.Line, position.Column), true, nil
}
