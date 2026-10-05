package termdiff

// Option defines the contract for options to configure a [Printer] or a call to
// any of the functions like [Print], [Fprint] and [Sprint].
type Option func(p *Printer)

// WithBeforeText customizes the text that appears in the diff header indicating
// what the logical "before" state in the diff means.
func WithBeforeText(text string) Option {
	return func(p *Printer) {
		p.beforeText = text
	}
}

// WithAfterText customizes the text that appears in the diff header indicating
// what the logical "after" state in the diff means.
func WithAfterText(text string) Option {
	return func(p *Printer) {
		p.afterText = text
	}
}

// WithBuffer customizes the number of lines with no insertions or deletions
// that will be printed both above and below each set of lines with diffs.
func WithBuffer(buffer int) Option {
	return func(p *Printer) {
		p.buffer = buffer
	}
}

// WithInsertLineFormatter customizes the text formatter/pretty printer used to
// format portions of a line with inserted text that are not themselves changed.
func WithInsertLineFormatter(f Formatter) Option {
	return func(p *Printer) {
		p.insertLineFormatter = f
	}
}

// WithInsertTextFormatter customizes the text formatter/pretty printer used to
// format portions of a line where text has been inserted.
func WithInsertTextFormatter(f Formatter) Option {
	return func(p *Printer) {
		p.insertTextFormatter = f
	}
}

// WithEqualFormatter customizes the text formatter/pretty printer used to
// format any text that is not associated with insertion or deletion.
func WithEqualFormatter(f Formatter) Option {
	return func(p *Printer) {
		p.equalFormatter = f
	}
}

// WithDeleteLineFormatter customizes the text formatter/pretty printer used to
// format portions of a line with deleted text that are not themselves changed.
func WithDeleteLineFormatter(f Formatter) Option {
	return func(p *Printer) {
		p.deleteLineFormatter = f
	}
}

// WithDeleteTextFormatter customizes the text formatter/pretty printer used to
// format portions of a line where text has been deleted.
func WithDeleteTextFormatter(f Formatter) Option {
	return func(p *Printer) {
		p.deleteTextFormatter = f
	}
}

// WithNameFormatter customizes the text formatter/pretty printer used to format
// the name of the text being diffed at the top of the overall diff.
func WithNameFormatter(f Formatter) Option {
	return func(p *Printer) {
		p.nameFormatter = f
	}
}
