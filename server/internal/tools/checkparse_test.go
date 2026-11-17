package tools

import (
	"fmt"
	"strings"
	"testing"
)

func TestParseGoTestOutput(t *testing.T) {
	out := `--- FAIL: TestAdd (0.00s)
    calc_test.go:9: Add(1, 2) = 4, want 3
--- FAIL: TestTable (0.00s)
    --- FAIL: TestTable/negative (0.00s)
        calc_test.go:21: got -1
--- FAIL: TestSilent (0.00s)
FAIL
FAIL	example.com/calc	0.004s
FAIL
`
	format, fs, total := ParseCheckOutput(out)
	if format != FormatGo || total != 3 {
		t.Fatalf("format %q total %d: %+v", format, total, fs)
	}
	want := []CheckFailure{
		{Test: "TestAdd", File: "calc_test.go", Line: 9, Message: "Add(1, 2) = 4, want 3"},
		{Test: "TestTable/negative", File: "calc_test.go", Line: 21, Message: "got -1"},
		{Test: "TestSilent", Message: "test failed"},
	}
	for i := range want {
		if fs[i] != want[i] {
			t.Errorf("failure %d = %+v, want %+v", i, fs[i], want[i])
		}
	}
}

func TestParseGoVerboseAttachesLogsToRunningTest(t *testing.T) {
	out := `=== RUN   TestOK
--- PASS: TestOK (0.00s)
=== RUN   TestBad
    bad_test.go:5: boom
--- FAIL: TestBad (0.00s)
FAIL
`
	_, fs, total := ParseCheckOutput(out)
	if total != 1 || fs[0].Test != "TestBad" || fs[0].Line != 5 || fs[0].Message != "boom" {
		t.Fatalf("%d %+v", total, fs)
	}
}

func TestParseGoBuildErrorsAndPanics(t *testing.T) {
	out := `# example.com/calc
./calc.go:4:9: undefined: missing
./calc.go:7:2: declared and not used: x
FAIL	example.com/calc [build failed]
`
	format, fs, total := ParseCheckOutput(out)
	if format != FormatGo || total != 2 || fs[0] != (CheckFailure{File: "./calc.go", Line: 4, Message: "undefined: missing"}) {
		t.Fatalf("%s %d %+v", format, total, fs)
	}
	_, fs, _ = ParseCheckOutput("=== RUN   TestPanics\npanic: runtime error: index out of range [recovered]\nFAIL\texample.com/x\t0.1s\n")
	if len(fs) != 1 || fs[0].Test != "TestPanics" || !strings.HasPrefix(fs[0].Message, "panic: runtime error") {
		t.Fatalf("panic: %+v", fs)
	}
}

func TestParseVitestOutput(t *testing.T) {
	out := ` FAIL  src/sum.test.ts > sum > adds numbers
AssertionError: expected 3 to be 4 // Object.is equality
 ❯ src/sum.test.ts:6:19
      4|   it('adds numbers', () => {

 Test Files  1 failed (1)
      Tests  1 failed | 2 passed (3)
 FAIL  src/sum.test.ts > sum > adds numbers
`
	format, fs, total := ParseCheckOutput(out)
	if format != FormatVitest || total != 1 {
		t.Fatalf("%s %d %+v", format, total, fs)
	}
	if fs[0] != (CheckFailure{File: "src/sum.test.ts", Line: 6, Test: "sum > adds numbers", Message: "AssertionError: expected 3 to be 4 // Object.is equality"}) {
		t.Fatalf("%+v", fs[0])
	}
}

