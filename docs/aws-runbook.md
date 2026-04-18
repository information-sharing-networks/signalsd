# AWS Deployment Runbook

Operational reference for the AWS App Runner deployment of signalsd. For initial
infrastructure setup, see [aws-setup.md](aws-setup.md).

---

## Deployment Overview

### How a Production Deployment Works

Triggered by pushing a `v*` tag (e.g. `git tag v1.2.3 && git push --tags`).

1. GitHub Actions checks out the code and sets up Go
2. Authenticates to AWS via OIDC — the workflow requests a short-lived token from GitHub's
   OIDC provider, which AWS exchanges for temporary AWS credentials by assuming the
   `github-actions-signalsd` IAM role. No credentials are stored in GitHub Secrets.
3. Logs in to ECR and builds a `linux/amd64` Docker image, pushing two tags:
   - `:<git-sha>` — immutable reference to this exact deployment
   - `:latest` — floating tag, updated on each prod deploy
4. Runs database migrations (`goose up`) against the production database
5. Calls `aws apprunner update-service` with the new image SHA tag and the full set of
   environment variables
6. Polls `aws apprunner describe-service` until status returns `RUNNING` or a failure status

The workflow can also be triggered manually via `workflow_dispatch` in the GitHub Actions UI.

### How a Staging Deployment Works

Triggered automatically when the CI workflow completes successfully on the `main` branch
(i.e. after every merged PR). Also triggerable manually.

The process is identical to production except:
- Image tags use `staging-<sha>` and `staging-latest`
- Uses staging secrets and variables
- Smaller instance sizing (0.5 vCPU / 1 GB vs 1 vCPU / 2 GB)

---

## Deployment Behaviour and Graceful Handoff

### What App Runner does during an update

App Runner uses a rolling deployment strategy — equivalent to Cloud Run's revision-based
traffic shifting:

1. New instances are started with the new image
2. App Runner waits for new instances to pass the HTTP health check (`GET /health/ready`)
3. Once healthy, traffic is shifted to new instances
4. Old instances receive `SIGTERM` and are given 120 seconds to drain before `SIGKILL`

### What the application does on SIGTERM

The signalsd binary handles `SIGTERM` gracefully:

- `signal.NotifyContext` converts `SIGTERM` to context cancellation
- The HTTP server stops accepting new connections
- `http.Server.Shutdown()` waits for in-flight requests to complete (10-second timeout)
- The database connection pool is closed
- The process exits cleanly

The 10-second shutdown timeout is well within App Runner's 120-second grace period, so
in-flight requests complete before the instance terminates.

### Health check configuration

The App Runner service is configured to use the application's readiness endpoint:

- **Protocol**: HTTP
- **Path**: `/health/ready`
- **Interval**: 10 seconds
- **Timeout**: 5 seconds
- **Healthy threshold**: 1 consecutive success
- **Unhealthy threshold**: 3 consecutive failures

`/health/ready` returns `200 OK` only when the database connection is active. This ensures
traffic is not routed to a new instance until it is fully initialised.

`/health/live` (always `200 OK`) is not used for App Runner health checks — it is available
for external monitoring tools.

### Key difference from Cloud Run

Cloud Run's `gcloud run deploy` is synchronous — it blocks until the new revision is serving
traffic or fails. App Runner's `update-service` returns immediately (the deployment continues
in the background), which is why the workflow includes a polling loop. The end result is
equivalent.

**There is no automatic rollback.** If new instances fail health checks, App Runner retries
but does not revert to the previous image. See the rollback procedure below.

---

## Rollback Procedure

Every deployment pushes an image tagged with `:<git-sha>`. These are retained in ECR and
can be redeployed at any time.

> **Check migrations first.** The CD workflow runs `goose up` before deploying the new image.
> If the deployment you are rolling back included a migration, verify the older code is
> compatible with the current database schema. Goose migrations are in `app/sql/schema/` —
> check whether any were added between the target SHA and HEAD.

### Identify the target SHA

