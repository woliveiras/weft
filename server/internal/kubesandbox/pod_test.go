package kubesandbox

import (
	"github.com/woliveiras/weft/server/internal/domain"
	"os"
	"path/filepath"
	"testing"
)

func TestReplicaPod(t *testing.T) {
	root := t.TempDir()
	work := filepath.Join(root, "graph")
	if err := os.MkdirAll(filepath.Join(work, "api"), 0755); err != nil {
		t.Fatal(err)
	}
	r := Replica{Root: root, Workdir: work, Image: "sandbox", Graph: "graph", Node: "qa", Repositories: []domain.TaskRepository{{Alias: "api"}}}
	p, err := template(r)
	if err != nil {
		t.Fatal(err)
	}
	m := p.Spec.Containers[0].VolumeMounts
	if p.Namespace != Namespace || *p.Spec.AutomountServiceAccountToken || !m[0].ReadOnly || m[0].SubPath != "graph" || m[2].ReadOnly || m[2].SubPath != "graph/api" {
		t.Fatal("unexpected sandbox mounts or identity", m)
	}
	r.Index++
	r.ReadOnly = true
	next, err := template(r)
	if err != nil || next.GenerateName == p.GenerateName || !next.Spec.Containers[0].VolumeMounts[2].ReadOnly {
		t.Fatal("unexpected second replica", err)
	}
	link := filepath.Join(root, "alias")
	if err := os.Symlink(work, link); err != nil {
		t.Fatal(err)
	}
	for name, modify := range map[string]func(*Replica){
		"root":      func(r *Replica) { r.Workdir = root },
		"outside":   func(r *Replica) { r.Workdir = filepath.Dir(root) },
		"symlink":   func(r *Replica) { r.Workdir = link },
		"repo":      func(r *Replica) { r.Repositories = []domain.TaskRepository{{Alias: "../api"}} },
		"duplicate": func(r *Replica) { r.Repositories = append(r.Repositories, r.Repositories[0]) },
		"image":     func(r *Replica) { r.Isolation.Image = "other" },
		"network":   func(r *Replica) { r.Isolation.Network = "bridge" },
		"memory":    func(r *Replica) { r.Isolation.Memory = "0" },
		"cpu":       func(r *Replica) { r.Isolation.CPU = "invalid" },
	} {
		t.Run(name, func(t *testing.T) {
			invalid := r
			modify(&invalid)
			if _, err := template(invalid); err == nil {
				t.Fatal("accepted invalid pod")
			}
		})
	}
}
