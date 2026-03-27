# Spark Operator Architecture

This document provides a high-level overview of the Spark Operator architecture, its core components, and how they interact to manage Apache Spark applications on Kubernetes.

## Overview

The Kubernetes Operator for Apache Spark (Spark Operator) enables running Apache Spark applications natively on Kubernetes using custom resources. It follows the Kubernetes Operator pattern, watching for custom resource changes and reconciling the desired state with the actual state of Spark applications in the cluster.

## Architecture Diagram

```
┌─────────────────────────────────────────────────────────────┐
│                        Kubernetes API                        │
└──────────────────┬──────────────────────────────────────────┘
                   │
                   │ Watch/Update
                   │
┌──────────────────▼──────────────────────────────────────────┐
│                    Spark Operator                            │
│  ┌──────────────────────────────────────────────────────┐   │
│  │              Controllers                             │   │
│  │  • SparkApplication Controller                       │   │
│  │  • ScheduledSparkApplication Controller              │   │
│  │  • MutatingWebhookConfiguration Controller           │   │
│  └──────────────────────────────────────────────────────┘   │
│  ┌──────────────────────────────────────────────────────┐   │
│  │              Mutating Webhook                        │   │
│  │  • Pod mutation and injection                        │   │
│  │  • Volume mounting                                   │   │
│  │  • Resource customization                            │   │
│  └──────────────────────────────────────────────────────┘   │
│  ┌──────────────────────────────────────────────────────┐   │
│  │              Support Components                      │   │
│  │  • Metrics exporter (Prometheus)                     │   │
│  │  • Scheduler extender                                │   │
│  │  • Client libraries                                  │   │
│  └──────────────────────────────────────────────────────┘   │
└─────────────────────────────────────────────────────────────┘
                   │
                   │ Creates/Manages
                   │
┌──────────────────▼──────────────────────────────────────────┐
│              Spark Application Pods                          │
│  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐      │
│  │    Driver    │  │  Executor 1  │  │  Executor N  │      │
│  │     Pod      │  │     Pod      │  │     Pod      │      │
│  └──────────────┘  └──────────────┘  └──────────────┘      │
└─────────────────────────────────────────────────────────────┘
```

## Core Components

### 1. Custom Resource Definitions (CRDs)

#### SparkApplication
The primary CRD that represents a Spark application to be run on Kubernetes. It includes:
- Application metadata (name, namespace, labels)
- Spark configuration (driver/executor settings, Spark conf)
- Dependencies (jars, files, pyFiles)
- Resource specifications
- Monitoring and metrics configuration

**Location:** `api/v1beta2/sparkapplication_types.go`

#### ScheduledSparkApplication
Extends SparkApplication with cron-based scheduling capabilities for recurring Spark jobs.

**Location:** `api/v1beta2/scheduledsparkapplication_types.go`

### 2. Controllers

Controllers implement the reconciliation logic using the controller-runtime framework.

#### SparkApplication Controller
**Location:** `internal/controller/sparkapplication/`

**Responsibilities:**
- Watches SparkApplication resources
- Manages application lifecycle (submission, monitoring, cleanup)
- Handles application state transitions
- Manages driver and executor pods
- Configures driver ingress for UI access
- Exports metrics to Prometheus
- Implements retry logic for failed submissions
- Manages automatic restart policies

**Key Files:**
- `controller.go` - Main reconciliation logic
- `submission.go` - Spark application submission handling
- `driveringress.go` - Driver UI ingress management
- `monitoring_config.go` - Metrics and monitoring setup

#### ScheduledSparkApplication Controller
**Location:** `internal/controller/scheduledsparkapplication/`

**Responsibilities:**
- Watches ScheduledSparkApplication resources
- Manages cron-based scheduling
- Creates SparkApplication instances based on schedule
- Handles schedule updates and deletions

#### MutatingWebhookConfiguration Controller
**Location:** `internal/controller/mutatingwebhookconfiguration/`

**Responsibilities:**
- Manages the lifecycle of MutatingWebhookConfiguration
- Ensures webhook certificate validity
- Maintains webhook registration with the API server

### 3. Mutating Admission Webhook

**Location:** `internal/webhook/`

The webhook intercepts pod creation requests for Spark driver and executor pods, enabling:
- Mounting additional volumes (ConfigMaps, Secrets, PVCs)
- Injecting environment variables
- Setting pod affinity/anti-affinity rules
- Applying node selectors and tolerations
- Adding custom annotations and labels
- Resource quota enforcement

This allows customization beyond what Spark natively supports through its Kubernetes backend.

### 4. Support Components

#### Metrics Exporter
**Location:** `internal/metrics/`

Exports application-level and pod-level metrics to Prometheus for monitoring:
- Application state and lifecycle events
- Submission success/failure rates
- Execution duration
- Resource utilization

#### Scheduler Extender
**Location:** `internal/scheduler/`

Provides advanced scheduling capabilities:
- Gang scheduling for driver and executors
- Resource quota management
- Custom scheduling policies