```bash
# List recent images in ECR (most recent first)
aws ecr describe-images \
  --repository-name signalsd \
  --region eu-west-1 \
  --query 'sort_by(imageDetails, &imagePushedAt)[-10:].imageTags' \
  --output table
```

Or find the SHA from git:
```bash
git log --oneline -10
```

### Redeploy a specific SHA (production)

```bash
ACCOUNT_ID=$(aws sts get-caller-identity --query Account --output text)
REGION=eu-west-1
SHA=<target-git-sha>
SERVICE_ARN=<prod-service-arn>
APPRUNNER_ROLE_ARN=$(aws iam get-role \
  --role-name apprunner-ecr-access \
  --query 'Role.Arn' --output text)

aws apprunner update-service \
  --service-arn "$SERVICE_ARN" \
  --source-configuration "{
    \"ImageRepository\": {
      \"ImageIdentifier\": \"${ACCOUNT_ID}.dkr.ecr.${REGION}.amazonaws.com/signalsd:${SHA}\",
      \"ImageConfiguration\": {
        \"Port\": \"8080\"
      },
      \"ImageRepositoryType\": \"ECR\"
    },
    \"AuthenticationConfiguration\": {
      \"AccessRoleArn\": \"${APPRUNNER_ROLE_ARN}\"
    },
    \"AutoDeploymentsEnabled\": false
  }" \
  --region "$REGION"
```

Poll for completion (the `watch` command is Linux-only; use the `while` loop on macOS):

```bash
# Linux
watch -n 10 "aws apprunner describe-service \
  --service-arn \"$SERVICE_ARN\" \
  --region $REGION \
  --query 'Service.Status' --output text"

# macOS
while true; do
  aws apprunner describe-service \
    --service-arn "$SERVICE_ARN" \
    --region $REGION \
    --query 'Service.Status' --output text
  sleep 10
done
```

For staging, use `staging-<sha>` as the image tag and the staging service ARN.

---

## Monitoring a Deployment

Commands below use `<prod-service-arn>` / `<service-arn>` as placeholders. Look up your
service ARNs with:

```bash
aws apprunner list-services --region eu-west-1 \
  --query 'ServiceSummaryList[*].{Name:ServiceName,ARN:ServiceArn,Status:Status}' \
  --output table
```

### Check current service status

```bash
# Production
aws apprunner describe-service \
  --service-arn "<prod-service-arn>" \
  --region eu-west-1 \
  --query 'Service.{Status:Status,URL:ServiceUrl,Updated:UpdatedAt}' \
  --output table
```

Possible status values:

| Status | Meaning |
|---|---|
| `RUNNING` | Service is live and healthy |
| `OPERATION_IN_PROGRESS` | Deployment or update in progress |
| `CREATE_FAILED` | Initial creation failed |
| `UPDATE_FAILED` | Most recent update failed |
| `PAUSED` | Service manually paused |
| `DELETED` | Service has been deleted |

### View deployment logs

App Runner sends application logs to CloudWatch Logs automatically. The log group name is
derived from the service ARN — the service ID is the final segment of the ARN
(`arn:aws:apprunner:region:account:service/<name>/<id>`):

```bash
# Extract the service ID from the ARN
SERVICE_ARN=<your-service-arn>
SERVICE_ID=$(echo $SERVICE_ARN | cut -d'/' -f3)

# Stream application logs
aws logs tail \
  --follow \
  --region eu-west-1 \
  "/aws/apprunner/signalsd/${SERVICE_ID}/application"
```

### View recent operations

Each deployment, pause, or resume creates an operation record:

```bash
aws apprunner list-operations \
  --service-arn "<service-arn>" \
  --region eu-west-1 \
  --query 'OperationSummaryList[*].{Type:Type,Status:Status,Started:StartedAt,Ended:EndedAt}' \
  --output table
```

---

## Service Management

### Pause a service (stops compute billing, retains configuration)

App Runner services can be paused when not needed — useful for staging outside working hours.
ECR storage continues to be billed while paused, but the compute cost stops. The service URL
remains reserved but returns an error page while paused.

There is no equivalent to this in Cloud Run (which handles cost by scaling to zero).