func TestParseJestOutput(t *testing.T) {
	out := ` FAIL  src/sum.test.js
  ● sum › adds numbers

    expect(received).toBe(expected) // Object.is equality

    Expected: 4
    Received: 3

      at Object.<anonymous> (src/sum.test.js:3:17)

Tests:       1 failed, 1 passed, 2 total
`
	format, fs, total := ParseCheckOutput(out)
	if format != FormatJest || total != 1 {
		t.Fatalf("%s %d %+v", format, total, fs)
	}
	if fs[0].Test != "sum › adds numbers" || fs[0].File != "src/sum.test.js" || fs[0].Line != 3 || !strings.Contains(fs[0].Message, "expect(received).toBe(expected)") {
		t.Fatalf("%+v", fs[0])
	}
}

func TestParsePytestOutput(t *testing.T) {
	out := `________________________________ test_add ________________________________
    def test_add():
>       assert add(1, 2) == 4
E       assert 3 == 4

tests/test_calc.py:5: AssertionError
=========================== short test summary info ============================
FAILED tests/test_calc.py::test_add - assert 3 == 4
ERROR tests/test_io.py - ModuleNotFoundError: No module named 'x'
========================= 1 failed, 1 error in 0.02s ==========================
`
	format, fs, total := ParseCheckOutput(out)
	if format != FormatPytest || total != 2 {
		t.Fatalf("%s %d %+v", format, total, fs)
	}
	if fs[0] != (CheckFailure{File: "tests/test_calc.py", Line: 5, Test: "test_add", Message: "assert 3 == 4"}) {
		t.Fatalf("%+v", fs[0])
	}
	if fs[1].File != "tests/test_io.py" || !strings.Contains(fs[1].Message, "ModuleNotFoundError") {
		t.Fatalf("%+v", fs[1])
	}
}

func TestParseGenericFallbacks(t *testing.T) {
	format, fs, _ := ParseCheckOutput("checking...\nsrc/app.rs:12:5: error: mismatched types\nwarning: unused\nError: lint failed\n")
	if format != FormatGeneric || len(fs) != 2 || fs[0] != (CheckFailure{File: "src/app.rs", Line: 12, Message: "error: mismatched types"}) || fs[1].Message != "Error: lint failed" {
		t.Fatalf("%s %+v", format, fs)
	}
	// No error-looking line: keep the last lines of output.
	var b strings.Builder
	for i := 1; i <= 30; i++ {
		fmt.Fprintf(&b, "line %d\n", i)
	}
	_, fs, total := ParseCheckOutput(b.String())
	if total != genericTailLines || fs[0].Message != "line 16" || fs[len(fs)-1].Message != "line 30" {
		t.Fatalf("%d %+v", total, fs)
	}
	if _, fs, total := ParseCheckOutput(""); len(fs) != 0 || total != 0 {
		t.Fatalf("empty output: %+v", fs)
	}
}

func TestParseCheckOutputIsBounded(t *testing.T) {
	var b strings.Builder
	for i := 0; i < 100; i++ {
		fmt.Fprintf(&b, "--- FAIL: Test%d (0.00s)\n    x_test.go:%d: %s\n", i, i+1, strings.Repeat("é", 400))
	}
	_, fs, total := ParseCheckOutput(b.String())
	if total != 100 || len(fs) != maxCheckFailures {
		t.Fatalf("total %d kept %d", total, len(fs))
	}
	for _, f := range fs {
		if len(f.Message) > maxCheckFailureMessage+len("…") || !strings.HasSuffix(f.Message, "…") {
			t.Fatalf("message not clipped: %d bytes", len(f.Message))
		}
	}
}

func TestParseMixedOutputFallsThroughToTheFormatThatFailed(t *testing.T) {
	out := "ok  \texample.com/calc\t0.01s\n# example.com/tools\n" +
		"FAILED tests/test_api.py::test_get - assert 500 == 200\n"
	format, fs, total := ParseCheckOutput(out)
	if format != FormatPytest || total != 1 || fs[0].Test != "test_get" {
		t.Fatalf("%s %d %+v", format, total, fs)
	}
	format, fs, _ = ParseCheckOutput("ok  \texample.com/calc\t0.01s\n FAIL  src/a.test.ts > a > works\nAssertionError: expected 1 to be 2\n")
	if format != FormatVitest || fs[0].Test != "a > works" {
		t.Fatalf("%s %+v", format, fs)
	}
}

