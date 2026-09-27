package termdiff

type block struct {
	lines []line
}

func (p *Printer) getBlocks(lines []line) []block {
	var blocks []block

	var currentBlock *block
	var lastLineWithDiff int
	for i, l := range lines {
		if currentBlock == nil {
			if !l.HasDiff() {
				// Still not in a block
				continue
			}

			currentBlock = &block{}
			startOfRange := maxInt(i-p.buffer, 0)
			currentBlock.lines = append(currentBlock.lines, lines[startOfRange:i]...)
			lastLineWithDiff = i - 1
		}

		switch {
		case l.HasDiff():
			currentBlock.lines = append(currentBlock.lines, lines[lastLineWithDiff+1:i+1]...)
			lastLineWithDiff = i
		case i-lastLineWithDiff > p.buffer:
			currentBlock.lines = append(currentBlock.lines, lines[lastLineWithDiff+1:lastLineWithDiff+1+p.buffer]...)
			blocks = append(blocks, *currentBlock)
			currentBlock = nil
		}
	}

	if currentBlock != nil {
		end := minInt(lastLineWithDiff+1+p.buffer, len(lines))
		currentBlock.lines = append(currentBlock.lines, lines[lastLineWithDiff+1:end]...)
		blocks = append(blocks, *currentBlock)
	}

	return blocks
}
