# Proxy AAE - Kubernetes API Server for MultiKueue + Tekton

A Kubernetes Aggregated API Extension (AAE) that provides a unified API for accessing Tekton resources across multiple worker clusters managed by MultiKueue.

## Overview

This service exposes a manager-cluster-resident API that:
- Resolves worker clusters using Kueue Workload status
- Proxies read-only calls (TaskRuns, Pods, Pod status, and logs) to the appropriate worker cluster
- Supports both HTTP and WebSocket streaming for logs

## Features

- **Worker Cluster Resolution**: Uses Kueue Workload status to determine which worker cluster to proxy to
- **Authorization**: Validates access using SelfSubjectAccessReview
- **Log Streaming**: Supports both HTTP fetch and WebSocket streaming for logs
- **Multi-Cluster Support**: Manages multiple worker clusters via kubeconfig secrets

## API Endpoints

| Endpoint | Method | Purpose |
|----------|--------|---------|
| `/api/v1/namespaces/{ns}/pipelineruns/{name}/resolve` | GET | Resolve worker cluster |
| `/api/v1/namespaces/{ns}/pipelineruns/{name}/taskruns` | GET | List TaskRuns for PipelineRun |
| `/api/v1/namespaces/{ns}/pipelineruns/{name}/pods` | GET | List Pods for PipelineRun |
| `/api/v1/namespaces/{ns}/pods/{pod}/status?pipelineRun={name}` | GET | Get Pod status |
| `/api/v1/namespaces/{ns}/logs?pipelineRun={name}&pod={pod}&container={container}` | GET | Fetch logs (HTTP) |
| `/api/v1/namespaces/{ns}/logs/stream?pipelineRun={name}&pod={pod}&container={container}` | GET | Stream logs (WebSocket) |
| `/health` | GET | Health check |
| `/ready` | GET | Readiness check |

## Installation

### Prerequisites

- Kubernetes cluster with MultiKueue and Tekton installed
- Worker clusters already configured with MultiKueueCluster resources and secrets in `kueue-system` namespace

### Deploy with ko

```bash
# Deploy with ko
make deploy
```

### Build and Deploy

```bash
# Build with ko (local)
make ko-build

# Deploy to cluster
make deploy
```

## Configuration

### Environment Variables

- `WORKERS_SECRET_NAMESPACE`: Namespace for worker kubeconfig secrets (default: `kueue-system`)
- `REQUEST_TIMEOUT`: Timeout for worker cluster requests (default: `30s`)
- `DEFAULT_LOG_TAIL_LINES`: Default number of log lines to tail (default: `100`)
- `LOG_LEVEL`: Logging level (default: `2`)

### Worker Cluster Configuration

Worker clusters are configured following the [Kueue MultiKueue pattern](https://kueue.sigs.k8s.io/docs/tasks/manage/setup_multikueue/):

1. **Secrets in kueue-system namespace**: Worker kubeconfigs are stored as secrets
2. **MultiKueueCluster resources**: Define the mapping between cluster names and secrets

```yaml
# 1. Create worker cluster secret
apiVersion: v1
kind: Secret
metadata:
  name: worker1-secret
  namespace: kueue-system
type: Opaque
stringData:
  kubeconfig: |
    <worker kubeconfig YAML>

---
# 2. Create MultiKueueCluster resource
apiVersion: kueue.x-k8s.io/v1beta1
kind: MultiKueueCluster
metadata:
  name: multikueue-test-worker1
spec:
  kubeConfig:
    locationType: Secret
    location: worker1-secret
```

### Worker Cluster RBAC Configuration

The proxy service needs read permissions on the worker clusters to access Tekton resources. Apply the following RBAC configuration to **each worker cluster**:

```yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: multikueue-tekton-reader
rules:
# Tekton Pipeline resources
- apiGroups: ["tekton.dev"]
  resources: ["pipelineruns", "taskruns", "pipelines", "tasks"]
  verbs: ["get", "list", "watch"]
# Core Kubernetes resources
- apiGroups: [""]
  resources: ["pods", "pods/log", "pods/status"]
  verbs: ["get", "list", "watch"]
# Events for debugging
- apiGroups: [""]
  resources: ["events"]
  verbs: ["get", "list", "watch"]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: multikueue-tekton-reader-binding
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: multikueue-tekton-reader
subjects:
- kind: ServiceAccount
  name: multikueue-sa
  namespace: kueue-system
```

#### Applying RBAC to Worker Clusters

1. **Extract worker cluster kubeconfig:**
   ```bash
   kubectl get secret worker1-secret -n kueue-system -o jsonpath='{.data.kubeconfig}' | base64 -d > worker-kubeconfig
   ```

2. **Apply RBAC to worker cluster:**
   ```bash
   kubectl --kubeconfig=worker-kubeconfig apply -f worker-rbac.yaml
   ```

3. **Verify permissions:**
   ```bash
   kubectl --kubeconfig=worker-kubeconfig auth can-i list taskruns --as=system:serviceaccount:kueue-system:multikueue-sa
   ```

## Usage

### Port Forward Access

```bash
# Port forward to access the service
make port-forward

# Resolve worker cluster for a PipelineRun
curl http://localhost:8080/api/v1/namespaces/default/pipelineruns/my-pipeline/resolve

# Get TaskRuns for a PipelineRun
curl http://localhost:8080/api/v1/namespaces/default/pipelineruns/my-pipeline/taskruns

# Get Pods for a PipelineRun
curl http://localhost:8080/api/v1/namespaces/default/pipelineruns/my-pipeline/pods

# Get Pod status
curl "http://localhost:8080/api/v1/namespaces/default/pods/my-pod/status?pipelineRun=my-pipeline"

# Get logs
curl "http://localhost:8080/api/v1/namespaces/default/logs?pipelineRun=my-pipeline&pod=my-pod&container=my-container"

# Stream logs (WebSocket)
# Use a WebSocket client or browser to connect to:
# ws://localhost:8080/api/v1/namespaces/default/logs/stream?pipelineRun=my-pipeline&pod=my-pod&container=my-container
```

### Response Headers

All responses include the `X-Worker-Cluster` header indicating which worker cluster the data came from.

### Error Codes

- `401`: Unauthenticated
- `403`: Forbidden (SSAR failed)
- `404`: PipelineRun/Workload not found
- `409`: Not admitted (includes nominated clusters)
- `424`: Worker config missing/unreachable
- `502/504`: Upstream errors

## Development

### Local Development

```bash
# Run locally
go run main.go --port=8080 --workers-secret-namespace=proxy-aae

# With kubeconfig
go run main.go --kubeconfig=/path/to/kubeconfig
```

### Testing

```bash
# Run tests
go test ./...

# Run with coverage
go test -cover ./...
```

## Architecture

The service consists of several components:

- **WorkloadResolver**: Resolves worker clusters from Kueue Workload status
- **WorkerConfigRegistry**: Manages worker cluster kubeconfigs
- **AuthzHandler**: Handles authorization using SelfSubjectAccessReview
- **ProxyServer**: HTTP server that routes requests to appropriate worker clusters

## Contributing

1. Fork the repository
2. Create a feature branch
3. Make your changes
4. Add tests
5. Submit a pull request

## License

This project is licensed under the Apache License 2.0.
