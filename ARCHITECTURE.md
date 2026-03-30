# Spark Operator Architecture

This document provides an architectural overview of the Kubernetes Operator for Apache Spark (Spark Operator).

## Overview

The Spark Operator is a Kubernetes operator that manages the lifecycle of Apache Spark applications on Kubernetes. It uses Custom Resource Definitions (CRDs) to extend the Kubernetes API with `SparkApplication` and `ScheduledSparkApplication` resources.

```
┌──────────────────────────────────────────────────────────────────┐
│                        Kubernetes Cluster                         │
│                                                                    │
│  ┌──────────────────┐         ┌──────────────────┐               │
│  │  SparkApplication│         │ScheduledSpark    │               │
│  │     CRDs         │         │Application CRDs  │               │
│  └────────┬─────────┘         └────────┬─────────┘               │
│           │                            │                          │
│           ▼                            ▼                          │
│  ┌────────────────────────────────────────────────┐              │
│  │         Spark Operator Controller              │              │
│  │  ┌──────────────┐  ┌─────────────────────┐    │              │
│  │  │SparkApp      │  │ScheduledSparkApp    │    │              │
│  │  │Controller    │  │Controller (Cron)    │    │              │
│  │  └──────┬───────┘  └─────────┬───────────┘    │              │
│  └─────────┼──────────────────────┼────────────────┘              │
│            │                      │                               │
│            ▼                      ▼                               │
│  ┌─────────────────────────────────────────────┐                 │
│  │         Mutating Webhook                    │                 │
│  │  (Pod Customization & Validation)           │                 │
│  └─────────────────┬───────────────────────────┘                 │
│                    │                                              │
│                    ▼                                              │
│  ┌──────────────────────────────────────────────────────┐        │
│  │           Spark Driver & Executor Pods               │        │
│  │  ┌──────────┐  ┌──────────┐   ┌──────────┐          │        │
│  │  │ Driver   │  │Executor  │...│Executor  │          │        │
│  │  │   Pod    │  │  Pod 1   │   │  Pod N   │          │        │
│  │  └──────────┘  └──────────┘   └──────────┘          │        │
│  └──────────────────────────────────────────────────────┘        │
└──────────────────────────────────────────────────────────────────┘
```

## Core Components

### 1. Custom Resource Definitions (CRDs)

The operator defines two primary CRDs:

#### SparkApplication (`api/v1beta2/sparkapplication_types.go`)
- Represents a single Spark application to be run on Kubernetes
- Spec includes:
  - Spark image and version
  - Driver and executor configurations
  - Application code location
  - Dependencies and environment variables
  - Scheduling options (batch schedulers like Volcano, YuniKorn)
  - Monitoring and metrics configuration

#### ScheduledSparkApplication (`api/v1beta2/scheduledsparkapplication_types.go`)
- Wraps SparkApplication with cron-based scheduling
- Allows recurring Spark jobs using cron expressions
- Maintains history of executed applications

### 2. Controllers

Located in `internal/controller/`, the controllers watch CRD resources and reconcile their desired state.

#### SparkApplication Controller (`internal/controller/sparkapplication/`)
**Responsibilities:**
- Watch SparkApplication resources
- Submit applications to Spark using `spark-submit`
- Monitor application status and update CRD status
- Handle application lifecycle (submission, running, completion, failure)
- Manage driver and executor pod lifecycle
- Configure monitoring and metrics collection
- Support batch schedulers (Volcano, YuniKorn, Kube-scheduler)

**Key Files:**
- `controller.go` - Main reconciliation logic
- `submission.go` - spark-submit wrapper and execution
- `validator.go` - Application specification validation
- `monitoring_config.go` - Prometheus metrics configuration
- `web_ui.go` - Spark UI service management

#### ScheduledSparkApplication Controller (`internal/controller/scheduledsparkapplication/`)
**Responsibilities:**
- Manage cron-based scheduling of Spark applications
- Create SparkApplication instances based on schedule
- Track execution history and handle concurrency policies
- Clean up completed/failed application instances

### 3. Mutating Webhook (`internal/webhook/`)

The webhook intercepts pod creation events and customizes Spark driver/executor pods before they're created.

**Functions:**
- Pod mutation (`sparkpod_defaulter.go`):
  - Add volumes and volume mounts
  - Set environment variables
  - Configure affinity/anti-affinity
  - Add init containers and sidecars
  - Configure resource quotas
- Validation (`sparkapplication_validator.go`, `scheduledsparkapplication_validator.go`):
  - Validate application specifications
  - Check resource quota compliance
  - Ensure required fields are present

### 4. Support Components

#### Metrics (`internal/metrics/`)
- Export Prometheus metrics for SparkApplications
- Track submission rates, completion rates, failures
- Monitor pod states (driver/executor metrics)

#### Schedulers (`internal/scheduler/`)
- Integration with batch schedulers:
  - **Volcano** (`scheduler/volcano/`) - Gang scheduling for Spark
  - **YuniKorn** (`scheduler/yunikorn/`) - Resource-aware scheduling
  - **Kube-scheduler** (`scheduler/kubescheduler/`) - Default Kubernetes scheduler