#### Client Libraries
**Location:** `pkg/client/`

Generated clientsets for programmatic interaction with SparkApplication and ScheduledSparkApplication resources.

### 5. Utilities and Helpers

**Location:** `pkg/`

- **certificate:** TLS certificate management for webhooks
- **common:** Shared constants and utilities
- **features:** Feature gate management
- **scheme:** Kubernetes scheme registration
- **util:** General-purpose helper functions

## Workflow

### SparkApplication Lifecycle

1. **Creation**
   - User creates a SparkApplication CR via kubectl or API
   - Controller detects the new resource through watch mechanism

2. **Validation**
   - Controller validates the SparkApplication spec
   - Ensures required fields are present
   - Validates resource specifications

3. **Submission**
   - Controller generates spark-submit command
   - Creates driver pod with appropriate configuration
   - Driver pod is mutated by webhook (if enabled)

4. **Execution**
   - Driver pod starts and requests executor pods from Kubernetes
   - Executor pods are created and mutated by webhook
   - Application runs with configured resources

5. **Monitoring**
   - Controller monitors driver and executor pod status
   - Updates SparkApplication status with current state
   - Exports metrics to Prometheus

6. **Completion/Failure**
   - Controller detects completion or failure
   - Updates final status
   - Handles cleanup based on configuration
   - Triggers retry/restart if configured

7. **Cleanup**
   - Removes executor pods
   - Optionally removes driver pod based on configuration
   - Maintains application history

### Scheduled Application Workflow

1. **Schedule Evaluation**
   - ScheduledSparkApplication controller evaluates cron schedule
   - Determines if a new run should be triggered

2. **SparkApplication Creation**
   - Creates a new SparkApplication instance from template
   - Adds scheduling metadata and labels

3. **Concurrency Management**
   - Enforces concurrency policy (Allow/Forbid/Replace)
   - Manages active runs based on policy

4. **History Management**
   - Maintains history of successful/failed runs
   - Prunes old runs based on configured limits

## Configuration and Deployment

### Installation Methods

1. **Helm Chart**
   - Recommended for production deployments
   - Location: `charts/spark-operator-chart/`
   - Supports extensive customization via values

2. **Kustomize**
   - Location: `config/`
   - For declarative configuration management

3. **Operator-based**
   - Can be deployed via operator framework
   - Provides lifecycle management

### Configuration Options

- **Webhook:** Enable/disable mutating webhook
- **Namespace:** Single or multi-namespace mode
- **RBAC:** Service accounts and role bindings
- **Resource Quotas:** CPU/memory limits for operator
- **Metrics:** Prometheus integration settings
- **Image Pull:** Registry and pull secrets configuration

## Development Guidelines

### Project Structure

```
spark-operator/
├── api/                      # CRD definitions and API types
│   └── v1beta2/              # Current API version
├── internal/                 # Internal packages
│   ├── controller/           # Controller implementations
│   ├── metrics/              # Metrics exporters
│   ├── scheduler/            # Scheduler components
│   └── webhook/              # Webhook handlers
├── pkg/                      # Public libraries
│   ├── client/               # Generated clients
│   ├── common/               # Shared utilities
│   └── util/                 # Helper functions
├── charts/                   # Helm charts
├── config/                   # Kustomize configs and CRD manifests
├── examples/                 # Example SparkApplication YAMLs
└── test/                     # Test suites

```

### Building and Testing

```bash
# Run unit tests
make unit-test

# Run e2e tests
make e2e-test

# Build operator binary
make build-operator

# Build Docker image
make docker-build

# Run linting
make go-lint
```

### Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md) for development workflow, coding standards, and pull request guidelines.

## Security Considerations

1. **RBAC:** Operator requires cluster-wide permissions to watch and manage resources
2. **Webhook TLS:** Requires valid TLS certificates for webhook communication
3. **Service Accounts:** Spark applications run with dedicated service accounts
4. **Network Policies:** Can be configured for pod-to-pod communication
5. **Image Security:** Supports private registries and image pull secrets

## Performance and Scalability

- **Controller Concurrency:** Configurable number of concurrent reconciliations
- **Namespace Scoping:** Can be limited to specific namespaces
- **Resource Management:** Implements efficient caching and watch mechanisms
- **Metrics:** Low-overhead Prometheus metrics collection

## Future Enhancements

- Enhanced autoscaling support
- Multi-cluster deployment capabilities
- Advanced scheduling policies
- Improved observability and debugging tools

## References

- [Kubeflow Spark Operator Documentation](https://www.kubeflow.org/docs/components/spark-operator/)
- [API Documentation](docs/api-docs.md)
- [User Guide](https://www.kubeflow.org/docs/components/spark-operator/user-guide/)
- [Developer Guide](https://www.kubeflow.org/docs/components/spark-operator/developer-guide/)

## License

Apache License 2.0 - See [LICENSE](LICENSE) for details.
