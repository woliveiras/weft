package tools

import (
	"regexp"
	"strconv"
	"strings"
)

// CheckFailure is one concise failure parsed from a check's output.
type CheckFailure struct {
	Test    string `json:"test,omitempty"`
	File    string `json:"file,omitempty"`
	Line    int    `json:"line,omitempty"`
	Message string `json:"message,omitempty"`
}

const (
	maxCheckFailures       = 20
	maxCheckFailureMessage = 300
	genericTailLines       = 15
)

// Output formats recognised by ParseCheckOutput.
const (
	FormatGo      = "go"
	FormatVitest  = "vitest"
	FormatJest    = "jest"
	FormatPytest  = "pytest"
	FormatGeneric = "generic"
)

var (
	goFailRe        = regexp.MustCompile(`^\s*--- FAIL: (\S+)`)
	goRunRe         = regexp.MustCompile(`^=== (?:RUN|CONT|PAUSE)\s+(\S+)`)
	goLogRe         = regexp.MustCompile(`^\s+(\S+\.go):(\d+): (.*)$`)
	goBuildRe       = regexp.MustCompile(`^(\S+\.go):(\d+):(\d+): (.*)$`)
	goPassRe        = regexp.MustCompile(`^\s*--- (PASS|SKIP): (\S+)`)
	goPanicRe       = regexp.MustCompile(`^panic: (.*)$`)
	vitestFailRe    = regexp.MustCompile(`^\s*(?:FAIL|×|✗)\s+(\S+\.[cm]?[jt]sx?)\s+>\s+(.+?)\s*$`)
	vitestLocRe     = regexp.MustCompile(`❯\s+(\S+?):(\d+):\d+`)
	jestSuiteRe     = regexp.MustCompile(`^\s*FAIL\s+(\S+\.[cm]?[jt]sx?)\s*$`)
	jestTestRe      = regexp.MustCompile(`^\s*●\s+(.+?)\s*$`)
	jsLocRe         = regexp.MustCompile(`\(?((?:[\w./@-]+/)?[\w.@-]+\.[cm]?[jt]sx?):(\d+):\d+\)?`)
	errorLineRe     = regexp.MustCompile(`(?:\w*Error\b|\bexpected\b|\bExpected\b|\bReceived\b)`)
	pytestFailRe    = regexp.MustCompile(`^(?:FAILED|ERROR)\s+(\S+?\.py)(?:::(.+?))?(?:\s+-\s+(.*))?$`)
	pytestSectionRe = regexp.MustCompile(`^_{3,} (.+?) _{3,}$`)
	pytestLocRe     = regexp.MustCompile(`^(\S+\.py):(\d+): (.*)$`)
	genericLocRe    = regexp.MustCompile(`^(\S+?\.\w+):(\d+)(?::\d+)?:?\s*(.*)$`)
	// genericZeroRe removes "0 failed", "0 errors", "no failures" before
	// the error-word test, so summaries of a clean run are not failures.
	genericZeroRe = regexp.MustCompile(`(?i)\b(0|no|zero)\s+(errors?|fail(ed|ures?)?|fatal)\b`)
	genericErrRe  = regexp.MustCompile(`(?i)\b(error|fail(ed|ure)?|fatal|panic)\b`)
)

// ParseCheckOutput extracts concise failures from a check's combined output
// and names the format it recognised. The result is bounded: at most
// maxCheckFailures entries (total reports how many were found) with
// messages clipped, so a small model reads the failures rather than pages
// of logs. An unrecognised failing output falls back to the first lines
// that look like errors, then to the last lines of output.
func ParseCheckOutput(out string) (format string, failures []CheckFailure, total int) {
	lines := strings.Split(strings.ReplaceAll(out, "\r\n", "\n"), "\n")
	// A format is accepted only when it explains the failure: mixed output
	// (Go passes, then pytest fails) falls through to the next parser.
	for _, p := range []struct {
		name  string
		parse func([]string) []CheckFailure
	}{{FormatGo, parseGo}, {FormatVitest, parseVitest}, {FormatJest, parseJest}, {FormatPytest, parsePytest}} {
		if fs := p.parse(lines); len(fs) > 0 {
			return p.name, bound(fs), len(fs)
		}
	}
	fs := parseGeneric(lines)
	return FormatGeneric, bound(fs), len(fs)
}

