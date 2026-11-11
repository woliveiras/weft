package taskauth

import "testing"

func TestApproval(t *testing.T) {
	for _, tc := range []struct {
		mode                        Mode
		capability, isolation       string
		mandatory, approval, denied bool
	}{
		{Ask, "filesystem.read", "process", false, true, false},
		{Workspace, "filesystem.write", "process", false, false, false},
		{Workspace, "shell.exec", "process", false, false, true},
		{Workspace, "shell.exec", "docker", false, false, false},
		{Workspace, "mcp.remote.read", "docker", false, true, false},
		{Workspace, "git.write", "process", false, true, false},
		{Full, "shell.exec", "process", false, false, false},
		{Full, "mcp.remote.write", "process", true, true, false},
		{"invalid", "filesystem.read", "process", false, false, true},
	} {
		t.Run(string(tc.mode)+tc.capability+tc.isolation, func(t *testing.T) {
			got, err := Approval(tc.mode, tc.capability, tc.isolation, tc.mandatory)
			if (err != nil) != tc.denied || got != tc.approval {
				t.Fatalf("%v %v", got, err)
			}
		})
	}
}

func TestReduction(t *testing.T) {
	for _, from := range []Mode{Ask, Workspace, Full, "bad"} {
		for _, to := range []Mode{Ask, Workspace, Full, "bad"} {
			want := (from == Full && (to == Ask || to == Workspace)) || (from == Workspace && to == Ask)
			if Reduction(from, to) != want {
				t.Fatal(from, to)
			}
		}
	}
}