func TestParseGoVerboseIgnoresLogsOfPassingTests(t *testing.T) {
	out := `=== RUN   TestOK
    ok_test.go:4: setting up fixture
--- PASS: TestOK (0.00s)
=== RUN   TestSkip
    skip_test.go:2: needs docker
--- SKIP: TestSkip (0.00s)
=== RUN   TestBad
    bad_test.go:5: boom
--- FAIL: TestBad (0.00s)
FAIL
FAIL	example.com/x	0.01s
`
	_, fs, total := ParseCheckOutput(out)
	if total != 1 || fs[0] != (CheckFailure{Test: "TestBad", File: "bad_test.go", Line: 5, Message: "boom"}) {
		t.Fatalf("%d %+v", total, fs)
	}
}

func TestParsePytestUsesEachTestsOwnTraceback(t *testing.T) {
	out := `________________________ test_add[one plus one] ________________________
tests/test_calc.py:5: in test_add
    assert add(a, b) == want
src/calc.py:2: in add
    raise ValueError
E   ValueError
________________________ TestDiv.test_zero ________________________
tests/test_calc.py:21: in test_zero
    assert div(1, 0) == 0
E   ZeroDivisionError
=========================== short test summary info ============================
FAILED tests/test_calc.py::test_add[one plus one] - ValueError
FAILED tests/test_calc.py::TestDiv::test_zero - ZeroDivisionError
`
	format, fs, total := ParseCheckOutput(out)
	if format != FormatPytest || total != 2 {
		t.Fatalf("%s %d %+v", format, total, fs)
	}
	if fs[0] != (CheckFailure{File: "tests/test_calc.py", Line: 5, Test: "test_add[one plus one]", Message: "ValueError"}) {
		t.Fatalf("%+v", fs[0])
	}
	if fs[1].Test != "TestDiv::test_zero" || fs[1].Line != 21 {
		t.Fatalf("%+v", fs[1])
	}
}

func TestParseGenericIgnoresZeroCounts(t *testing.T) {
	_, fs, _ := ParseCheckOutput("Linting 40 files\n0 errors, 0 failed, no failures\nsrc/x.ts:3:1 error unexpected any\n")
	if len(fs) != 1 || fs[0].File != "src/x.ts" || fs[0].Line != 3 {
		t.Fatalf("%+v", fs)
	}
}

func TestCappedBufferKeepsHeadAndTail(t *testing.T) {
	b := &cappedBuffer{limit: 10, tail: 8}
	for i := 0; i < 100; i++ {
		_, _ = b.Write([]byte("0123456789"))
	}
	_, _ = b.Write([]byte("SUMMARY!"))
	got := b.String()
	if !b.truncated || !strings.HasPrefix(got, "0123456789\n…[") || !strings.HasSuffix(got, "\nSUMMARY!") || !strings.Contains(got, "[990 bytes omitted]") {
		t.Fatalf("%q", got)
	}
	head := &cappedBuffer{limit: 4}
	_, _ = head.Write([]byte("abcdef"))
	if head.String() != "abcd" || !head.truncated {
		t.Fatalf("head-only buffer %q", head.String())
	}
}

func TestIsCheckDefiningFile(t *testing.T) {
	for _, p := range []string{"Makefile", "web/package.json", "pyproject.toml", "tests/conftest.py", "server/x_test.go", "src/a.test.tsx", "vitest.config.ts", "tests/test_api.py", "go.mod"} {
		if !IsCheckDefiningFile(p) {
			t.Errorf("%s should be check-defining", p)
		}
	}
	for _, p := range []string{"server/main.go", "src/app.tsx", "README.md", "src/latest.go"} {
		if IsCheckDefiningFile(p) {
			t.Errorf("%s is not check-defining", p)
		}
	}
}
