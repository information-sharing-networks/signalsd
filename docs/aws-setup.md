# AWS Deployment Setup

One-time infrastructure setup for the AWS deployment pipeline. Run these steps before the
`cd-production-aws` and `cd-staging-aws` workflows can execute.

The GCP deployment continues to operate independently. These steps create parallel AWS
infrastructure and do not affect the existing GCP setup.

---

## AWS Concepts for GCP Users

A few AWS-specific patterns appear throughout these steps:

**ARNs (Amazon Resource Names)** are the unique identifier for any AWS resource — the
equivalent of a GCP fully-qualified resource name. Format:
`arn:aws:<service>:<region>:<account-id>:<resource-type>/<resource-name>`

Examples:
- `arn:aws:iam::123456789012:role/my-role` — IAM role (global, no region)
- `arn:aws:ecr:eu-west-1:123456789012:repository/signalsd` — ECR repository (regional)
- `arn:aws:apprunner:eu-west-1:123456789012:service/signalsd/abc123` — App Runner service

You will frequently be asked to "note the ARN from the output" — it is the primary way to
reference resources in subsequent commands.

**IAM Roles** are the AWS equivalent of GCP Service Accounts, but with a key structural
difference. A role has two separate policy documents:

- **Trust policy** — declares *who* or *what* is allowed to assume (use) this role.
  In GCP terms: who has `iam.serviceAccounts.actAs` on this service account.
- **Permissions policy** — declares *what actions* the role can perform once assumed.
  In GCP terms: what IAM bindings the service account has on which resources.

Both policies must be correct for a role to work.

**`--query` flag** — the AWS CLI uses JMESPath syntax to filter and shape output, similar
to GCP's `--format='value(...)'`. For example, `--query 'Role.Arn' --output text` extracts
just the ARN string from a larger JSON response.

---

## Prerequisites

- AWS CLI authenticated with your `AdministratorAccess` profile (see [aws-cli-setup.md](aws-cli-setup.md))
- Profile exported for the session before running any commands below:

```bash
export AWS_PROFILE=signalsd
aws sts get-caller-identity   # confirm identity before proceeding
```

- Chosen region: `eu-west-1` (Ireland) — adjust throughout if using a different region

---

## Step 1 — ECR Repository

ECR (Elastic Container Registry) is the AWS equivalent of GCP Artifact Registry. A single
repository is used for both environments, distinguished by image tag — the same pattern used
in the GCP setup.

```bash
aws ecr create-repository \
  --repository-name signalsd \
  --image-scanning-configuration scanOnPush=true \
  --region eu-west-1
```

The repository URI in the output will have the form:
`<ACCOUNT_ID>.dkr.ecr.eu-west-1.amazonaws.com/signalsd`

Unlike GCP's registry (which uses a path-based structure), ECR repository URIs embed the
account ID and region directly in the hostname.

---

## Step 2 — GitHub OIDC Provider

Allows GitHub Actions to authenticate to AWS without storing any static credentials — the
equivalent of using Workload Identity Federation on GCP instead of a service account key.
This is a one-time step per AWS account and may already be present in a shared account.

**The provider is safe to share.** Its existence grants no permissions to anyone — access is
only controlled by IAM roles that explicitly reference it with a repo-scoped condition.
If another team in the same account also needs GitHub OIDC, they reuse this same provider
and create their own role scoped to their repository. Only one provider per URL can exist
per account.

Check whether it already exists and create it if not:

```bash
OIDC_ARN=$(aws iam list-open-id-connect-providers \
  --query "OpenIDConnectProviderList[?contains(Arn, 'token.actions.githubusercontent.com')].Arn" \
  --output text)

if [ -n "$OIDC_ARN" ]; then
  echo "Already exists: $OIDC_ARN"
else
  echo "Not found — creating..."
  OIDC_ARN=$(aws iam create-open-id-connect-provider \
    --url https://token.actions.githubusercontent.com \
    --client-id-list sts.amazonaws.com \
    --thumbprint-list 6938fd4d98bab03faadb97b34396831e3780aea1 \
    --query 'OpenIDConnectProviderArn' \
    --output text)
  echo "Created: $OIDC_ARN"
fi
```