#### Certificate Management (`pkg/certificate/`)
- Manage TLS certificates for webhook server
- Auto-rotation and renewal

## Workflow

### SparkApplication Lifecycle

1. **User creates SparkApplication**
   \`\`\`yaml
   apiVersion: sparkoperator.k8s.io/v1beta2
   kind: SparkApplication
   metadata:
     name: spark-pi
   spec:
     type: Scala
     mode: cluster
     image: spark:latest
     mainClass: org.apache.spark.examples.SparkPi
     ...
   \`\`\`

2. **Controller watches and reconciles**
   - Controller detects new SparkApplication
   - Validates specification
   - Prepares spark-submit command
   - Submits application to Spark

3. **Spark creates driver pod**
   - Kubernetes creates driver pod
   - Webhook intercepts and customizes pod
   - Driver pod starts running

4. **Driver requests executors**
   - Driver asks Kubernetes for executor pods
   - Webhook customizes executor pods
   - Executors start and connect to driver

5. **Application runs**
   - Controller monitors pod states
   - Updates SparkApplication status
   - Exposes metrics to Prometheus

6. **Completion**
   - Application completes (success or failure)
   - Controller updates final status
   - Cleanup based on TTL and restart policy

### ScheduledSparkApplication Lifecycle

1. **User creates ScheduledSparkApplication**
   - Defines SparkApplication template
   - Specifies cron schedule
   - Sets concurrency policy

2. **Controller evaluates schedule**
   - Checks if it's time to run
   - Applies concurrency policy
   - Creates new SparkApplication instance

3. **SparkApplication runs** (follows normal lifecycle above)

4. **History management**
   - Tracks successful/failed runs
   - Cleans up old instances based on history limits

## Project Structure

\`\`\`
.
├── api/                          # CRD definitions and types
│   ├── v1alpha1/                 # Alpha API version (SparkConnect)
│   └── v1beta2/                  # Stable API version
├── charts/                       # Helm chart for deployment
├── cmd/operator/                 # Main entry points
│   ├── controller/               # Controller manager
│   └── webhook/                  # Webhook server
├── config/                       # Kubernetes manifests
│   ├── crd/                      # CRD definitions
│   ├── manager/                  # Operator deployment
│   ├── rbac/                     # RBAC roles and bindings
│   └── webhook/                  # Webhook configuration
├── internal/                     # Internal packages
│   ├── controller/               # Controllers
│   ├── metrics/                  # Metrics exporters
│   ├── scheduler/                # Batch scheduler integrations
│   └── webhook/                  # Webhook handlers
├── pkg/                          # Public packages
│   ├── certificate/              # Certificate management
│   ├── client/                   # Generated Kubernetes clients
│   └── common/                   # Shared constants and utilities
├── examples/                     # Example applications
└── test/                         # E2E and integration tests
\`\`\`

## Development

### Building

\`\`\`bash
# Build operator binary
make build

# Build Docker image
make docker-build

# Run tests
make test

# Run E2E tests
make e2e-test
\`\`\`

### Testing Locally

\`\`\`bash
# Install CRDs
make install

# Run operator locally (requires kubeconfig)
make run

# Submit example application
kubectl apply -f examples/spark-pi.yaml
\`\`\`

### Code Generation

The operator uses Kubebuilder for scaffolding and code generation:

\`\`\`bash
# Generate CRD manifests
make manifests

# Generate deep copy methods
make generate

# Update API documentation
make api-docs
\`\`\`

## Configuration

### Operator Configuration
- **Controller Manager** flags (`cmd/operator/controller/start.go`):
  - Namespace to watch
  - Resync period
  - Worker threads
  - Metrics address

### SparkApplication Configuration
Key spec fields:
- `type`: Language (Scala, Java, Python, R)
- `mode`: Deployment mode (cluster, client)
- `image`: Spark Docker image
- `driver/executor`: Resource requests/limits, configuration
- `deps`: JARs, files, pyFiles
- `monitoring`: Prometheus, metrics configuration
- `batchScheduler`: Volcano, YuniKorn configuration

## Security

### RBAC
- Operator requires cluster-admin for CRD management
- SparkApplications run with service accounts
- Fine-grained RBAC roles for different components

### Pod Security
- Webhook validates and can enforce pod security policies
- Resource quotas prevent resource exhaustion
- Network policies can isolate Spark jobs

### Secrets Management
- Support for imagePullSecrets
- Hadoop configuration secrets
- Custom secrets mounted as volumes or env vars

## Performance & Scalability

### Resource Management
- Configurable resource requests/limits per driver/executor
- Dynamic allocation support
- Resource quota integration

### Batch Scheduling
- **Volcano**: Gang scheduling ensures all executors start together
- **YuniKorn**: Queue-based scheduling with fairness
- Prevents resource fragmentation

### Monitoring
- Prometheus metrics for operator health
- Application-level metrics via Spark metrics
- Custom metrics exporters

## References

- [User Guide](https://www.kubeflow.org/docs/components/spark-operator/user-guide/)
- [API Documentation](docs/api-docs.md)
- [Developer Guide](https://www.kubeflow.org/docs/components/spark-operator/developer-guide/)
- [Kubebuilder Book](https://book.kubebuilder.io/)
