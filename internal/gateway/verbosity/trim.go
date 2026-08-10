package verbosity

import (
	"regexp"
	"strings"
	"unicode/utf8"
)

// trimSentenceFloor is the maximum number of trailing prose sentences that may
// remain after trimming.
const trimSentenceFloor = 2

// trimMinSentencesToTrim is the trailing-prose sentence count at or above which
// trimming becomes active. Responses whose trailing prose is shorter than this
// are returned unchanged.
const trimMinSentencesToTrim = 3

// trimSuffix is appended when trimming removes trailing text, signalling the
// response was intentionally cut.
const trimSuffix = "…"

// Substantive-boundary regexes. Content at or after the LAST match is treated
// as substantive and never trimmed into; only pure prose strictly after the
// last boundary is a candidate for removal.
var (
	// codeFence matches a complete fenced code block (```lang ... ```).
	codeFence = regexp.MustCompile("(?s)```[^\n]*\n.*?```[^\n]*\n?")

	// inlineCode matches a single-line fenced block (```...```).
	inlineCode = regexp.MustCompile("(?s)```[^`\n]*```")

	// bulletItem matches a markdown bullet or numbered list line.
	bulletItem = regexp.MustCompile(`(?m)^[ \t]*[-*+] |^[ \t]*\d+[.)] `)

	// diffMarker matches a unified-diff header or a +/-/@@ line.
	diffMarker = regexp.MustCompile(`(?m)^(diff --git |@@ -\d+,\d+ \+\d+,\d+ @@|^[+-])`)

	// dsmlTool matches a DSML-style tool-call block (DeepSeek).
	dsmlTool = regexp.MustCompile(`(?s)<(?:antml:function_calls|tool_calls)[\s\S]*?</(?:antml:function_calls|tool_calls)>`)
)

// trimProse removes trailing verbose prose from content. It returns content
// unchanged when there is nothing safe to trim.
//
// Safety invariants:
//   - Never truncate code blocks, diffs, tool calls, or bullet/numbered lists.
//   - Only pure prose strictly after the LAST substantive boundary is removed.
//   - The trailing prose region must contain >= trimMinSentencesToTrim
//     sentences before trimming is attempted; if it survives trimming, at most
//     trimSentenceFloor sentences are retained.
func trimProse(content string) string {
	original := content
	stripped := strings.TrimSpace(content)
	if stripped == "" {
		return original
	}

	last := lastSubstantiveBoundary(content)

	// No substantive boundary anywhere — the whole body is prose.
	if last == -1 {
		if sentenceCount(stripped) < trimMinSentencesToTrim {
			return original
		}
		trimmed := trimToSentenceFloor(stripped)
		if trimmed == stripped {
			return original
		}
		return trimmed + trimSuffix
	}

	// Trailing region strictly after the last substantive boundary.
	trailing := strings.TrimSpace(content[last:])
	if trailing == "" {
		// Content ends right at a substantive boundary — leave alone.
		return original
	}
	if sentenceCount(trailing) < trimMinSentencesToTrim {
		return original
	}

	kept := keepAtMost(trailing, trimSentenceFloor)
	if kept == trailing {
		return original
	}

	head := strings.TrimRight(content[:last], " \t\r\n")
	out := head
	if kept != "" {
		out += " " + kept
	}
	return strings.TrimSpace(out) + trimSuffix
}

// lastSubstantiveBoundary returns the byte offset just after the LAST
// substantive block (code fence, inline code, list line, diff line, DSML tool
// call) in content. Returns -1 when no substantive block exists.
func lastSubstantiveBoundary(content string) int {
	last := -1
	grow := func(loc []int) {
		if len(loc) == 2 && loc[1] > last {
			last = loc[1]
		}
	}
	for _, loc := range codeFence.FindAllStringIndex(content, -1) {
		grow(loc)
	}
	for _, loc := range inlineCode.FindAllStringIndex(content, -1) {
		grow(loc)
	}
	for _, loc := range bulletItem.FindAllStringIndex(content, -1) {
		grow(toLineEnd(content, loc))
	}
	for _, loc := range diffMarker.FindAllStringIndex(content, -1) {
		grow(toLineEnd(content, loc))
	}
	for _, loc := range dsmlTool.FindAllStringIndex(content, -1) {
		grow(loc)
	}
	return last
}

// toLineEnd extends a match to the end of its line (exclusive of the trailing
// newline), so prose after a list/diff line is what gets trimmed.
func toLineEnd(content string, loc []int) []int {
	end := loc[1]
	if idx := strings.IndexByte(content[loc[0]:], '\n'); idx >= 0 {
		end = loc[0] + idx
	}
	return []int{loc[0], end}
}

// sentenceCount counts sentence-ending punctuation in s, treating code fences
// as opaque (punctuation inside code is not a sentence boundary).
func sentenceCount(s string) int {
	count := 0
	inCode := false
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		if r == '`' && i+2 < len(s) && s[i+1] == '`' && s[i+2] == '`' {
			inCode = !inCode
			i += 3
			continue
		}
		if !inCode {
			switch r {
			case '!', '?', '。', '！', '？':
				count++
			case '.':
				if isSentenceDot(s, i) {
					count++
				}
			}
		}
		i += size
	}
	return count
}

// isSentenceDot reports whether a '.' at index i in s ends a sentence (as
// opposed to a decimal number or abbreviation). It is conservative: a '.' does
// not end a sentence when it is between two digits (e.g. "1.2").
func isSentenceDot(s string, i int) bool {
	// "1.2" or "v1.2" — '.' between two digits is a decimal, not a boundary.
	if i > 0 && isDigit(s[i-1]) && i+1 < len(s) && isDigit(s[i+1]) {
		return false
	}
	return true
}

// sentenceBoundaries returns byte offsets just after each sentence-ending
// dot/!?/。/！/？ in s, ignoring code-fence regions and collapsing "..." so
// only its last dot is a boundary.
func sentenceBoundaries(s string) []int {
	var out []int
	inCode := false
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		if r == '`' && i+2 < len(s) && s[i+1] == '`' && s[i+2] == '`' {
			inCode = !inCode
			i += 3
			continue
		}
		if !inCode {
			switch r {
			case '!', '?', '。', '！', '？':
				out = append(out, i+size)
			case '.':
				// Collapse "..." ellipsis: only treat the last '.' as a boundary.
				if i+2 < len(s) && s[i+1] == '.' && s[i+2] == '.' {
					out = append(out, i+3)
					i += 3
					continue
				}
				if isSentenceDot(s, i) {
					out = append(out, i+1)
				}
			}
		}
		i += size
	}
	return out
}

// keepAtMost returns the longest suffix of proseText that ends at a sentence
// boundary and contains at most keep complete sentences, whitespace-trimmed.
// If proseText already has <= keep sentences, it is returned unchanged.
func keepAtMost(proseText string, keep int) string {
	if keep <= 0 {
		return ""
	}
	bounds := sentenceBoundaries(proseText)
	if len(bounds) <= keep {
		return proseText
	}
	cut := bounds[len(bounds)-keep-1]
	return strings.TrimSpace(proseText[cut:])
}

// trimToSentenceFloor trims a pure-prose body down to at most trimSentenceFloor
// sentences. Used only when there is no substantive boundary in the content.
func trimToSentenceFloor(body string) string {
	bounds := sentenceBoundaries(body)
	if len(bounds) <= trimSentenceFloor {
		return body
	}
	cut := bounds[len(bounds)-trimSentenceFloor-1]
	return strings.TrimSpace(body[cut:])
}

func isDigit(b byte) bool {
	return b >= '0' && b <= '9'
}