Verify the configuration is correct — the output should contain `sts.amazonaws.com` in
`ClientIDList`. This is the audience value that GitHub embeds in its JWT tokens; AWS checks
it matches before accepting the token:

```bash
aws iam get-open-id-connect-provider \
  --open-id-connect-provider-arn "$OIDC_ARN" \
  --query '{ClientIDs:ClientIDList,Thumbprints:ThumbprintList}'
```

If `sts.amazonaws.com` is missing from `ClientIDs`, add it:

```bash
aws iam add-client-id-to-open-id-connect-provider \
  --open-id-connect-provider-arn "$OIDC_ARN" \
  --client-id sts.amazonaws.com
```

---

## Step 3 — GitHub Actions IAM Role

This role is assumed by the GitHub Actions workflows via OIDC. It has two parts (see the
concepts section above): a trust policy scoped to this repository only, and a permissions
policy limited to ECR push and App Runner deploy actions.

The commands below capture your account ID automatically and write the policy files to
`/tmp/` — temporary scratch space that works in both a local terminal and CloudShell:

```bash
ACCOUNT_ID=$(aws sts get-caller-identity --query Account --output text)

# Trust policy: allows GitHub Actions (scoped to this repo) to assume this role
cat > /tmp/trust-policy.json << EOF
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Effect": "Allow",
      "Principal": {
        "Federated": "arn:aws:iam::${ACCOUNT_ID}:oidc-provider/token.actions.githubusercontent.com"
      },
      "Action": "sts:AssumeRoleWithWebIdentity",
      "Condition": {
        "StringEquals": {
          "token.actions.githubusercontent.com:aud": "sts.amazonaws.com"
        },
        "StringLike": {
          "token.actions.githubusercontent.com:sub": "repo:information-sharing-networks/signalsd:*"
        }
      }
    }
  ]
}
EOF

# Permissions policy: what the role can do once assumed
# Sid values are optional labels — treat them as comments
cat > /tmp/deploy-policy.json << EOF
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Sid": "ECRAuth",
      "Effect": "Allow",
      "Action": "ecr:GetAuthorizationToken",
      "Resource": "*"
    },
    {
      "Sid": "ECRPush",
      "Effect": "Allow",
      "Action": [
        "ecr:BatchCheckLayerAvailability",
        "ecr:GetDownloadUrlForLayer",
        "ecr:BatchGetImage",
        "ecr:InitiateLayerUpload",
        "ecr:UploadLayerPart",
        "ecr:CompleteLayerUpload",
        "ecr:PutImage"
      ],
      "Resource": "arn:aws:ecr:eu-west-1:${ACCOUNT_ID}:repository/signalsd"
    },
    {
      "Sid": "AppRunnerDeploy",
      "Effect": "Allow",
      "Action": [
        "apprunner:UpdateService",
        "apprunner:DescribeService"
      ],
      "Resource": [
        "arn:aws:apprunner:eu-west-1:${ACCOUNT_ID}:service/signalsd/*",
        "arn:aws:apprunner:eu-west-1:${ACCOUNT_ID}:service/signalsd-staging/*"
      ]
    }
  ]
}
EOF

# Create the role with the trust policy, then attach the permissions policy
aws iam create-role \
  --role-name github-actions-signalsd \
  --assume-role-policy-document file:///tmp/trust-policy.json

aws iam put-role-policy \
  --role-name github-actions-signalsd \
  --policy-name signalsd-deploy \
  --policy-document file:///tmp/deploy-policy.json
```

Note the role ARN from the `create-role` output — needed for GitHub secret `AWS_ROLE_ARN`.

> **Why `Resource: "*"` on `ECRAuth`?** ECR's `GetAuthorizationToken` is an account-level
> operation that cannot be scoped to a specific repository — this is an AWS API constraint,
> not a permissions shortcut. The actual image push permissions (`ECRPush`) are scoped to the
> `signalsd` repository only.

