package termdiff

import (
	"strings"

	"github.com/sergi/go-diff/diffmatchpatch"
)

type (
	// Diff holds a single diff with a type and text.
	Diff struct {
		Type DiffType
		Text string
	}

	// DiffType categorizes the kind of operation associated with a [Diff].
	DiffType int
)

// The valid types of diffs.
const (
	InsertDiffType DiffType = iota + 1
	EqualDiffType
	DeleteDiffType
)

// DiffsFromDiffMatchPatch converts a set of [diffmatchpatch.Diff] diffs into
// their equivalent in this package to enable easy interoperability.
func DiffsFromDiffMatchPatch(diffs []diffmatchpatch.Diff) []Diff {
	out := make([]Diff, len(diffs))
	for i, d := range diffs {
		var typ DiffType
		switch d.Type {
		case diffmatchpatch.DiffInsert:
			typ = InsertDiffType
		case diffmatchpatch.DiffEqual:
			typ = EqualDiffType
		case diffmatchpatch.DiffDelete:
			typ = DeleteDiffType
		}

		out[i] = Diff{
			Type: typ,
			Text: d.Text,
		}
	}

	return out
}

func diffsToLines(diffs []Diff) []line {
	var lines []line

	leftLine := 1
	rightLine := 1
	currentLine := line{
		leftNumber:  1,
		rightNumber: 1,
	}
	for _, d := range diffs {
		var kind DiffType
		switch d.Type {
		case DeleteDiffType:
			kind = DeleteDiffType
		case EqualDiffType:
			kind = EqualDiffType
		case InsertDiffType:
			kind = InsertDiffType
		}

		diffLines := strings.Split(d.Text, "\n")
		for i, l := range diffLines {
			s := span{
				kind: kind,
				text: l,
			}

			if i > 0 {
				// Add an empty span if needed (empty line with no change)
				if len(currentLine.spans) == 0 {
					currentLine.spans = []span{
						{
							kind: EqualDiffType,
							text: "",
						},
					}
				}
				lines = append(lines, currentLine)

				switch d.Type {
				case DeleteDiffType:
					leftLine++
				case EqualDiffType:
					leftLine++
					rightLine++
				case InsertDiffType:
					rightLine++
				}

				currentLine = line{
					leftNumber:  leftLine,
					rightNumber: rightLine,
				}
			}

			if d.Type != EqualDiffType || len(s.text) > 0 {
				currentLine.spans = append(currentLine.spans, s)
			}
		}
	}

	// Add an empty span if needed (empty line with no change)
	if len(currentLine.spans) == 0 {
		currentLine.spans = []span{
			{
				kind: EqualDiffType,
				text: "",
			},
		}
	}
	lines = append(lines, currentLine)
	return lines
}
