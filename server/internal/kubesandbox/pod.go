// Package kubesandbox provisions and executes commands in replica-scoped pods.
package kubesandbox

import (
	"crypto/sha256"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/woliveiras/weft/server/internal/domain"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
)

const Namespace = "weft-sandboxes"
const claim = "weft-workspaces"
const managed = "weft.io/sandbox"
const lifetime int64 = 3600

type Replica struct {
	Graph, Node          string
	Index                int
	Root, Workdir, Image string
	Isolation            domain.IsolationSpec
	ReadOnly             bool
	Repositories         []domain.TaskRepository
}

func template(r Replica) (*v1.Pod, error) {
	root, err := filepath.Abs(r.Root)
	if err != nil {
		return nil, err
	}
	// Compare the lexical and physical paths. Even an in-volume symlink could
	// otherwise select a sibling graph's workspace with the same PVC.
	sub, err := filepath.Rel(root, r.Workdir)
	if err != nil || !fs.ValidPath(sub) || sub == "." {
		return nil, fmt.Errorf("workspace must be a subdirectory of the shared claim")
	}
	actual, err := filepath.EvalSymlinks(r.Workdir)
	if err != nil {
		return nil, err
	}
	physicalRoot, err := filepath.EvalSymlinks(root)
	if err != nil || actual != filepath.Join(physicalRoot, sub) {
		return nil, fmt.Errorf("sandbox workspace contains a symbolic link")
	}
	if r.Image == "" || r.Isolation.Image != "" && r.Isolation.Image != r.Image || r.Isolation.Network != "" && r.Isolation.Network != "none" {
		return nil, fmt.Errorf("sandbox requires the installation image and isolated network")
	}
	id := fmt.Sprintf("%s/%s/%d", r.Graph, r.Node, r.Index)
	hash := sha256.Sum256([]byte(id))
	mounts := []v1.VolumeMount{{Name: "workspace", MountPath: "/workspace", SubPath: sub, ReadOnly: r.ReadOnly || len(r.Repositories) > 0}, {Name: "scratch", MountPath: "/tmp"}}
	aliases := map[string]bool{}
	for _, repo := range r.Repositories {
		if !domain.ValidRepositoryAlias(repo.Alias) || aliases[repo.Alias] {
			return nil, fmt.Errorf("invalid repository mount")
		}
		aliases[repo.Alias] = true
		path := filepath.Join(actual, repo.Alias)
		p, err := filepath.EvalSymlinks(path)
		if err != nil || p != path {
			return nil, fmt.Errorf("invalid repository directory")
		}
		if info, err := os.Stat(p); err != nil || !info.IsDir() {
			return nil, fmt.Errorf("repository mount must be a directory")
		}
		mounts = append(mounts, v1.VolumeMount{Name: "workspace", MountPath: "/workspace/" + repo.Alias, SubPath: sub + "/" + repo.Alias, ReadOnly: r.ReadOnly})
	}
	cpu, mem := r.Isolation.CPU, r.Isolation.Memory
	if cpu == "" {
		cpu = "1"
	}
	if mem == "" {
		mem = "512Mi"
	}
	c, err := resource.ParseQuantity(cpu)
	if err != nil || c.Sign() <= 0 {
		return nil, fmt.Errorf("invalid sandbox CPU limit")
	}
	m, err := resource.ParseQuantity(mem)
	if err != nil || m.Sign() <= 0 {
		return nil, fmt.Errorf("invalid sandbox memory limit")
	}
	resources := v1.ResourceList{v1.ResourceCPU: c, v1.ResourceMemory: m, v1.ResourceEphemeralStorage: resource.MustParse("1Gi")}
	container := v1.Container{
		Name:            "sandbox",
		Image:           r.Image,
		ImagePullPolicy: v1.PullIfNotPresent,
		Command:         []string{"sleep", "infinity"},
		WorkingDir:      "/workspace",
		Env:             []v1.EnvVar{{Name: "WEFT_POD_UID", ValueFrom: &v1.EnvVarSource{FieldRef: &v1.ObjectFieldSelector{FieldPath: "metadata.uid"}}}},
		Resources:       v1.ResourceRequirements{Limits: resources, Requests: resources},
		VolumeMounts:    mounts,
		SecurityContext: &v1.SecurityContext{
			AllowPrivilegeEscalation: ptr.To(false),
			ReadOnlyRootFilesystem:   ptr.To(true),
			Capabilities:             &v1.Capabilities{Drop: []v1.Capability{"ALL"}},
		},
	}
	return &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: fmt.Sprintf("weft-%x-", hash[:10]),
			Namespace:    Namespace,
			Labels:       map[string]string{managed: "true"},
			Annotations:  map[string]string{"weft.io/replica": id},
		},
		Spec: v1.PodSpec{
			ServiceAccountName:            "sandbox",
			AutomountServiceAccountToken:  ptr.To(false),
			EnableServiceLinks:            ptr.To(false),
			RestartPolicy:                 v1.RestartPolicyNever,
			ActiveDeadlineSeconds:         ptr.To(lifetime),
			TerminationGracePeriodSeconds: ptr.To(int64(1)),
			SecurityContext: &v1.PodSecurityContext{
				RunAsNonRoot:   ptr.To(true),
				RunAsUser:      ptr.To(int64(10001)),
				RunAsGroup:     ptr.To(int64(10001)),
				SeccompProfile: &v1.SeccompProfile{Type: v1.SeccompProfileTypeRuntimeDefault},
			},
			Volumes: []v1.Volume{
				{Name: "workspace", VolumeSource: v1.VolumeSource{PersistentVolumeClaim: &v1.PersistentVolumeClaimVolumeSource{ClaimName: claim}}},
				{Name: "scratch", VolumeSource: v1.VolumeSource{EmptyDir: &v1.EmptyDirVolumeSource{SizeLimit: ptr.To(resource.MustParse("1Gi"))}}},
			},
			Containers: []v1.Container{container},
		},
	}, nil
}