---

## Step 4 — App Runner ECR Access Role

App Runner needs its own IAM role to pull images from ECR at runtime. This is separate from
the GitHub Actions role because it is assumed by the App Runner service itself (not by the
workflow), and only needs read access to ECR — not push access.

In GCP terms: the Cloud Run service uses a runtime service account to access GCP resources;
this is the AWS equivalent.

```bash
# Trust policy: allows the App Runner build service to assume this role
cat > /tmp/apprunner-trust.json << 'EOF'
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Effect": "Allow",
      "Principal": {
        "Service": "build.apprunner.amazonaws.com"
      },
      "Action": "sts:AssumeRole"
    }
  ]
}
EOF

aws iam create-role \
  --role-name apprunner-ecr-access \
  --assume-role-policy-document file:///tmp/apprunner-trust.json

# Attach AWS's managed policy granting ECR read access
aws iam attach-role-policy \
  --role-name apprunner-ecr-access \
  --policy-arn arn:aws:iam::aws:policy/service-role/AWSAppRunnerServicePolicyForECRAccess
```

Note the role ARN from the `create-role` output — needed in Steps 6 and 7.

---

## Step 5 — Auto-scaling Configurations

App Runner scales the number of running instances based on concurrent requests. `max-concurrency`
sets how many simultaneous requests a single instance handles before App Runner adds another
instance — analogous to Cloud Run's `--concurrency` flag.

Unlike Cloud Run, App Runner has no scale-to-zero — `min-size 1` means at least one instance
is always running. This eliminates cold starts but incurs baseline compute cost even with
zero traffic. See the runbook's "Pause a service" section for stopping billing when idle.

In Cloud Run, scaling parameters are inline flags on the service (`--min-instances`,
`--max-instances`). In App Runner, auto-scaling is a standalone named resource that is
attached to a service by ARN — this allows sharing a configuration across services, but
means it must be created separately before the services that reference it.

> **Shell session note:** The `$PROD_ASG_ARN` and `$STAGING_ASG_ARN` variables captured
> here are used in Steps 6 and 7. Run Steps 5, 6, and 7 in the same terminal session, or
> retrieve the ARNs manually later with:
> `aws apprunner list-auto-scaling-configurations --region eu-west-1`

```bash
# Production: 1–4 instances, up to 100 concurrent requests per instance
PROD_ASG_ARN=$(aws apprunner create-auto-scaling-configuration \
  --auto-scaling-configuration-name signalsd-prod \
  --min-size 1 \
  --max-size 4 \
  --max-concurrency 100 \
  --region eu-west-1 \
  --query 'AutoScalingConfiguration.AutoScalingConfigurationArn' \
  --output text)

echo "Prod ASG ARN: $PROD_ASG_ARN"

# Staging: 1–2 instances
STAGING_ASG_ARN=$(aws apprunner create-auto-scaling-configuration \
  --auto-scaling-configuration-name signalsd-staging \
  --min-size 1 \
  --max-size 2 \
  --max-concurrency 100 \
  --region eu-west-1 \
  --query 'AutoScalingConfiguration.AutoScalingConfigurationArn' \
  --output text)

echo "Staging ASG ARN: $STAGING_ASG_ARN"
```

---

## Steps 6 & 7 — App Runner Services

Unlike `gcloud run deploy` (which creates or updates a service in one idempotent command),
App Runner separates service creation (`create-service`, run once here) from deployment
(`update-service`, run by the CD workflow on every deploy). The CD workflow works exactly
the same as the GCP version — it builds and pushes the image, then calls `update-service`.

`create-service` requires a valid image to already exist in ECR at the time it is called.
The step below pushes the real application image once from your local machine to satisfy
this requirement. After that, the CD workflow owns all future image builds and deployments.

`AutoDeploymentsEnabled: false` is required to prevent App Runner from automatically
deploying whenever a new image is pushed to ECR — without this, it would start deploying
before the migrations step in the CD workflow has run.

### Push the initial image

Run from the repository root on your local machine (requires Docker):

