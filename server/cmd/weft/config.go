package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/woliveiras/weft/server/internal/installation"
)

type forceObjects []string

func (v *forceObjects) String() string { return strings.Join(*v, ",") }
func (v *forceObjects) Set(s string) error {
	if !strings.Contains(s, "/") {
		return fmt.Errorf("force requires Kind/name")
	}
	*v = append(*v, s)
	return nil
}

type configState struct {
	Server    string            `json:"server"`
	Revisions map[string]string `json:"revisions"`
}

func readConfigFiles(path string) ([]string, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("configuration input must not be a symlink")
	}
	paths := []string{path}
	if info.IsDir() {
		paths = nil
		err = filepath.WalkDir(path, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.Type()&os.ModeSymlink != 0 {
				return fmt.Errorf("configuration directory must not contain symlinks")
			}
			if !d.IsDir() && (strings.HasSuffix(p, ".yaml") || strings.HasSuffix(p, ".yml")) {
				paths = append(paths, p)
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	sort.Strings(paths)
	files := []string{}
	total := 0
	for _, p := range paths {
		f, err := os.Open(p)
		if err != nil {
			return nil, err
		}
		b, err := io.ReadAll(io.LimitReader(f, int64(installation.MaxBytes-total+1)))
		closeErr := f.Close()
		if err != nil {
			return nil, err
		}
		if closeErr != nil {
			return nil, closeErr
		}
		total += len(b)
		if total > installation.MaxBytes {
			return nil, fmt.Errorf("configuration exceeds 4 MiB")
		}
		files = append(files, string(b))
	}
	if _, err := installation.Parse(files); err != nil {
		return nil, err
	}
	return files, nil
}
func (c *client) apply(args []string) error { return c.applyConfiguration(args, os.Stdin, os.Stdout) }
func (c *client) applyConfiguration(args []string, input io.Reader, output io.Writer) error {
	flags := flag.NewFlagSet("apply", flag.ContinueOnError)
	path := flags.String("f", "", "YAML file or directory")
	dry := flags.Bool("dry-run", false, "show the plan without writes")
	yes := flags.Bool("yes", false, "confirm the reviewed apply")
	consent := flags.Bool("record-consent", false, "record issue-trigger consent")
	var force forceObjects
	flags.Var(&force, "force", "override baseline conflict for Kind/name (repeatable)")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *path == "" || len(flags.Args()) > 0 {
		return fmt.Errorf("apply needs -f <file|directory>")
	}
	files, err := readConfigFiles(*path)
	if err != nil {
		return err
	}
	statePath := strings.TrimRight(*path, string(filepath.Separator)) + ".weft-state.json"
	state := configState{Server: c.base, Revisions: map[string]string{}}
	b, err := os.ReadFile(statePath)
	if err == nil {
		if json.Unmarshal(b, &state) != nil {
			return fmt.Errorf("invalid revision state file; export current configuration before retry")
		}
		if state.Server != c.base {
			return fmt.Errorf("revision state belongs to another server")
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	if state.Revisions == nil {
		state.Revisions = map[string]string{}
	}
	request := installation.Request{Files: files, Revisions: state.Revisions, Force: force, RecordConsent: *consent}
	var plan installation.Plan
	if err = c.do("POST", "/api/v1/config/plan", request, &plan); err != nil {
		return err
	}
	printConfigPlan(output, plan)
	if !plan.OK {
		return fmt.Errorf("plan contains errors or conflicts")
	}
	if *dry {
		return nil
	}
	if !*yes {
		fmt.Fprint(output, "Apply this plan (including any repository downloads)? [y/N] ")
		scanner := bufio.NewScanner(input)
		if !scanner.Scan() {
			if err := scanner.Err(); err != nil {
				return err
			}
			return fmt.Errorf("confirmation required; use --yes for non-interactive apply")
		}
		answer := strings.TrimSpace(scanner.Text())
		if answer != "y" && answer != "yes" {
			return fmt.Errorf("apply cancelled")
		}
	}
	request.Expected = map[string]string{}
	for _, item := range plan.Objects {
		request.Expected[item.Key] = item.Revision
	}
	request.Confirmed = true
	// Imports can take longer than ordinary API requests. The server retains their progress on interruption.
	originalTimeout := c.http.Timeout
	c.http.Timeout = 0
	defer func() { c.http.Timeout = originalTimeout }()
	var report installation.Plan
	if err = c.do("POST", "/api/v1/config/apply", request, &report); err != nil {
		return err
	}
	printConfigPlan(output, report)
	for _, item := range report.Objects {
		if item.Result == "applied" || item.Result == "unchanged" {
			state.Revisions[item.Key] = item.Revision
		}
	}
	if err = writeConfigState(statePath, state); err != nil {
		return fmt.Errorf("apply returned; saving revisions failed: %w; export before retry", err)
	}
	if !report.OK {
		return fmt.Errorf("apply stopped; inspect the per-object report and retry")
	}
	return nil
}
func writeConfigState(path string, state configState) error {
	b, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".weft-state-*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	if _, err = f.Write(append(b, '\n')); err != nil {
		f.Close()
		return err
	}
	if err = f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
func printConfigPlan(out io.Writer, plan installation.Plan) {
	for _, item := range plan.Objects {
		fmt.Fprintf(out, "%s %s", item.Action, item.Key)
		if item.Result != "" {
			fmt.Fprintf(out, " [%s]", item.Result)
		}
		fmt.Fprintln(out)
		for _, change := range item.Changes {
			b, _ := json.Marshal(change.Before)
			a, _ := json.Marshal(change.After)
			fmt.Fprintf(out, "  %s: %s -> %s\n", change.Path, b, a)
		}
		for _, line := range []string{item.Status, item.Consent, item.Warning, item.Error} {
			if line != "" {
				fmt.Fprintf(out, "  %s\n", line)
			}
		}
	}
}
func (c *client) exportConfiguration(args []string) error {
	flags := flag.NewFlagSet("export", flag.ContinueOnError)
	path := flags.String("o", "", "output YAML file (default stdout)")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if len(flags.Args()) > 0 {
		return fmt.Errorf("export accepts only -o <file>")
	}
	resp, err := c.req("GET", "/api/v1/config/export", nil, "")
	if err != nil {
		return err
	}
	if resp.StatusCode != 200 {
		return decode(resp, nil)
	}
	defer resp.Body.Close()
	var out io.Writer = os.Stdout
	if *path != "" {
		f, err := os.OpenFile(*path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err != nil {
			return err
		}
		defer f.Close()
		out = f
	}
	_, err = io.Copy(out, resp.Body)
	return err
}
