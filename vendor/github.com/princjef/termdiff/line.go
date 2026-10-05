package termdiff

type line struct {
	leftNumber  int
	rightNumber int
	spans       []span
}

type span struct {
	kind DiffType
	text string
}

func (l line) HasDiff() bool {
	for _, s := range l.spans {
		if s.kind != EqualDiffType {
			return true
		}
	}

	return false
}

func (l line) HasLeftNum() bool {
	if l.leftNumber == 0 {
		return false
	}

	for _, s := range l.spans {
		if s.kind != InsertDiffType {
			return true
		}
	}

	return false
}

func (l line) HasRightNum() bool {
	if l.rightNumber == 0 {
		return false
	}

	for _, s := range l.spans {
		if s.kind != DeleteDiffType {
			return true
		}
	}

	return false
}

func (l line) HasBothDiff() bool {
	var hasInsert, hasDelete bool
	for _, s := range l.spans {
		switch s.kind {
		case InsertDiffType:
			hasInsert = true
		case DeleteDiffType:
			hasDelete = true
		}

		if hasInsert && hasDelete {
			return true
		}
	}

	return false
}

func (l line) Split() (first line, second line) {
	var firstKind DiffType
	for _, s := range l.spans {
		if s.kind != EqualDiffType {
			firstKind = s.kind
			break
		}
	}

	if firstKind == InsertDiffType {
		// No number included for one of each side on a split line
		first.leftNumber = 0
		first.rightNumber = l.rightNumber
		second.leftNumber = l.leftNumber
		second.rightNumber = 0
	} else {
		first.leftNumber = l.leftNumber
		first.rightNumber = 0
		second.leftNumber = 0
		second.rightNumber = l.rightNumber
	}

	for _, s := range l.spans {
		switch {
		case s.kind == EqualDiffType:
			first.spans = append(first.spans, s)
			second.spans = append(second.spans, s)
		case s.kind == firstKind:
			first.spans = append(first.spans, s)
		default:
			second.spans = append(second.spans, s)
		}
	}

	return
}