func bound(fs []CheckFailure) []CheckFailure {
	if len(fs) > maxCheckFailures {
		fs = fs[:maxCheckFailures]
	}
	for i := range fs {
		fs[i].Message = clip(strings.TrimSpace(fs[i].Message), maxCheckFailureMessage)
		fs[i].Test = clip(fs[i].Test, 200)
		fs[i].File = clip(fs[i].File, 300)
	}
	return fs
}

func clip(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return truncateUTF8(s, n) + "…"
}

func atoi(s string) int { n, _ := strconv.Atoi(s); return n }

// parseGo reads `go test` and `go build`/`go vet` output: `--- FAIL`
// lines with their indented file:line logs, compiler errors and panics.
// With -v, logs print before the verdict, so they are buffered per running
// test and kept only when that test fails (passing tests' t.Log lines are
// not failures). Without -v they follow the `--- FAIL` line.
func parseGo(lines []string) []CheckFailure {
	var (
		fs        []CheckFailure
		failed    []string
		current   string // test named by the latest === RUN/CONT
		afterFail string // test whose `--- FAIL` line was just printed
	)
	pending := map[string][]CheckFailure{}
	covered := map[string]bool{}
	for _, l := range lines {
		switch {
		case goRunRe.MatchString(l):
			current, afterFail = goRunRe.FindStringSubmatch(l)[1], ""
		case goFailRe.MatchString(l):
			t := goFailRe.FindStringSubmatch(l)[1]
			failed, afterFail = append(failed, t), t
			if logs := pending[t]; len(logs) > 0 {
				fs, covered[t] = append(fs, logs...), true
				delete(pending, t)
			}
		case goPassRe.MatchString(l):
			delete(pending, goPassRe.FindStringSubmatch(l)[2])
			afterFail = ""
		case goLogRe.MatchString(l) && (afterFail != "" || current != ""):
			m := goLogRe.FindStringSubmatch(l)
			if afterFail != "" {
				fs = append(fs, CheckFailure{Test: afterFail, File: m[1], Line: atoi(m[2]), Message: m[3]})
				covered[afterFail] = true
			} else {
				pending[current] = append(pending[current], CheckFailure{Test: current, File: m[1], Line: atoi(m[2]), Message: m[3]})
			}
		case goBuildRe.MatchString(l):
			m := goBuildRe.FindStringSubmatch(l)
			fs = append(fs, CheckFailure{File: m[1], Line: atoi(m[2]), Message: m[4]})
		case goPanicRe.MatchString(l):
			t := current
			if afterFail != "" {
				t = afterFail
			}
			fs = append(fs, CheckFailure{Test: t, Message: "panic: " + goPanicRe.FindStringSubmatch(l)[1]})
			if t != "" {
				covered[t] = true
			}
		case strings.TrimSpace(l) != "" && !strings.HasPrefix(l, " ") && !strings.HasPrefix(l, "\t"):
			afterFail = ""
		}
	}
	// A failed test with no log line of its own, and no failed subtest
	// that explains it, is still reported by name.
	for _, t := range failed {
		if covered[t] || hasCoveredChild(t, covered) || hasFailedChild(t, failed) {
			continue
		}
		fs = append(fs, CheckFailure{Test: t, Message: "test failed"})
		covered[t] = true
	}
	return fs
}

func hasCoveredChild(t string, covered map[string]bool) bool {
	for c := range covered {
		if strings.HasPrefix(c, t+"/") {
			return true
		}
	}
	return false
}

func hasFailedChild(t string, failed []string) bool {
	for _, c := range failed {
		if strings.HasPrefix(c, t+"/") {
			return true
		}
	}
	return false
}

// parseVitest reads Vitest's `FAIL file > suite > test` headers, the first
// error line after each and the `❯ file:line:col` location.
func parseVitest(lines []string) []CheckFailure {
	var fs []CheckFailure
	for i, l := range lines {
		m := vitestFailRe.FindStringSubmatch(l)
		if m == nil {
			continue
		}
		f := CheckFailure{File: m[1], Test: m[2]}
		fillJSDetail(&f, lines[i+1:], vitestLocRe)
		fs = append(fs, f)
	}
	// Vitest prints each failure twice (summary and detail); keep one.
	return dedupe(fs)
}