```bash
ACCOUNT_ID=$(aws sts get-caller-identity --query Account --output text)
REGION=eu-west-1

# Log Docker in to ECR
aws ecr get-login-password --region $REGION | \
  docker login --username AWS --password-stdin \
  ${ACCOUNT_ID}.dkr.ecr.${REGION}.amazonaws.com

# Build and push — this is a one-off; the CD workflow handles all subsequent builds
GO_VERSION=$(grep '^go ' app/go.mod | awk '{print $2}')
docker buildx build --platform linux/amd64 \
  --build-arg GO_VERSION=${GO_VERSION} \
  -f app/Dockerfile \
  -t ${ACCOUNT_ID}.dkr.ecr.${REGION}.amazonaws.com/signalsd:latest \
  --push .
```

### Step 6 — Production Service

```bash
ACCOUNT_ID=$(aws sts get-caller-identity --query Account --output text)
APPRUNNER_ROLE_ARN=$(aws iam get-role \
  --role-name apprunner-ecr-access \
  --query 'Role.Arn' --output text)

aws apprunner create-service \
  --service-name signalsd \
  --source-configuration "{
    \"ImageRepository\": {
      \"ImageIdentifier\": \"${ACCOUNT_ID}.dkr.ecr.eu-west-1.amazonaws.com/signalsd:latest\",
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
  --instance-configuration "Cpu=1024,Memory=2048" \
  --health-check-configuration "Protocol=HTTP,Path=/health/ready,Interval=10,Timeout=5,HealthyThreshold=1,UnhealthyThreshold=3" \
  --auto-scaling-configuration-arn "$PROD_ASG_ARN" \
  --region eu-west-1
```

Note the `ServiceArn` from the output — needed for GitHub secret `AWS_PROD_APP_RUNNER_ARN`.
To retrieve it later: `aws apprunner list-services --region eu-west-1`

### Step 7 — Staging Service

```bash
aws apprunner create-service \
  --service-name signalsd-staging \
  --source-configuration "{
    \"ImageRepository\": {
      \"ImageIdentifier\": \"${ACCOUNT_ID}.dkr.ecr.eu-west-1.amazonaws.com/signalsd:latest\",
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
  --instance-configuration "Cpu=512,Memory=1024" \
  --health-check-configuration "Protocol=HTTP,Path=/health/ready,Interval=10,Timeout=5,HealthyThreshold=1,UnhealthyThreshold=3" \
  --auto-scaling-configuration-arn "$STAGING_ASG_ARN" \
  --region eu-west-1
```

Note the `ServiceArn` — needed for GitHub secret `AWS_STAGING_APP_RUNNER_ARN`.

The services will initially fail health checks because no environment variables (DATABASE_URL
etc.) are set yet. This is expected — the first CD workflow deployment will provide them.

Once both services are created and the GitHub secrets/variables from Step 8 are configured,
trigger the staging workflow via `workflow_dispatch` in GitHub Actions to verify the full
deployment pipeline end-to-end.

---

## Step 8 — GitHub Repository Secrets and Variables

### Secrets

| Name | Value |
|---|---|
| `AWS_ROLE_ARN` | IAM role ARN from Step 3 |
| `AWS_DATABASE_URL` | Production database connection string |
| `AWS_STAGING_DATABASE_URL` | Staging database connection string |
| `AWS_SECRET_KEY` | Production application secret key |
| `AWS_STAGING_SECRET_KEY` | Staging application secret key |
| `AWS_PROD_APP_RUNNER_ARN` | App Runner service ARN from Step 6 |
| `AWS_STAGING_APP_RUNNER_ARN` | App Runner service ARN from Step 7 |

### Variables

| Name | Example value | Notes |
|---|---|---|
| `AWS_REGION` | `eu-west-1` | Must match the region used above |
| `AWS_ACCOUNT_ID` | `123456789012` | Used to construct the ECR registry URL |
| `AWS_PUBLIC_BASE_URL` | `https://api.example.com` | Production public URL |
| `AWS_STAGING_PUBLIC_BASE_URL` | `https://staging.api.example.com` | |
| `AWS_ALLOWED_ORIGINS` | `https://app.example.com` | Production CORS origins |
| `AWS_STAGING_ALLOWED_ORIGINS` | `https://staging.app.example.com` | |

