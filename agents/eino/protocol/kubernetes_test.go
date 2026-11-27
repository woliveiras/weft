package protocol

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

type workload struct {
	Kind     string
	Metadata struct{ Name string }
	Spec     struct {
		ServiceName string `yaml:"serviceName"`
		ClusterIP   string `yaml:"clusterIP"`
		Replicas    int
		Selector    map[string]any
		Template    struct {
			Metadata struct{ Labels map[string]string }
			Spec     struct {
				Containers []struct {
					Env []struct {
						Name, Value string
						ValueFrom   struct {
							FieldRef struct {
								FieldPath string `yaml:"fieldPath"`
							} `yaml:"fieldRef"`
						} `yaml:"valueFrom"`
					}
				}
			}
		}
	}
}

func TestKubernetesReplicaOrigins(t *testing.T) {
	manifests := map[string]workload{}
	for _, file := range []string{"weftd.yaml", "agents.yaml"} {
		f, err := os.Open(filepath.Join("..", "..", "..", "deploy", "kubernetes", file))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { f.Close() })
		decoder := yaml.NewDecoder(f)
		for {
			var doc workload
			err := decoder.Decode(&doc)
			if errors.Is(err, io.EOF) {
				break
			}
			if err != nil {
				t.Fatal(err)
			}
			manifests[doc.Kind+"/"+doc.Metadata.Name] = doc
		}
	}
	controllers := manifests["StatefulSet/weftd"]
	callback := manifests["Service/"+controllers.Spec.ServiceName]
	public := manifests["Service/weftd"]
	if controllers.Spec.Replicas < 2 || callback.Spec.ClusterIP != "None" || public.Metadata.Name == "" || public.Spec.ClusterIP == "None" || callback.Spec.Selector["app"] != controllers.Spec.Template.Metadata.Labels["app"] {
		t.Fatal("missing per-pod callback DNS or public service")
	}
	var template, instanceField string
	for _, v := range controllers.Spec.Template.Spec.Containers[0].Env {
		switch v.Name {
		case "WEFT_INTERNAL_URL":
			template = v.Value
		case "WEFT_INSTANCE":
			instanceField = v.ValueFrom.FieldRef.FieldPath
		}
	}
	if instanceField != "metadata.name" {
		t.Fatal("instance must use pod name")
	}
	origins := map[string]bool{}
	agents := manifests["Deployment/weft-agents"]
	for _, v := range agents.Spec.Template.Spec.Containers[0].Env {
		if v.Name == "WEFT_URL" {
			for _, o := range strings.Split(v.Value, ",") {
				origins[o] = true
			}
		}
	}
	for ordinal := range controllers.Spec.Replicas {
		name := fmt.Sprintf("weftd-%d", ordinal)
		origin := strings.ReplaceAll(template, "$(WEFT_INSTANCE)", name)
		if origin != "http://"+name+"."+callback.Metadata.Name+":8080" {
			t.Fatalf("wrong callback origin %q", origin)
		}
		in := TurnInput{ExecutionID: "exe_test"}
		in.Callback.Token = "fixture"
		in.Callback.URL = origin + "/internal/v1/executions/exe_test"
		if _, err := bind(&in, origins); err != nil {
			t.Fatalf("replica %d: %v", ordinal, err)
		}
	}
	if len(origins) != controllers.Spec.Replicas || origins["http://weftd:8080"] {
		t.Fatal("agents allow a destination outside the controller ordinal set")
	}
}