// parseJest reads Jest's `FAIL file` suite lines and `● suite › test`
// blocks with their first error line and stack location.
func parseJest(lines []string) []CheckFailure {
	var (
		fs    []CheckFailure
		suite string
		seen  bool
	)
	for i, l := range lines {
		if m := jestSuiteRe.FindStringSubmatch(l); m != nil {
			suite, seen = m[1], true
			continue
		}
		m := jestTestRe.FindStringSubmatch(l)
		if m == nil || !seen {
			continue
		}
		f := CheckFailure{File: suite, Test: m[1]}
		fillJSDetail(&f, lines[i+1:], jsLocRe)
		fs = append(fs, f)
	}
	return fs
}

func fillJSDetail(f *CheckFailure, rest []string, loc *regexp.Regexp) {
	for j, l := range rest {
		if j > 40 || vitestFailRe.MatchString(l) || jestTestRe.MatchString(l) {
			return
		}
		t := strings.TrimSpace(l)
		if f.Message == "" && errorLineRe.MatchString(t) {
			f.Message = t
		}
		if f.Line == 0 {
			if m := loc.FindStringSubmatch(l); m != nil && !strings.Contains(m[1], "node_modules") {
				f.File, f.Line = m[1], atoi(m[2])
			}
		}
		if f.Message != "" && f.Line != 0 {
			return
		}
	}
}

// parsePytest reads the `FAILED file::test - message` short summary and
// takes each failure's line from the traceback in its own
// `____ test ____` section (parametrized ids may contain spaces).
func parsePytest(lines []string) []CheckFailure {
	var fs []CheckFailure
	type loc struct {
		file string
		line int
	}
	sections := map[string][]loc{}
	section := ""
	for _, l := range lines {
		if m := pytestSectionRe.FindStringSubmatch(l); m != nil {
			section = m[1]
			continue
		}
		if strings.HasPrefix(l, "=====") {
			section = ""
		}
		if m := pytestLocRe.FindStringSubmatch(l); m != nil && section != "" {
			sections[section] = append(sections[section], loc{m[1], atoi(m[2])})
		}
		if m := pytestFailRe.FindStringSubmatch(l); m != nil {
			fs = append(fs, CheckFailure{File: m[1], Test: m[2], Message: m[3]})
		}
	}
	for i, f := range fs {
		// pytest titles a section Class.test[id] for the node id Class::test[id].
		locs := sections[strings.ReplaceAll(f.Test, "::", ".")]
		if len(locs) == 0 {
			if j := strings.LastIndex(f.Test, "::"); j >= 0 {
				locs = sections[f.Test[j+2:]]
			}
		}
		for _, l := range locs { // the first frame in the test's own file
			if l.file == f.File || strings.HasSuffix(f.File, "/"+l.file) || strings.HasSuffix(l.file, "/"+f.File) {
				fs[i].Line = l.line
				break
			}
		}
		if fs[i].Line == 0 && len(locs) > 0 {
			fs[i].Line = locs[len(locs)-1].line
		}
	}
	return fs
}

// parseGeneric keeps the first lines that look like errors; with none, the
// last lines of output.
func parseGeneric(lines []string) []CheckFailure {
	var fs []CheckFailure
	for _, l := range lines {
		t := strings.TrimSpace(l)
		if t == "" || !genericErrRe.MatchString(genericZeroRe.ReplaceAllString(t, "")) {
			continue
		}
		f := CheckFailure{Message: t}
		if m := genericLocRe.FindStringSubmatch(t); m != nil {
			f = CheckFailure{File: m[1], Line: atoi(m[2]), Message: m[3]}
		}
		fs = append(fs, f)
	}
	if len(fs) > 0 {
		return fs
	}
	for i := len(lines) - 1; i >= 0 && len(fs) < genericTailLines; i-- {
		if t := strings.TrimSpace(lines[i]); t != "" {
			fs = append([]CheckFailure{{Message: t}}, fs...)
		}
	}
	return fs
}

func dedupe(fs []CheckFailure) []CheckFailure {
	seen := map[string]int{}
	out := fs[:0]
	for _, f := range fs {
		k := f.File + "\x00" + f.Test
		if i, ok := seen[k]; ok {
			if out[i].Message == "" {
				out[i].Message = f.Message
			}
			if out[i].Line == 0 {
				out[i].Line = f.Line
			}
			continue
		}
		seen[k] = len(out)
		out = append(out, f)
	}
	return out
}
