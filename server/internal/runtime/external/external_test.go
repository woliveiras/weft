package external

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/runtime"
)

func TestEndpointOnlyRejectsLocalLaunch(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "launched")
	rt := New(Options{EndpointOnly: true})
	rc := runtime.ReplicaContext{Graph: &domain.Graph{}, Node: &domain.Node{Runtime: domain.RuntimeRef{Command: []string{"touch", marker}}}}
	err := rt.Prepare(t.Context(), rc)
	if err == nil || !strings.Contains(err.Error(), "runtime.endpoint") {
		t.Fatal(err)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatal("runtime launched locally", err)
	}
}
