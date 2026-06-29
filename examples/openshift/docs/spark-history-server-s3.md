# Spark History Server with AWS S3 on OpenShift

Complete guide to setting up Spark History Server with AWS S3 storage on OpenShift. This allows you to view Spark UI for completed jobs long after the driver pods have terminated.

## Table of Contents
- [What is Spark History Server](#what-is-spark-history-server)
- [Why AWS S3](#why-aws-s3)
- [Prerequisites](#prerequisites)
- [Architecture Overview](#architecture-overview)
- [Step-by-Step Setup](#step-by-step-setup)
  - **Phase 1: Prerequisites Setup**
    - [Step 1: Build Custom Spark Image](#step-1-build-custom-spark-image-with-s3-support)
  - **Phase 2: Storage Setup**
    - [Step 2: Create S3 Bucket](#step-2-create-s3-bucket)
  - **Phase 3: Application Setup**
    - [Step 3: Create AWS Credentials Secret](#step-3-create-aws-credentials-secret)
    - [Step 4: Configure SparkApplication](#step-4-configure-sparkapplication-for-event-logging)
  - **Phase 4: History Server Setup**
    - [Step 5: Deploy History Server](#step-5-deploy-spark-history-server)
    - [Step 6: Verify and Access](#step-6-verify-and-access)
- [Understanding Event Logs](#understanding-event-logs)
- [Troubleshooting](#troubleshooting)

---

## What is Spark History Server

If you've used Spark UI before, you know it's available on the driver pod (port 4040) while your job is running. **Once the job completes and the driver pod terminates, that UI disappears.**

**Spark History Server** solves this problem:
- Spark jobs write **event logs** (detailed execution data) to persistent storage
- History Server **reads** these event logs
- History Server **reconstructs** the familiar Spark UI
- You can browse completed jobs anytime, even weeks later

**Quick Comparison:**

| | Spark UI (Live) | History Server |
|---|---|---|
| **When available?** | Only while job runs | After job completes |
| **Where?** | Driver pod :4040 | Separate deployment :18080 |
| **What happens when driver pod dies?** | UI disappears | UI persists |
| **Use case** | Monitor running jobs | Analyze completed jobs |

---

## Why AWS S3

For this setup, we use **AWS S3** - a managed object storage service.

**Advantages:**
- ✅ **Managed service** - No infrastructure to maintain
- ✅ **Highly available** - Built-in redundancy and durability
- ✅ **Scalable** - Automatically handles any amount of event logs
- ✅ **Simple configuration** - No custom endpoints or SSL settings needed

---

## Prerequisites

Before starting, ensure you have:

### 1. OpenShift Cluster Access
```bash
oc whoami
# Should show your username
```

### 2. Spark Operator Installed
```bash
oc get pods -n spark-operator
# Should show spark-operator-controller and spark-operator-webhook pods
```

### 3. Tools Installed Locally
- `oc` CLI
- `podman` or `docker` (for building custom image)
- Access to push to a container registry (e.g., quay.io)

### 4. AWS Account and Credentials
- AWS account with S3 access
- IAM user credentials (Access Key ID and Secret Access Key)
- Permissions to create S3 buckets

---

## Architecture Overview

```
┌─────────────────────────────────────────────────────────┐
│ 1. SparkApplication Runs                                │
│    • Executes your Spark job                            │
│    • Writes event logs to AWS S3 via S3 API            │
└────────────────┬────────────────────────────────────────┘
                 │
                 │ S3 API (s3a://)
                 ▼
┌─────────────────────────────────────────────────────────┐
│ 2. AWS S3 (Managed Object Storage)                      │
│    Bucket: s3bucket-sparkhistoryserver                   │
│    Region: us-east-2                                     │
│    • Stores event logs in /spark-event-logs/           │
│    • Managed by AWS (high availability, durability)     │
└────────────────┬────────────────────────────────────────┘
                 │
                 │ Reads event logs
                 ▼
┌─────────────────────────────────────────────────────────┐
│ 3. Spark History Server                                 │
│    Namespace: spark-test (same as SparkApplications)    │
│    • Reads logs from S3 via S3 API                     │
│    • Reconstructs Spark UI for each job                │
│    • Serves UI on port 18080                           │
│    • Accessible via HTTPS Route                        │
└─────────────────────────────────────────────────────────┘
```

**Flow:**
1. Spark job writes event logs → AWS S3 (via S3A protocol)
2. History Server reads event logs ← AWS S3 (via S3A protocol)
3. You access History Server UI → Browse completed jobs

---

## Step-by-Step Setup

The setup is organized into four main phases:

1. **[Prerequisites Setup](#phase-1-prerequisites-setup)** - Build custom Spark image with S3 support
2. **[Storage Setup](#phase-2-storage-setup)** - Create S3 bucket
3. **[Application Setup](#phase-3-application-setup)** - Configure Spark jobs to write event logs
4. **[History Server Setup](#phase-4-history-server-setup)** - Deploy and access History Server

---

## Phase 1: Prerequisites Setup

Build a custom Spark image with S3A libraries.

### Step 1: Build Custom Spark Image with S3 Support

The base Spark image doesn't include S3A libraries. We need to build a custom image with Hadoop AWS dependencies.

**1.1 Create Dockerfile**

Save as `Dockerfile.spark-s3`:

```dockerfile
# Dockerfile for Spark with S3/MinIO support
# Based on official Spark image with added S3A dependencies
FROM apache/spark:4.0.1

LABEL maintainer="your-name"
LABEL version="4.0.1-s3"
LABEL description="Spark 4.0.1 with S3/MinIO support (hadoop-aws + aws-sdk)"

# Set the working directory
WORKDIR /opt/spark

# --- Add S3/MinIO Dependencies ---
# Download Hadoop AWS and AWS SDK JARs compatible with Spark 4.0.1
USER root

# Hadoop AWS libraries for S3A FileSystem support
# Use Hadoop 3.4.0 (already in base) with AWS SDK v2 2.25+ (has crossRegionAccessEnabled method)
RUN cd /opt/spark/jars && \
    curl -sL https://repo1.maven.org/maven2/org/apache/hadoop/hadoop-aws/3.4.0/hadoop-aws-3.4.0.jar -o hadoop-aws-3.4.0.jar && \
    curl -sL https://repo1.maven.org/maven2/software/amazon/awssdk/bundle/2.25.16/bundle-2.25.16.jar -o aws-sdk-java-bundle-2.25.16.jar && \
    curl -sL https://repo1.maven.org/maven2/software/amazon/awssdk/url-connection-client/2.25.16/url-connection-client-2.25.16.jar -o aws-url-connection-client-2.25.16.jar && \
    chgrp 0 hadoop-aws-3.4.0.jar aws-sdk-java-bundle-2.25.16.jar aws-url-connection-client-2.25.16.jar && \
    chmod 664 hadoop-aws-3.4.0.jar aws-sdk-java-bundle-2.25.16.jar aws-url-connection-client-2.25.16.jar

# --- OpenShift Arbitrary UID Compatibility ---
# OpenShift assigns arbitrary non-root UID at runtime, but it's always a member of GID 0
# All directories must be owned by group 0 and group-writable (g=u)

# Set Spark directories to be owned by group 0 and group-writable
# Required for: reading jars, writing to work-dir/logs
RUN chgrp -R 0 /opt/spark && \
    chmod -R g=u /opt/spark && \
    mkdir -p /opt/spark/work-dir /opt/spark/logs && \
    chgrp -R 0 /opt/spark/work-dir /opt/spark/logs && \
    chmod -R 775 /opt/spark/work-dir /opt/spark/logs

# Ensure /tmp is writable
RUN chmod 1777 /tmp

# Set HOME for Spark temp files
ENV HOME=/home/spark

# Create HOME directory with proper permissions
RUN mkdir -p /home/spark && \
    chgrp -R 0 /home/spark && \
    chmod -R g=u /home/spark && \
    chmod -R 775 /home/spark

# Verify JARs are present (optional - for debugging)
RUN ls -lh /opt/spark/jars/hadoop-aws* /opt/spark/jars/aws-* || echo "S3 JARs not found!"

# DO NOT set USER directive - OpenShift will assign arbitrary UID at runtime
```

**Why these JARs?**
- `hadoop-aws-3.4.0.jar` - S3A FileSystem implementation
- `aws-sdk-java-bundle-2.25.16.jar` - AWS SDK v2 (required by Hadoop 3.4.x)
- `aws-url-connection-client-2.25.16.jar` - HTTP client for AWS SDK

**1.2 Build and Push Image**

```bash
# Build the image
podman build -f Dockerfile.spark-s3 -t quay.io/YOUR_USERNAME/spark-s3:4.0.1 .

# Login to registry
podman login quay.io

# Push the image
podman push quay.io/YOUR_USERNAME/spark-s3:4.0.1

# Tag as latest
podman tag quay.io/YOUR_USERNAME/spark-s3:4.0.1 quay.io/YOUR_USERNAME/spark-s3:latest
podman push quay.io/YOUR_USERNAME/spark-s3:latest
```

**1.3 Make Image Public (if using quay.io)**

Go to https://quay.io/repository/YOUR_USERNAME/spark-s3 → Settings → Make Public

---

## Phase 2: Storage Setup

Create an S3 bucket for event logs.

### Step 2: Create S3 Bucket

**2.1 Create Bucket via AWS Console**

1. Go to [AWS S3 Console](https://s3.console.aws.amazon.com/s3/)
2. Click **Create bucket**
3. **Bucket name**: `your-spark-event-logs` (must be globally unique)
4. **Region**: Choose your preferred region (e.g., `us-east-2`)
5. Keep default settings (versioning, encryption optional)
6. Click **Create bucket**

**2.2 Create Folder (Optional)**

Navigate into your bucket and create a folder named `spark-event-logs/` to organize event logs.

**2.3 Verify Bucket**

```bash
# Using AWS CLI (if installed)
aws s3 ls s3://your-spark-event-logs/

# Should show empty or the spark-event-logs/ folder
```

---

## Phase 3: Application Setup

Configure Spark jobs to write event logs to S3.

### Step 3: Create AWS Credentials Secret

Spark jobs need AWS credentials to write to S3.

**3.1 Create Secret**

```bash
# Replace 'spark-test' with your SparkApplication namespace
oc create namespace spark-test  # If it doesn't exist

cat <<EOF | oc apply -f -
apiVersion: v1
kind: Secret
metadata:
  name: spark-s3-credentials
  namespace: spark-test
type: Opaque
stringData:
  AWS_ACCESS_KEY_ID: YOUR_ACCESS_KEY_ID
  AWS_SECRET_ACCESS_KEY: YOUR_SECRET_ACCESS_KEY
EOF
```

**3.2 Verify Secret**

```bash
oc get secret spark-s3-credentials -n spark-test
```

---

### Step 4: Configure SparkApplication for Event Logging

Now configure your Spark jobs to write event logs to S3.

**4.1 Create SparkApplication with Event Logging**

Save as `spark-pi-with-eventlog.yaml`:

```yaml
apiVersion: sparkoperator.k8s.io/v1beta2
kind: SparkApplication
metadata:
  name: spark-pi-eventlog
  namespace: spark-test
spec:
  type: Scala
  mode: cluster
  image: quay.io/YOUR_USERNAME/spark-s3:4.0.1
  imagePullPolicy: Always
  mainClass: org.apache.spark.examples.SparkPi
  mainApplicationFile: local:///opt/spark/examples/jars/spark-examples.jar
  arguments:
    - "1000"
  sparkVersion: "4.0.1"
  
  restartPolicy:
    type: Never
  
  sparkConf:
    # Enable event logging
    "spark.eventLog.enabled": "true"
    "spark.eventLog.dir": "s3a://your-spark-event-logs/spark-event-logs/"
    "spark.eventLog.compress": "true"
    
    # AWS S3 configuration
    "spark.hadoop.fs.s3a.impl": "org.apache.hadoop.fs.s3a.S3AFileSystem"
    "spark.hadoop.fs.s3a.aws.credentials.provider": "org.apache.hadoop.fs.s3a.SimpleAWSCredentialsProvider"
  
  driver:
    cores: 1
    memory: "1000m"
    serviceAccount: spark-operator-spark
    env:
    - name: AWS_ACCESS_KEY_ID
      valueFrom:
        secretKeyRef:
          name: spark-s3-credentials
          key: AWS_ACCESS_KEY_ID
    - name: AWS_SECRET_ACCESS_KEY
      valueFrom:
        secretKeyRef:
          name: spark-s3-credentials
          key: AWS_SECRET_ACCESS_KEY
    - name: AWS_REGION
      value: us-east-2
  
  executor:
    cores: 1
    instances: 2
    memory: "1000m"
    env:
    - name: AWS_ACCESS_KEY_ID
      valueFrom:
        secretKeyRef:
          name: spark-s3-credentials
          key: AWS_ACCESS_KEY_ID
    - name: AWS_SECRET_ACCESS_KEY
      valueFrom:
        secretKeyRef:
          name: spark-s3-credentials
          key: AWS_SECRET_ACCESS_KEY
    - name: AWS_REGION
      value: us-east-2
```

**Key sparkConf settings:**
- `spark.eventLog.enabled: "true"` - Enable event logging
- `spark.eventLog.dir: "s3a://your-bucket/path/"` - S3 URI for event logs  
- `spark.hadoop.fs.s3a.impl` - S3A FileSystem implementation

**Note:** AWS S3 uses HTTPS by default and doesn't require endpoint configuration. Credentials are provided via environment variables.

**4.2 Submit the Job**

```bash
# Update bucket name and image reference in the YAML first
oc apply -f spark-pi-with-eventlog.yaml
```

**4.3 Verify Job Completes**

```bash
# Watch job status
oc get sparkapplication spark-pi-eventlog -n spark-test -w

# Check when COMPLETED
oc get sparkapplication spark-pi-eventlog -n spark-test -o jsonpath='{.status.applicationState.state}'
```

**4.4 Verify Event Logs Were Written**

```bash
# Check S3 bucket
aws s3 ls s3://your-spark-event-logs/spark-event-logs/
```

**Expected output:**
```
PRE eventlog_v2_spark-abc123.../
```

The event log directory contains your job's execution history!

---

## Phase 4: History Server Setup

Deploy the History Server to read event logs from S3.

### Step 5: Deploy Spark History Server

**5.1 Create History Server Deployment**

Save as `spark-history-server.yaml`:

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: spark-history-server
  namespace: spark-test
  labels:
    app: spark-history-server
spec:
  selector:
    matchLabels:
      app: spark-history-server
  template:
    metadata:
      labels:
        app: spark-history-server
    spec:
      serviceAccountName: spark-operator-spark
      containers:
      - name: spark-history-server
        image: quay.io/YOUR_USERNAME/spark-s3:4.0.1
        imagePullPolicy: Always
        command: ["/opt/spark/sbin/start-history-server.sh"]
        env:
        - name: AWS_ACCESS_KEY_ID
          valueFrom:
            secretKeyRef:
              name: spark-s3-credentials
              key: AWS_ACCESS_KEY_ID
        - name: AWS_SECRET_ACCESS_KEY
          valueFrom:
            secretKeyRef:
              name: spark-s3-credentials
              key: AWS_SECRET_ACCESS_KEY
        - name: AWS_REGION
          value: us-east-2
        - name: SPARK_NO_DAEMONIZE
          value: "true"
        - name: SPARK_HISTORY_OPTS
          value: >-
            -Dspark.history.fs.logDirectory=s3a://your-spark-event-logs/spark-event-logs/
            -Dspark.hadoop.fs.s3a.impl=org.apache.hadoop.fs.s3a.S3AFileSystem
            -Dspark.hadoop.fs.s3a.aws.credentials.provider=org.apache.hadoop.fs.s3a.SimpleAWSCredentialsProvider
---
apiVersion: v1
kind: Service
metadata:
  name: spark-history-server
  namespace: spark-test
spec:
  selector:
    app: spark-history-server
  ports:
  - port: 18080
    targetPort: 18080
---
apiVersion: route.openshift.io/v1
kind: Route
metadata:
  name: spark-history-server
  namespace: spark-test
  labels:
    app: spark-history-server
spec:
  port:
    targetPort: 18080
  to:
    kind: Service
    name: spark-history-server
  tls:
    termination: edge
    insecureEdgeTerminationPolicy: Redirect
```

**5.2 Deploy History Server**

```bash
# Update bucket name and image reference in YAML first
oc apply -f spark-history-server.yaml
```

**5.3 Verify History Server is Running**

```bash
# Check pod status
oc get pods -n spark-test -l app=spark-history-server

# Should show:
# NAME                                    READY   STATUS    RESTARTS   AGE
# spark-history-server-xxxxxxxxxx-xxxxx   1/1     Running   0          20s

# Check logs
oc logs -n spark-test -l app=spark-history-server --tail=20
```

**Expected log output:**
```
Listing status of s3a://your-spark-event-logs/spark-event-logs/
Replaying log path: s3a://your-spark-event-logs/spark-event-logs/eventlog_v2_spark-...
Bound HistoryServer to 0.0.0.0, and started at http://...:18080
Started HistoryServer
```

---

### Step 6: Verify and Access

**6.1 Get History Server URL**

```bash
oc get route spark-history-server -n spark-test -o jsonpath='https://{.spec.host}'
```

**6.2 Open in Browser**

Copy the URL and open it in your browser. You should see:

- **Main page**: List of completed applications
- **Click App ID**: Full Spark UI with Jobs, Stages, Storage, Environment, Executors tabs
- **Event Timeline**: Visual representation of job execution

**6.3 Verify Your Job Appears**

You should see your `spark-pi-eventlog` application with:
- Application ID
- Application Name
- Duration
- User (service account)
- Last Updated timestamp

Click on the App ID to explore the full Spark UI!

---

## Understanding Event Logs

### What's in an Event Log?

Event logs contain **everything** that happened during your Spark job:
- Job submissions and completions
- Stage details (tasks, shuffle data, metrics)
- Executor additions and removals
- Task attempts and failures
- RDD/DataFrame caching
- SQL query plans (if using Spark SQL)

### Event Log Format

**While running:**
```
eventlog_v2_spark-<app-id>.inprogress
```

**After completion:**
```
eventlog_v2_spark-<app-id>/
├── events_1_<hash>
├── events_2_<hash>
└── appstatus_<hash>
```

Spark 4.x uses event log v2 format with rolling files for large jobs.

### Event Log Compression

With `spark.eventLog.compress: "true"`, logs are compressed using LZ4 codec by default, saving significant storage space.

---

## Troubleshooting

### Spark Job Fails with S3A Errors
Using the custom image from Step 1? Check: `oc get sparkapplication spark-pi-eventlog -n spark-test -o yaml | grep image`

### Image Build Missing JARs
Verify JARs exist: `podman run --rm quay.io/YOUR_USERNAME/spark-s3:4.0.1 ls /opt/spark/jars/hadoop-aws*`

### AWS Access Denied
Check credentials are correct: `oc get secret spark-s3-credentials -n spark-test -o yaml`
Verify IAM user has S3 permissions: `s3:PutObject`, `s3:GetObject`, `s3:ListBucket`

### History Server Shows No Applications
Check event logs exist in S3: `aws s3 ls s3://your-spark-event-logs/spark-event-logs/`
Check History Server logs: `oc logs -n spark-test -l app=spark-history-server | grep Replaying`

### Wrong AWS Region
Verify `AWS_REGION` matches your bucket region in SparkApplication and History Server YAMLs

### Route Returns 503
Check History Server pod running: `oc get pods -n spark-test -l app=spark-history-server`

---

## Next Steps

Now that you have Spark History Server working with AWS S3:

1. **Run more Spark jobs** - All jobs with event logging enabled will appear in History Server
2. **Explore the UI** - Check Jobs, Stages, Executors tabs for performance insights
3. **Set up lifecycle policies** - Configure S3 bucket lifecycle rules to auto-delete old logs
4. **Enable versioning** - Turn on S3 versioning for additional data protection

---

**Tested With:** Spark 4.0.1, AWS S3 (us-east-2), OpenShift 4.x
