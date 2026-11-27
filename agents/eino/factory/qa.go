package factory

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/google/shlex"
	"github.com/woliveiras/weft/agents/eino/protocol"
)

func QA(ctx context.Context) (protocol.Agent, error) {
	return messageAgent(ctx, "check", map[string]action{"review.approved": checkTask})
}

func checkTask(ctx context.Context, j *job) error {
	command := first(stringValue(j.in.Config["test_command"]), "go test ./...")
	argv, err := shlex.Split(command)
	if err != nil || len(argv) == 0 {
		return errors.New("invalid QA test command")
	}
	seconds := 600
	if raw := j.in.Config["test_timeout_sec"]; raw != nil {
		if json.Unmarshal(raw, &seconds) != nil || seconds <= 0 {
			return errors.New("invalid QA timeout")
		}
	}
	result, err := j.client.Tool(ctx, "shell.exec", map[string]any{"argv": argv, "timeoutSec": seconds})
	item := make(map[string]any, len(j.item)+3)
	for k, v := range j.item {
		item[k] = v
	}
	kind := "qa.failed"
	switch {
	case forbidden(err):
		item["reason"] = "shell.exec not granted"
		item["failures"] = []string{"shell.exec not granted"}
	case errors.Is(err, protocol.ErrToolFailed):
		item["failures"] = []string{"test command failed in the gateway"}
	case err != nil:
		return err
	default:
		var check struct {
			ExitCode       *int `json:"exitCode"`
			Stdout, Stderr string
			TimedOut       bool
		}
		if json.Unmarshal(result.Data, &check) != nil || check.ExitCode == nil {
			item["failures"] = []string{"missing test exit status"}
		} else {
			report := strings.TrimSpace(check.Stdout + "\n" + check.Stderr)
			if report == "" {
				report = result.Content
			}
			runes := []rune(report)
			report = string(runes[max(0, len(runes)-8000):])
			item["report"], item["exit_code"] = report, *check.ExitCode
			if *check.ExitCode == 0 && !check.TimedOut {
				kind = "qa.passed"
			} else {
				item["failures"] = []string{first(report, "tests failed")}
			}
		}
	}
	j.emit(kind, item)
	j.summary = j.text("task_id") + ": " + kind
	return nil
}
