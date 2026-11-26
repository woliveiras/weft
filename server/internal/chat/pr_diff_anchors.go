package chat

import (
	"regexp"
	"strconv"
	"strings"
)

type prDiffAnchor struct {
	Path, Side string
	Line       int
}

var prHunkHeader = regexp.MustCompile(`^@@ -([0-9]+)(?:,([0-9]+))? \+([0-9]+)(?:,([0-9]+))? @@(?: |$)`)

// AnchorsPRComments confines a reviewed selection to changed lines in the
// exact unified diff inspected by Chat. Provider-specific binary and malformed
// sections never create eligible anchors.
func AnchorsPRComments(diff string, comments []PRDraftComment) bool {
	anchors := changedPRLines(diff)
	for _, c := range comments {
		if !anchors[prDiffAnchor{c.Path, c.Side, c.Line}] {
			return false
		}
	}
	return len(comments) > 0
}

func changedPRLines(diff string) map[prDiffAnchor]bool {
	anchors := make(map[prDiffAnchor]bool)
	var before, after string
	var left, right, leftCount, rightCount int
	var pending []prDiffAnchor
	inHunk := false
	for _, line := range strings.Split(diff, "\n") {
		line = strings.TrimSuffix(line, "\r")
		if strings.HasPrefix(line, "diff --git ") {
			before, after, inHunk = "", "", false
		}
		if !inHunk {
			switch {
			case strings.HasPrefix(line, "--- "):
				before, after = prDiffPath(line[4:], "a/"), ""
			case strings.HasPrefix(line, "+++ "):
				after = prDiffPath(line[4:], "b/")
			case before != "" || after != "":
				if match := prHunkHeader.FindStringSubmatch(line); match != nil {
					left, leftCount = prHunkRange(match[1], match[2])
					right, rightCount = prHunkRange(match[3], match[4])
					inHunk = leftCount >= 0 && rightCount >= 0 && leftCount+rightCount > 0
					pending = pending[:0]
				}
			}
			continue
		}
		file := after
		if file == "" {
			file = before
		}
		if line == "" {
			inHunk = false
			continue
		}
		switch line[0] {
		case '-':
			pending = append(pending, prDiffAnchor{file, "LEFT", left})
			left, leftCount = left+1, leftCount-1
		case '+':
			pending = append(pending, prDiffAnchor{file, "RIGHT", right})
			right, rightCount = right+1, rightCount-1
		case ' ':
			left, leftCount = left+1, leftCount-1
			right, rightCount = right+1, rightCount-1
		case '\\':
			continue
		default:
			inHunk = false
		}
		if leftCount < 0 || rightCount < 0 {
			inHunk = false
		} else if inHunk && leftCount == 0 && rightCount == 0 {
			for _, anchor := range pending {
				anchors[anchor] = true
			}
			inHunk = false
		}
	}
	return anchors
}

func prHunkRange(line, length string) (int, int) {
	if length == "" {
		length = "1"
	}
	start, firstErr := strconv.Atoi(line)
	count, countErr := strconv.Atoi(length)
	if firstErr != nil || countErr != nil || start > 10000000 || count > 12000 || start == 0 && count > 0 {
		return 0, -1
	}
	return start, count
}

func prDiffPath(raw, prefix string) string {
	value, _, _ := strings.Cut(raw, "\t")
	if strings.HasPrefix(value, `"`) {
		decoded, err := strconv.Unquote(value)
		if err != nil {
			return ""
		}
		value = decoded
	}
	path, _ := strings.CutPrefix(value, prefix)
	if path == value {
		return ""
	}
	return path
}