The optional tuning variables (`DB_MAX_CONNECTIONS`, `DB_MIN_CONNECTIONS`,
`DB_MAX_CONN_LIFETIME`, `DB_MAX_CONN_IDLE_TIME`, `DB_CONNECT_TIMEOUT`, `RATE_LIMIT_RPS`,
`RATE_LIMIT_BURST`, `MAX_SIGNAL_PAYLOAD_SIZE`, `READ_TIMEOUT`, `WRITE_TIMEOUT`,
`IDLE_TIMEOUT`) are already configured for GCP and are shared — no changes needed.

---

## Step 9 — Dockerfile Change

Cloud Run allows passing container arguments at deploy time via the `--args` flag — this is
how the GCP workflows tell the container to `run all` without needing a `CMD` in the
Dockerfile. App Runner has no equivalent runtime argument override; the container must
declare its own start command via `CMD` in the Dockerfile (or via `StartCommand` in the
service configuration, but `CMD` is simpler and keeps the start command with the code).

Add a default `CMD` to [app/Dockerfile](../app/Dockerfile). The Dockerfile already has
`ENTRYPOINT ["/app/signalsd"]` — add the `CMD` line immediately after it:

```dockerfile
CMD ["run", "all"]
```

The image is built on `scratch` (no shell), so exec form (`["run", "all"]` not `run all`) is
required.

**This does not affect the GCP deployment.** The `ENTRYPOINT` is unchanged — only a `CMD`
default is added. Cloud Run's `--args="run,all"` overrides `CMD` at deploy time, so the
resulting container command is identical for both clouds:

- **GCP**: `ENTRYPOINT` + `--args` override = `/app/signalsd run all`
- **AWS**: `ENTRYPOINT` + `CMD` default = `/app/signalsd run all`

---

## Step 10 — CD Workflow Files

Create two new workflow files alongside the existing GCP workflows:

| File | Trigger | Target | Based on |
|---|---|---|---|
| `cd-staging-aws.yml` | CI success on `main` | AWS App Runner (staging) | `cd-staging.yml` |
| `cd-production-aws.yml` | `v*` tag push | AWS App Runner (prod) | `cd-production.yml` |

The existing GCP workflows continue to operate — both clouds deploy independently from the
same CI pipeline.

Most steps are identical to the GCP workflows. The sections below cover only what changes.

### Triggers and permissions

Staging:

```yaml
name: Deploy to AWS Staging

on:
  workflow_run:
    workflows: ["CI"]
    types: [completed]
    branches: [main]
  workflow_dispatch:

permissions:
  id-token: write    # Required — allows the runner to request an OIDC token
  contents: read
```

Production:

```yaml
name: Deploy to AWS Production

on:
  push:
    tags:
      - 'v*'
  workflow_dispatch:

permissions:
  id-token: write
  contents: read
```

The `permissions` block is the first difference from GCP. `id-token: write` allows the
workflow to request an OIDC token from GitHub, which AWS exchanges for temporary credentials.
The GCP workflows don't need this because they authenticate with a static JSON credential.

For the staging job condition, include both trigger types so that manual dispatch works:

```yaml
if: ${{ github.event_name == 'workflow_dispatch' || github.event.workflow_run.conclusion == 'success' }}
```

### Step-by-step mapping

| Step | GCP → AWS change |
|---|---|
| Check out code | **Identical** — copy as-is |
| Set up Go | **Identical** — copy as-is |
| Download Go modules | **Identical** — copy as-is |
| Authenticate | Replace `google-github-actions/auth` — see below |
| Configure Docker | Replace `gcloud auth configure-docker` — see below |
| Build and push image | Change registry URL in `-t` tags only |
| Run migrations | **Identical** — change secret name only |
| Deploy | Replace `gcloud run deploy` — see below |

### AWS authentication

