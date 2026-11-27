package kubesandbox

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/woliveiras/weft/server/internal/tools"
	v1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes/scheme"
	core "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/remotecommand"
	"k8s.io/utils/ptr"
)

type Sandbox struct {
	env      map[string]string
	config   *rest.Config
	pods     core.PodInterface
	rest     rest.Interface
	template *v1.Pod
	mu       sync.Mutex
	pod      *v1.Pod
	root     string
	workdir  string
	discard  bool
	closed   bool
}

func New(r Replica) (*Sandbox, error) {
	p, err := template(r)
	if err != nil {
		return nil, err
	}
	conf, err := rest.InClusterConfig()
	if err != nil {
		return nil, fmt.Errorf("configure Kubernetes sandbox client: %w", err)
	}

	client, err := core.NewForConfig(conf)
	if err != nil {
		return nil, err
	}
	return &Sandbox{
		config: conf, pods: client.Pods(Namespace), rest: client.RESTClient(),
		template: p, root: r.Root, workdir: r.Workdir, env: maps.Clone(r.Isolation.Env),
	}, nil
}

func (*Sandbox) Kind() string { return "kubernetes" }

func (s *Sandbox) remove(ctx context.Context, pod *v1.Pod) error {
	if pod.UID == "" || pod.Labels[managed] != "true" || pod.Namespace != Namespace {
		return errors.New("invalid sandbox identity for removal")
	}
	err := s.pods.Delete(ctx, pod.Name, metav1.DeleteOptions{GracePeriodSeconds: ptr.To(int64(1)), Preconditions: &metav1.Preconditions{UID: &pod.UID}})
	if apierrors.IsNotFound(err) {
		return nil
	}
	return err
}

func (s *Sandbox) release() error {
	if s.pod == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	s.discard = true
	err := s.remove(ctx, s.pod)
	if err == nil || apierrors.IsConflict(err) {
		s.pod = nil
		s.discard = false
	}
	return err
}

func (s *Sandbox) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	return s.release()
}

// reap removes bounded-lifetime pods left behind by a lost controller or a
// lost create response. Concurrent controllers use UID delete preconditions.
func (s *Sandbox) reap(ctx context.Context) error {
	list, err := s.pods.List(ctx, metav1.ListOptions{LabelSelector: managed + "=true"})
	if err != nil {
		return err
	}
	for i := range list.Items {
		p := &list.Items[i]
		if p.DeletionTimestamp != nil || (p.Status.Phase != v1.PodFailed && p.Status.Phase != v1.PodSucceeded && time.Since(p.CreationTimestamp.Time) < (time.Duration(lifetime)*time.Second+5*time.Minute)) {
			continue
		}
		if err := s.remove(ctx, p); err != nil && !apierrors.IsConflict(err) {
			return err
		}
	}
	return nil
}

func (s *Sandbox) ready(ctx context.Context, timeout time.Duration) error {
	if s.pod != nil && (s.discard || time.Until(s.pod.CreationTimestamp.Add(time.Duration(lifetime)*time.Second)) < timeout+time.Minute) {
		if err := s.release(); err != nil {
			return err
		}
	}
	fresh := s.pod == nil
	if fresh {
		if err := s.reap(ctx); err != nil {
			return fmt.Errorf("reconcile sandbox pods: %w", err)
		}
		pod, err := s.pods.Create(ctx, s.template.DeepCopy(), metav1.CreateOptions{})
		if err != nil {
			return fmt.Errorf("create sandbox pod: %w", err)
		}
		s.pod = pod
	}
	var waiting string
	err := wait.PollUntilContextTimeout(ctx, 250*time.Millisecond, 90*time.Second, true, func(ctx context.Context) (bool, error) {
		current, err := s.pods.Get(ctx, s.pod.Name, metav1.GetOptions{})
		if err != nil {
			return false, err
		}
		if current.UID != s.pod.UID || current.DeletionTimestamp != nil {
			return false, errors.New("sandbox pod identity lost")
		}
		for _, condition := range current.Status.Conditions {
			if condition.Type == v1.PodScheduled && condition.Status == v1.ConditionFalse {
				waiting = condition.Reason
			}
		}
		switch current.Status.Phase {
		case v1.PodRunning:
			return true, nil
		case v1.PodFailed, v1.PodSucceeded:
			return false, fmt.Errorf("sandbox pod stopped (%s)", current.Status.Phase)
		}
		return false, nil
	})
	if err != nil && waiting != "" {
		return fmt.Errorf("pod %s (%s): %w", s.pod.Name, waiting, err)
	}
	if err != nil || !fresh {
		return err
	}
	// Both namespace-local claims must expose the same physical workspace.
	// Verify once per new pod before allowing a tool to modify anything.
	return s.verifyWorkspace(ctx)
}

