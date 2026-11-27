# Replica sandboxes on Kubernetes

The controller provisions a pod on the first command sent to a graph, node and replica. Later commands reuse its workspace and pod. This applies equally to a graph started from Chat, the console or the API. File and model tools use their existing gateways. Tool grants, reviewed task consent and mandatory approvals still apply. The base installation keeps commands unavailable until this overlay is configured.

## Installation

1. Use a cluster with Pod Security Admission, stable `admissionregistration.k8s.io/v1` ValidatingAdmissionPolicy and an enforcing network policy implementation. Reserve the `weft-sandboxes` namespace for this installation. Verify connectivity with both sides of the health probe below.
2. Provide two static PersistentVolumes backed by the **same filesystem export and directory**. Bind the controller volume to `weft/weft-workspaces` and the other volume, named `weft-sandbox-workspaces`, to `weft-sandboxes/weft-workspaces`. Configure the controller claim for its static PV before binding. Use ReadWriteMany, Retain and a backend supporting multiple PV views. Grant UID/GID 10001 access. Both claims need sufficient capacity. An existing bound claim requires a storage-specific plan before changing its backing. Sandbox cleanup only removes disposable pods.
3. Build the sandbox image from the repository root with `docker build -f deploy/kubernetes-sandboxes/Dockerfile -t weft/sandbox-go:dev .`. For an installation, set the same trusted image reference, preferably by digest, in `controller.yaml` and `admission.yaml`. The workload contains Go, Git, Make and the execution helper. Network-free checks need their source and dependencies available locally.
4. Replace the documentation address in `api-egress.yaml` with the Kubernetes API destination and port used by the controller under your CNI. Confirm that it can create and exec pods in the sandbox namespace. The controller uses its projected service-account credential. Workload pods disable token automount and service links.
5. Render with `kubectl kustomize deploy/kubernetes-sandboxes`. Install the admission policy, binding, namespace restrictions and network isolation before enabling the controller image setting. Use the ordinary Weft maintenance and rollout process to update the controller. A graph author needs no access to Kubernetes.

## Execution and recovery

The API admits the installation image, its fixed entrypoint, one workspace PVC and a bounded scratch volume. The pod runs as UID 10001 with a read-only root filesystem, dropped capabilities and runtime-default seccomp. Workspaces outside the claim and symbolic-link aliases fail preparation. Each newly created pod must see a temporary file created in its exact workspace by the controller before it can receive a tool command. The helper validates its pod UID before executing the submitted argv. A graph's declared read-only workspace becomes a read-only mount. Reviewed multi-repository layouts use a read-only parent and separately scoped mounts. Task review retains its existing eligibility rules.

The default pod requests and limits are 1 CPU, 512 MiB of memory and 1 GiB of ephemeral storage. Quota bounds the pod count and aggregate resource limits. Size the cluster and its quota for simultaneously active replicas. Network policy denies inbound and outbound pod traffic. Only the controller calls the Kubernetes API for exec. Verify that platform services are unreachable by IP from a sandbox on the installed CNI.

Normal replica shutdown removes its pod using the observed UID. Timeout, cancellation or an interrupted stream discards the pod. Workspace files survive. An abandoned pod has a one-hour active deadline. The next provisioning attempt reaps finished and expired managed pods. A failed command stream can have changed files before it failed. Inspect task history before retrying a non-idempotent action.

## Parallel Git branches

With this overlay, a `workspace.mode: worktree` node gets a private repository on the workspace claim for each replica. A fresh clone starts from its previously published branch when one exists, otherwise from the graph checkout's HEAD. The pod can use `git status`, `git add` and `git commit` offline. Its initial Git identity is `Weft Agent <agent@weft.local>`. A pod restart reuses the directory, including uncommitted files. Graph inspection and checkpoint history can read the retained private replicas.

Weft transfers committed history as a Git bundle. The export pod mounts only its source in read-only mode. The receiver mounts only the destination. The controller buffers up to 64 MiB per transfer in a temporary file and removes the file after the operation. The transfer carries one branch and creates no Git remote. Budget controller scratch space, pod quota and scheduling capacity for transfers alongside running agents.

The publication operation targets the replica's local branch in the graph repository. It takes the source commit and the expected current destination commit. An empty expected destination requests branch creation. A conflicting destination, a divergent history or a checked-out destination prevents the update. The integrator can merge an explicitly published branch from the shared graph repository. An operator can publish through the [replica API](../../docs/api.md#publish-a-private-replica-branch). An agent needs an exact `git.publish` grant on that replica. `git.write` operates inside the clone.

The source graph repository needs its Git metadata inside the mounted directory. An existing linked replica worktree requires an explicit migration that preserves its pending edits. Standalone linked worktrees keep their current lifecycle.

## Qualification

In a disposable cluster, mount both claims against one test directory. Compile the Linux controller fixtures with `./scripts/go.sh test -c -tags kubernetes_e2e -o bin/qa-replica.test ./internal/kubesandbox` and the corresponding command for `./internal/api`. Run `TestLiveReplicaPods` and `TestKubernetesEinoFactory` separately in a test controller pod with service account `weftd`, label `app=weftd` and `WEFT_FIXTURE_WORKSPACES` set to the mounted claim root. The factory test needs Git, the event schema and example graph at their repository paths and a writable temporary directory. In the test namespace, route `factory-controller:8080` to that pod. Configure the bundled Go agents on `weft-agents:8000` with the matching callback origin and the fixture's synthetic runtime secret. The store and broker used by this test are SQLite and the filesystem broker.

The cluster qualification checks access reviews, rejected pod shapes, real process exit and network denial. Run the same positive and negative network checks after changing the CNI. The evidence, including the private Git clone rehearsal, is recorded in [ENV-010](../../specs/workspaces-and-environments/kubernetes-replica-sandbox/matrix.md).