Replaces the `google-github-actions/auth@v3` and `setup-gcloud@v3` steps:

```yaml
- name: Configure AWS credentials
  uses: aws-actions/configure-aws-credentials@v4
  with:
    role-to-assume: ${{ secrets.AWS_ROLE_ARN }}
    aws-region: ${{ vars.AWS_REGION }}
```

This assumes the `github-actions-signalsd` IAM role from Step 3 via OIDC. No credentials
are stored — `AWS_ROLE_ARN` is just the role's ARN string, not a key.

### ECR login

Replaces `gcloud auth configure-docker`. Where GCloud installs a persistent credential
helper that Docker calls transparently, ECR uses short-lived auth tokens (valid 12 hours)
that must be fetched explicitly before each push:

```yaml
- name: Log in to Amazon ECR
  run: |
    aws ecr get-login-password --region ${{ vars.AWS_REGION }} | \
      docker login --username AWS --password-stdin \
      ${{ vars.AWS_ACCOUNT_ID }}.dkr.ecr.${{ vars.AWS_REGION }}.amazonaws.com
```

### Image tags

Same `docker buildx build` command — only the `-t` tag lines change. Replace the GCP
Artifact Registry URL with the ECR URL:

```yaml
# Staging tags
-t ${{ vars.AWS_ACCOUNT_ID }}.dkr.ecr.${{ vars.AWS_REGION }}.amazonaws.com/signalsd:staging-${{ github.sha }} \
-t ${{ vars.AWS_ACCOUNT_ID }}.dkr.ecr.${{ vars.AWS_REGION }}.amazonaws.com/signalsd:staging-latest \

# Production tags (no staging- prefix)
-t ${{ vars.AWS_ACCOUNT_ID }}.dkr.ecr.${{ vars.AWS_REGION }}.amazonaws.com/signalsd:${{ github.sha }} \
-t ${{ vars.AWS_ACCOUNT_ID }}.dkr.ecr.${{ vars.AWS_REGION }}.amazonaws.com/signalsd:latest \
```

### Migrations

Same goose command, different secret name:

```yaml
# Staging
run: go tool goose -dir sql/schema postgres "${{ secrets.AWS_STAGING_DATABASE_URL }}" up

# Production
run: go tool goose -dir sql/schema postgres "${{ secrets.AWS_DATABASE_URL }}" up
```

### Deploy to App Runner

This step has two structural differences from `gcloud run deploy`:

1. **Async, not sync.** Cloud Run blocks until the new revision is serving traffic. App
   Runner's `update-service` returns immediately — the deployment proceeds in the background,
   so the workflow must poll `describe-service` until status reaches `RUNNING` or a failure
   state. Without the polling loop, the workflow would report success before the deployment
   has actually completed.

2. **JSON config, not CLI flags.** Cloud Run passes environment variables as a flat
   comma-separated string (`--set-env-vars KEY=VAL,KEY2=VAL2`). App Runner bundles the
   image reference, port, environment variables, and ECR access role into a single
   `SourceConfiguration` JSON document. The workflow writes this to a temp file and passes
   it via `file:///tmp/source-config.json` to avoid shell-escaping issues with inline JSON.

The example below shows the staging workflow. For production, swap the secrets and variables
per the table at the end of this section.

