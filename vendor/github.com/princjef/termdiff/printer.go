package termdiff

import (
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/logrusorgru/aurora/v4"
)

// Formatter applies formatting/pretty printing to a piece of text.
type Formatter func(text string) string

// Printer handles creating text-based diffs with several customization options.
// Use [NewPrinter] to create a printer that is pre-filled with the default
// options.
type Printer struct {
	beforeText          string
	afterText           string
	buffer              int
	insertLineFormatter Formatter
	insertTextFormatter Formatter
	equalFormatter      Formatter
	deleteLineFormatter Formatter
	deleteTextFormatter Formatter
	nameFormatter       Formatter
}

// NewPrinter creates a new [Printer], optionally customized with the given
// options. This is primarily useful for scenarios where multiple diffs need to
// be printed with the same options. Consider using the top-level [Print],
// [Fprint] and [Sprint] functions instead if you don't need to re-use
// customizations.
func NewPrinter(opts ...Option) Printer {
	p := Printer{
		beforeText:          "(before)",
		afterText:           "(after)",
		buffer:              2,
		insertLineFormatter: func(s string) string { return aurora.Green(s).String() },
		insertTextFormatter: func(s string) string { return aurora.BgGreen(aurora.Black(s)).String() },
		equalFormatter:      func(s string) string { return aurora.Faint(s).String() },
		deleteLineFormatter: func(s string) string { return aurora.Red(s).String() },
		deleteTextFormatter: func(s string) string { return aurora.BgRed(aurora.Black(s)).String() },
		nameFormatter:       func(s string) string { return aurora.Bold(s).String() },
	}

	for _, o := range opts {
		o(&p)
	}

	return p
}

// Print writes a set of diffs for the given named entity to [os.Stdout].
// Options can be specified to override behaviors in the [Printer].
func (p Printer) Print(name string, diffs []Diff, opts ...Option) {
	p.Fprint(os.Stdout, name, diffs, opts...)
}

// Fprint writes a set of diffs for the given named entity to the given writer.
// Options can be specified to override behaviors in the [Printer].
func (p Printer) Fprint(w io.Writer, name string, diffs []Diff, opts ...Option) {
	_, _ = w.Write([]byte(p.Sprint(name, diffs, opts...)))
}

// Sprint converts a set of diffs into a string that can be sent to a terminal
// or any other place. Options can be specified to override behaviors in the
// [Printer].
func (p Printer) Sprint(name string, diffs []Diff, opts ...Option) string {
	// We have a copy of the printer so we can safely apply local options
	for _, o := range opts {
		o(&p)
	}

	lines := diffsToLines(diffs)
	blocks := p.getBlocks(lines)
	return p.serialize(name, blocks)
}

var defaultPrinter = NewPrinter()

// Print writes a set of diffs for the given named entity to [os.Stdout] using
// the default configuration for printing. Configurations can be overridden
// using the various [Option] functions.
func Print(name string, diffs []Diff, opts ...Option) {
	defaultPrinter.Print(name, diffs, opts...)
}

// Fprint writes a set of diffs for the given named entity to the given writer
// using the default configuration for printing. Configurations can be
// overridden using the various [Option] functions.
func Fprint(w io.Writer, name string, diffs []Diff, opts ...Option) {
	defaultPrinter.Fprint(w, name, diffs, opts...)
}

// Sprint converts a set of diffs into a string that can be sent to a terminal
// or other output using the default configuration for printing. Configurations
// can be overridden using the various [Option] functions.
func Sprint(name string, diffs []Diff, opts ...Option) string {
	return defaultPrinter.Sprint(name, diffs, opts...)
}

func (p *Printer) serialize(name string, blocks []block) string {
	var builder strings.Builder

	if len(blocks) == 0 {
		return ""
	}

	builder.WriteString(fmt.Sprintf(
		"%s - %s %s\n",
		p.nameFormatter(name),
		p.deleteLineFormatter(p.beforeText),
		p.insertLineFormatter(p.afterText),
	))

	lastBlock := blocks[len(blocks)-1]
	last := lastBlock.lines[len(lastBlock.lines)-1]
	leftNumLen := len(strconv.Itoa(last.leftNumber))
	rightNumLen := len(strconv.Itoa(last.rightNumber))

	for i, b := range blocks {
		if i > 0 {
			builder.WriteString(p.equalFormatter(strings.Repeat("~", leftNumLen)))
			builder.WriteString("   ")

			builder.WriteString(p.equalFormatter(strings.Repeat("~", rightNumLen)))
			builder.WriteRune('\n')
		}

		for _, l := range b.lines {
			if l.HasBothDiff() {
				l1, l2 := l.Split()
				p.writeLine(&builder, l1, leftNumLen, rightNumLen)
				p.writeLine(&builder, l2, leftNumLen, rightNumLen)
				continue
			}

			p.writeLine(&builder, l, leftNumLen, rightNumLen)
		}
	}

	return builder.String()
}

func (p *Printer) writeLine(b *strings.Builder, l line, leftNumLen, rightNumLen int) {
	p.writeLineNumbers(b, l, leftNumLen, rightNumLen)

	switch {
	case !l.HasLeftNum():
		b.WriteString(p.insertLineFormatter("+"))
	case !l.HasRightNum():
		b.WriteString(p.deleteLineFormatter("-"))
	default:
		b.WriteRune(' ')
	}
	b.WriteRune(' ')

	for _, s := range l.spans {
		switch s.kind {
		case DeleteDiffType:
			b.WriteString(p.deleteTextFormatter(s.text))
		case EqualDiffType:
			switch {
			case l.HasLeftNum() && !l.HasRightNum():
				b.WriteString(p.deleteLineFormatter(s.text))
			case l.HasRightNum() && !l.HasLeftNum():
				b.WriteString(p.insertLineFormatter(s.text))
			case l.HasLeftNum() && l.HasRightNum():
				b.WriteString(p.equalFormatter(s.text))
			default:
				b.WriteString(s.text)
			}
		case InsertDiffType:
			b.WriteString(p.insertTextFormatter(s.text))
		}
	}
	b.WriteRune('\n')
}

func (p *Printer) writeLineNumbers(b *strings.Builder, l line, leftNumLen, rightNumLen int) {
	if l.HasLeftNum() {
		text := fmt.Sprintf("%*s | ", leftNumLen, strconv.Itoa(l.leftNumber))
		if !l.HasRightNum() {
			b.WriteString(p.deleteLineFormatter(text))
		} else {
			b.WriteString(p.equalFormatter(text))
		}
	} else {
		b.WriteString(fmt.Sprintf("%*s   ", leftNumLen, ""))
	}

	if l.HasRightNum() {
		text := fmt.Sprintf("%*s | ", rightNumLen, strconv.Itoa(l.rightNumber))
		if !l.HasLeftNum() {
			b.WriteString(p.insertLineFormatter(text))
		} else {
			b.WriteString(p.equalFormatter(text))
		}
	} else {
		b.WriteString(fmt.Sprintf("%*s   ", rightNumLen, ""))
	}
}

func maxInt(left, right int) int {
	if left > right {
		return left
	}

	return right
}

func minInt(left, right int) int {
	if left < right {
		return left
	}

	return right
}