```bash
aws apprunner pause-service \
  --service-arn "<service-arn>" \
  --region eu-west-1
```

### Resume a paused service

```bash
aws apprunner resume-service \
  --service-arn "<service-arn>" \
  --region eu-west-1
```

After resuming, wait for status `RUNNING` before expecting traffic to be served.

---

## Environment Variable Updates

Environment variables are rebuilt and set on the service as part of every deployment. To
change a variable:

1. Update the GitHub secret or variable in the repository settings
2. Trigger a deployment (push a new tag for prod, merge to main for staging, or use
   `workflow_dispatch`)

Unlike Cloud Run (where `gcloud run services update --set-env-vars` can change variables
without redeploying the image), App Runner's env vars are part of the `SourceConfiguration`
and are always set alongside the image reference. This means every config change goes through
the CD workflow — intentional, keeping config and code changes in the same audit trail.

---

## Comparing AWS and GCP Environments

| Characteristic | GCP (Cloud Run) | AWS (App Runner) |
|---|---|---|
| Min instances | 0 (scales to zero) | 1 (always running) |
| Prod sizing | 1 CPU / 512 MB | 1 vCPU / 2 GB |
| Staging sizing | 0.5 CPU / 256 MB | 0.5 vCPU / 1 GB |
| Deployment trigger | `gcloud run deploy` (synchronous) | `update-service` + poll (async) |
| Container args at deploy | `--args="run,all"` overrides CMD | Not supported — requires `CMD` in Dockerfile |
| Env var passing | `--set-env-vars KEY=VAL` (CLI flags) | JSON map in `SourceConfiguration` document |
| Env var update without redeploy | Yes (`gcloud run services update`) | No — env vars are bundled with image config |
| Auto rollback on failure | No | No |
| Container registry | GCP Artifact Registry | Amazon ECR |
| Auth (GitHub → cloud) | Service account JSON secret | OIDC (no stored credentials) |
| Health check default | TCP | TCP — **overridden to HTTP `/health/ready`** |
| Graceful drain timeout | 300 seconds | 120 seconds |
| App shutdown timeout | 10 seconds | 10 seconds |
| Cost when idle | Free (scaled to zero) | Billed at min-instance rate (or pause manually) |

---

## Troubleshooting

### Deployment stuck in `OPERATION_IN_PROGRESS`

App Runner deployments typically complete within 3–5 minutes. If taking longer, check logs
and recent operations:

```bash
aws apprunner list-operations \
  --service-arn "<service-arn>" \
  --region eu-west-1
```

The most common cause is health check failures — new instances are started but never pass
`/health/ready`, so traffic never shifts and the deployment times out with `UPDATE_FAILED`.
Check:
- The image started successfully (no crash on startup — check CloudWatch logs)
- The database URL is correct and the database is reachable from App Runner
- `/health/ready` returns `200` (indicates database connectivity is working)

### `UPDATE_FAILED` status

Retrieve the reason:

```bash
aws apprunner describe-service \
  --service-arn "<service-arn>" \
  --region eu-west-1 \
  --query 'Service.{Status:Status,Reason:StateChangeReason}'
```

Then roll back to the last known good SHA (see rollback procedure above).

### Workflow fails at `aws apprunner update-service`

Check that:
- `AWS_PROD_APP_RUNNER_ARN` / `AWS_STAGING_APP_RUNNER_ARN` secrets contain the correct ARNs
- The `github-actions-signalsd` IAM role has `apprunner:UpdateService` and
  `apprunner:DescribeService` on the correct resource ARNs (see [aws-setup.md](aws-setup.md)
  Step 3)
- The ECR image push in the previous step completed successfully

### Workflow fails at ECR push

Check that:
- `AWS_ACCOUNT_ID` and `AWS_REGION` variables are set correctly in GitHub
- The `github-actions-signalsd` IAM role has the ECR push permissions (Step 3 of setup)
- The ECR repository `signalsd` exists in the correct region:
  ```bash
  aws ecr describe-repositories --region eu-west-1
  ```