```yaml
- name: Deploy to App Runner
  run: |
    REGISTRY="${{ vars.AWS_ACCOUNT_ID }}.dkr.ecr.${{ vars.AWS_REGION }}.amazonaws.com"
    ROLE_ARN=$(aws iam get-role --role-name apprunner-ecr-access \
      --query 'Role.Arn' --output text)

    # Write source configuration to a file — the file:// prefix tells the
    # AWS CLI to read from disk, avoiding shell escaping issues with inline JSON.
    cat > /tmp/source-config.json << EOF
    {
      "ImageRepository": {
        "ImageIdentifier": "${REGISTRY}/signalsd:staging-${{ github.sha }}",
        "ImageConfiguration": {
          "Port": "8080",
          "RuntimeEnvironmentVariables": {
            "DATABASE_URL": "${{ secrets.AWS_STAGING_DATABASE_URL }}",
            "SECRET_KEY": "${{ secrets.AWS_STAGING_SECRET_KEY }}",
            "ENVIRONMENT": "staging",
            "LOG_LEVEL": "debug",
            "PUBLIC_BASE_URL": "${{ vars.AWS_STAGING_PUBLIC_BASE_URL }}",
            "ALLOWED_ORIGINS": "${{ vars.AWS_STAGING_ALLOWED_ORIGINS }}"
          }
        },
        "ImageRepositoryType": "ECR"
      },
      "AuthenticationConfiguration": {
        "AccessRoleArn": "${ROLE_ARN}"
      },
      "AutoDeploymentsEnabled": false
    }
    EOF

    # Add optional tuning variables if configured (repeat for each)
    if [ -n "${{ vars.DB_MAX_CONNECTIONS }}" ]; then
      jq --arg v "${{ vars.DB_MAX_CONNECTIONS }}" \
        '.ImageRepository.ImageConfiguration.RuntimeEnvironmentVariables.DB_MAX_CONNECTIONS = $v' \
        /tmp/source-config.json > /tmp/tmp.json && mv /tmp/tmp.json /tmp/source-config.json
    fi

    aws apprunner update-service \
      --service-arn "${{ secrets.AWS_STAGING_APP_RUNNER_ARN }}" \
      --source-configuration file:///tmp/source-config.json \
      --region ${{ vars.AWS_REGION }}

    # Poll until deployment completes — equivalent of gcloud run deploy blocking
    echo "Waiting for deployment..."
    while true; do
      STATUS=$(aws apprunner describe-service \
        --service-arn "${{ secrets.AWS_STAGING_APP_RUNNER_ARN }}" \
        --region ${{ vars.AWS_REGION }} \
        --query 'Service.Status' --output text)
      echo "  Status: $STATUS"
      case "$STATUS" in
        RUNNING)              echo "Deployment complete"; break ;;
        OPERATION_IN_PROGRESS) sleep 15 ;;
        *)                    echo "Deployment failed: $STATUS"; exit 1 ;;
      esac
    done
```

The `jq` pattern for optional tuning variables applies to all the shared variables listed in
Step 8 (`DB_MAX_CONNECTIONS`, `DB_MIN_CONNECTIONS`, `DB_MAX_CONN_LIFETIME`, etc.). Add one
`if` block per variable using the same structure.

### Staging vs production differences

Everything else is identical between the two workflows. Swap these values:

| | Staging | Production |
|---|---|---|
| Image tag prefix | `staging-` | *(none)* |
| `ENVIRONMENT` | `staging` | `prod` |
| `LOG_LEVEL` | `debug` | `info` |
| Database URL secret | `AWS_STAGING_DATABASE_URL` | `AWS_DATABASE_URL` |
| Secret key | `AWS_STAGING_SECRET_KEY` | `AWS_SECRET_KEY` |
| Service ARN secret | `AWS_STAGING_APP_RUNNER_ARN` | `AWS_PROD_APP_RUNNER_ARN` |
| Public URL variable | `AWS_STAGING_PUBLIC_BASE_URL` | `AWS_PUBLIC_BASE_URL` |
| CORS origins variable | `AWS_STAGING_ALLOWED_ORIGINS` | `AWS_ALLOWED_ORIGINS` |

---

## Resource Summary

| Resource | Name | Purpose |
|---|---|---|
| ECR repository | `signalsd` | Container image storage (prod + staging) |
| IAM OIDC provider | `token.actions.githubusercontent.com` | Keyless auth for GitHub Actions |
| IAM role | `github-actions-signalsd` | Assumed by GitHub Actions workflows |
| IAM role | `apprunner-ecr-access` | Used by App Runner to pull from ECR |
| Auto-scaling config | `signalsd-prod` | 1–4 instances |
| Auto-scaling config | `signalsd-staging` | 1–2 instances |
| App Runner service | `signalsd` | Production |
| App Runner service | `signalsd-staging` | Staging |
