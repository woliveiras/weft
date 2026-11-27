# Weft on Kubernetes

```bash
# 1. PostgreSQL: install an operator (e.g. CloudNativePG) and create cluster "weft-postgres",
#    or use a managed PostgreSQL; put its DSN in secret.yaml.
# 2. Edit secret.yaml (or replace it with an ExternalSecret), configmap.yaml, and the image tags.
kubectl apply -k deploy/kubernetes
# 3. Expose weftd:8080 through your ingress (TLS). Do NOT route /internal/ publicly.
# 4. Agent runtimes for graph nodes that use runtime.image:
weft k8s render -f examples/graphs/software-factory.yaml --namespace weft | kubectl apply -f -
```

See [docs/deployment.md](../../docs/deployment.md) and
[docs/production-hardening.md](../../docs/production-hardening.md).

Notes:
- The service-account tokens are not mounted: weftd does not talk to the Kubernetes API.
- Nodes using `isolation.mode: kubernetes` need a replica sandbox for command tools. An unconfigured sandbox returns a tool error. Ordinary model and file callbacks still work. A graph that explicitly selects process isolation runs commands under the controller UID. Per-replica workload qualification is tracked in [ENV-010](../../specs/workspaces-and-environments/kubernetes-replica-sandbox/matrix.md).
- The workspace volume must be ReadWriteMany when weftd has more than one replica.


## Controller callbacks and upgrades

The public `weftd` Service balances API requests. Each controller advertises its own address through the headless `weftd-callback` Service. For example, a turn started by `weftd-1` calls back to `http://weftd-1.weftd-callback:8080`. The agents deployment lists both controller origins in `WEFT_URL`. When scaling up, roll out the extended list to agents before adding the controller replica. When scaling down, drain work and remove the retired origin after its turns settle. A restarted controller retains its DNS name and invalidates the capabilities from the previous process. Weft redelivers interrupted work with a fresh capability. [Kubernetes StatefulSet network identity](https://kubernetes.io/docs/concepts/workloads/controllers/statefulset/#stable-network-id)

For an existing installation, schedule a maintenance window, stop new admissions and drain active turns. Bring the previous `Deployment/weftd` to zero replicas before starting `StatefulSet/weftd`. Applying the new manifests alone leaves the old Deployment in the namespace. Preserve PostgreSQL, the broker and the RWX workspace claim. Check readiness for both controller ordinals and exercise a turn from each before reopening admissions. For rollback, drain the StatefulSet before restoring the previous controller and agents together.