func (s *Sandbox) verifyWorkspace(ctx context.Context) error {
	volume, err := os.OpenRoot(s.root)
	if err != nil {
		return err
	}
	defer volume.Close()
	base, err := filepath.Abs(s.root)
	if err != nil {
		return err
	}
	rel, err := filepath.Rel(base, s.workdir)
	if err != nil {
		return err
	}
	work, err := volume.OpenRoot(rel)
	if err != nil {
		return err
	}
	defer work.Close()
	if err := work.Mkdir(".weft", 0755); err != nil && !errors.Is(err, os.ErrExist) {
		return err
	}
	scratch, err := work.OpenRoot(".weft")
	if err != nil {
		return err
	}
	defer scratch.Close()
	name := "mount-check-" + rand.Text()
	marker, err := scratch.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	if err := marker.Close(); err != nil {
		return errors.Join(err, scratch.Remove(name))
	}
	data, err := json.Marshal(tools.ExecRequest{Argv: []string{"test", "-f", "/workspace/.weft/" + name}})
	if err == nil {
		var res *tools.ExecResult
		res, err = s.command(ctx, data)
		if err == nil && res.ExitCode != 0 {
			err = errors.New("sandbox claim does not expose the controller workspace")
		}
	}
	return errors.Join(err, scratch.Remove(name))
}

type boundedOutput struct{ buf bytes.Buffer }

func (w *boundedOutput) Write(p []byte) (int, error) {
	// Each stream can contain 8 MiB of head and 1 MiB of tail. JSON can
	// expand each input byte to six characters.
	if w.buf.Len()+len(p) > (18<<20)*6+4096 {
		return 0, errors.New("sandbox reply exceeds output bound")
	}
	return w.buf.Write(p)
}

func (s *Sandbox) Exec(ctx context.Context, req tools.ExecRequest) (*tools.ExecResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil, errors.New("sandbox closed")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(s.env) > 0 {
		env := maps.Clone(s.env)
		maps.Copy(env, req.Env)
		req.Env = env
	}
	data, err := json.Marshal(req)
	if err != nil || len(data) > 2<<20 {
		return nil, errors.New("invalid sandbox request size")
	}
	timeout := req.Timeout
	if timeout <= 0 {
		timeout = tools.DefaultExecTimeout
	}
	timeout = min(timeout, tools.MaxExecTimeout)
	prepare, stop := context.WithTimeout(ctx, 100*time.Second)
	err = s.ready(prepare, timeout)
	stop()
	if err != nil {
		return nil, errors.Join(fmt.Errorf("prepare replica sandbox: %w", err), s.release())
	}
	run, cancel := context.WithTimeout(ctx, timeout+10*time.Second)
	defer cancel()
	result, err := s.command(run, data)
	if err != nil {
		return nil, errors.Join(err, s.release())
	}
	if result.TimedOut {
		if err := s.release(); err != nil {
			return nil, fmt.Errorf("release timed-out sandbox: %w", err)
		}
	}
	return result, nil
}

func (s *Sandbox) command(ctx context.Context, data []byte) (*tools.ExecResult, error) {
	// The helper checks the downward-API UID in the destination container. A
	// name lookup before exec alone cannot fence a pod replacement.
	options := &v1.PodExecOptions{
		Container: "sandbox",
		Command:   []string{"/usr/local/bin/weft-sandbox-exec", string(s.pod.UID)},
		Stdin:     true, Stdout: true, Stderr: true,
	}
	url := s.rest.Post().Namespace(Namespace).Resource("pods").Name(s.pod.Name).SubResource("exec").VersionedParams(options, scheme.ParameterCodec).URL()
	runner, err := remotecommand.NewSPDYExecutor(s.config, http.MethodPost, url)
	if err != nil {
		return nil, err
	}
	var out boundedOutput
	err = runner.StreamWithContext(ctx, remotecommand.StreamOptions{Stdin: bytes.NewReader(data), Stdout: &out, Stderr: io.Discard})
	if err != nil {
		return nil, fmt.Errorf("replica sandbox exec: %w", err)
	}
	var reply struct {
		tools.ExecResult
		Code *int `json:"exitCode"`
	}
	if err := json.Unmarshal(out.buf.Bytes(), &reply); err != nil {
		return nil, fmt.Errorf("decode sandbox command result: %w", err)
	}
	if reply.Code == nil {
		return nil, errors.New("sandbox reply is missing the exit code")
	}
	reply.ExecResult.ExitCode = *reply.Code
	return &reply.ExecResult, nil
}
