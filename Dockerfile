# Build stage
FROM registry.access.redhat.com/hi/go:1.27@sha256:5032771340ee44e4bb1e5792e49a10e97929277d4f64535cf44232bd13170cbf AS builder

WORKDIR /opt/app-root/src

# Copy go mod files
COPY --chown=1001:0 go.mod go.sum ./

# Copy source code
COPY --chown=1001:0 . .

# Build the application
RUN go build -o proxy-aae ./cmd/proxy-server/main.go

# Final stage
FROM registry.access.redhat.com/ubi9/ubi-minimal@sha256:f26ef64172ea61f5ac5ba4be8c5c738bf8760b22b3e5148ce7b40f7308ab32db

WORKDIR /

# Copy the binary from builder stage
COPY --from=builder /opt/app-root/src/proxy-aae .

# Use nonroot user
USER 65532:65532

# Expose port
EXPOSE 8080

# Run the application
ENTRYPOINT ["/proxy-aae"]

LABEL \
    com.redhat.component="openshift-multicluster-proxy-aae-rhel9-container" \
    cpe="cpe:/a:redhat:openshift_pipelines:0.1::el9" \
    description="Red Hat OpenShift Pipelines multicluster-proxy-aae multicluster-proxy-aae" \
    io.k8s.description="Red Hat OpenShift Pipelines multicluster-proxy-aae multicluster-proxy-aae" \
    io.k8s.display-name="Red Hat OpenShift Pipelines multicluster-proxy-aae multicluster-proxy-aae" \
    io.openshift.tags="tekton,openshift,multicluster-proxy-aae,multicluster-proxy-aae" \
    maintainer="pipelines-extcomm@redhat.com" \
    name="openshift-pipelines/multicluster-proxy-aae-rhel9" \
    summary="Red Hat OpenShift Pipelines multicluster-proxy-aae multicluster-proxy-aae" \
    version="v0.1.1"
