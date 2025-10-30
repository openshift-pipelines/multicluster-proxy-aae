# Installation Guide

## Prerequisites

### Install ko

```bash
# Install ko (Kubernetes Object Builder)
go install github.com/google/ko@latest

# Or using curl
curl -L https://github.com/google/ko/releases/latest/download/ko_Linux_x86_64.tar.gz | tar xzf - ko
sudo mv ko /usr/local/bin/
```

### Verify ko installation

```bash
ko version
```

## Quick Start

### 1. Clone the repository

```bash
git clone https://github.com/khrm/proxy-aae.git
cd proxy-aae
```

### 2. Install dependencies

```bash
make deps
```

### 3. Deploy to Kubernetes

```bash
# Deploy with ko (recommended)
make deploy

# Or use the single file deployment
make deploy-ko-single
```

### 4. Configure worker clusters

```bash
# Apply example worker configuration
kubectl apply -f config/example-worker-config.yaml

# Or create your own worker cluster secrets
kubectl create secret generic worker-cluster1 \
  --from-file=kubeconfig=/path/to/worker-kubeconfig \
  --namespace=proxy-aae \
  --dry-run=client -o yaml | \
  kubectl label --local -f - proxy.tekton.dev/worker-config=true proxy.tekton.dev/cluster-name=cluster1 | \
  kubectl apply -f -
```

### 5. Test the deployment

```bash
# Port forward to access the service
make port-forward

# Test the health endpoint
curl http://localhost:8080/health

# Test the ready endpoint
curl http://localhost:8080/ready
```

## Development

### Local development

```bash
# Run locally
make run

# Build and test
make build
make test
```

### Building with ko

```bash
# Build locally with ko
make ko-build

# Build and push to registry
ko build --push .
```

## Troubleshooting

### Check deployment status

```bash
kubectl get pods -n proxy-aae
kubectl logs -n proxy-aae deployment/proxy-aae
```

### Check service status

```bash
kubectl get svc -n proxy-aae
kubectl describe svc proxy-aae -n proxy-aae
```

### Check RBAC

```bash
kubectl get clusterrole proxy-aae
kubectl get clusterrolebinding proxy-aae
kubectl get serviceaccount proxy-aae -n proxy-aae
```

## Configuration

### Environment Variables

The following environment variables can be configured:

- `WORKERS_SECRET_NAMESPACE`: Namespace for worker kubeconfig secrets (default: `proxy-aae`)
- `REQUEST_TIMEOUT`: Timeout for worker cluster requests (default: `30s`)
- `DEFAULT_LOG_TAIL_LINES`: Default number of log lines to tail (default: `100`)
- `LOG_LEVEL`: Logging level (default: `2`)

### Worker Cluster Configuration

Worker clusters are configured via Kubernetes Secrets with the following labels:

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: worker-<clusterName>
  namespace: proxy-aae
  labels:
    proxy.tekton.dev/worker-config: "true"
    proxy.tekton.dev/cluster-name: "<clusterName>"
type: Opaque
stringData:
  kubeconfig: |
    <worker kubeconfig YAML>
```

## Cleanup

### Remove the deployment

```bash
make undeploy
```

### Or remove manually

```bash
kubectl delete -f config/ko-deploy.yaml
```
