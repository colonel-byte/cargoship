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

// This file implements rpmvercmp, the segment-wise version comparison RPM itself uses. It is
// pure string handling with no repodata or HTTP in it, which is what makes the version
// ordering the rest of the package relies on testable on its own.

package dnfpins

// RpmCompareValues implements the RPM segment comparison algorithm (rpmvercmp).
func RpmCompareValues(a, b string) int {
	if a == b {
		return 0
	}

	i, j := 0, 0
	lenA, lenB := len(a), len(b)

	for i < lenA && j < lenB {
		for i < lenA && !isAlphaNum(a[i]) && a[i] != '~' && a[i] != '^' {
			i++
		}
		for j < lenB && !isAlphaNum(b[j]) && b[j] != '~' && b[j] != '^' {
			j++
		}

		if (i < lenA && a[i] == '~') || (j < lenB && b[j] == '~') {
			if i < lenA && a[i] == '~' && (j >= lenB || b[j] != '~') {
				return -1
			}
			if j < lenB && b[j] == '~' && (i >= lenA || a[i] != '~') {
				return 1
			}
			i++
			j++
			continue
		}

		if (i < lenA && a[i] == '^') || (j < lenB && b[j] == '^') {
			if i < lenA && a[i] == '^' && (j >= lenB || b[j] != '^') {
				return -1
			}
			if j < lenB && b[j] == '^' && (i >= lenA || a[i] != '^') {
				return 1
			}
			i++
			j++
			continue
		}

		if i >= lenA || j >= lenB {
			break
		}

		if isDigit(a[i]) {
			startA := i
			for i < lenA && isDigit(a[i]) {
				i++
			}
			for startA < i-1 && a[startA] == '0' {
				startA++
			}

			startB := j
			for j < lenB && isDigit(b[j]) {
				j++
			}
			for startB < j-1 && b[startB] == '0' {
				startB++
			}

			segA := a[startA:i]
			segB := b[startB:j]

			if len(segA) != len(segB) {
				if len(segA) < len(segB) {
					return -1
				}
				return 1
			}
			if segA != segB {
				if segA < segB {
					return -1
				}
				return 1
			}
		} else if isAlpha(a[i]) {
			startA := i
			for i < lenA && isAlpha(a[i]) {
				i++
			}
			startB := j
			for j < lenB && isAlpha(b[j]) {
				j++
			}

			segA := a[startA:i]
			segB := b[startB:j]

			if segA != segB {
				if segA < segB {
					return -1
				}
				return 1
			}
		} else {
			i++
			j++
		}
	}

	if i >= lenA && j >= lenB {
		return 0
	}
	if i >= lenA {
		if j < lenB && b[j] == '~' {
			return 1
		}
		return -1
	}
	if a[i] == '~' {
		return -1
	}
	return 1
}

func isDigit(c byte) bool {
	return c >= '0' && c <= '9'
}

func isAlpha(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

func isAlphaNum(c byte) bool {
	return isDigit(c) || isAlpha(c)
}
