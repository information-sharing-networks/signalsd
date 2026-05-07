
## AWS

A `staging` and `production` version of the software runs in `eu-west-1`.

### Compute and traffic

Container compute runs on ECS Express Mode (Fargate) behind an Application Load Balancer
that ECS provisions automatically. There are two services — `signalsd` (production) and
`signalsd-staging` — sharing a single ALB, with host-header listener rules routing
traffic to the correct service. Each service receives a stable default URL of the form
`https://<service-name>.ecs.<region>.on.aws`.

Custom domains are added by issuing an ACM certificate, attaching it to the shared ALB,
and adding the custom hostname as an additional host-header value on the relevant
service's listener rule. The default URL is preserved as a second value on the same rule
so both hostnames continue to resolve.

### Authentication

The deploy pipeline authenticates via GitHub OIDC: GitHub's identity token is exchanged
for short-lived AWS credentials by assuming an IAM role scoped to this repository. Three
IAM roles are involved:

- **Deploy role** (`github-actions-signalsd`, assumed by the workflow): push to ECR,
  create and update ECS Express services, read secrets and SSM parameters, and manage its own runner IP on the RDS security group during migrations.
- **Task-execution role** (`ecs-task-execution-signalsd`, assumed by Fargate at task
  start): image pull from ECR, log writes to CloudWatch, secret injection from Secrets Manager.

- **Infrastructure role** (`ecs-infrastructure-signalsd`, assumed by ECS to provision Express resources): ALB, auto-scaling, security groups and ACM bindings.

The task-execution and infrastructure role names are hardcoded in the workflow files
and must match exactly. The deploy role name is referenced only via the `AWS_ROLE_ARN` GitHub secret.

### Configuration and secrets

Each environment has its own isolated set of secrets and parameters under matching
paths:

- **Secrets Manager** (`signalsd/<env>/database-url`, `signalsd/<env>/secret-key`):
  fetched at task start by the task-execution role and injected as `DATABASE_URL` and `SECRET_KEY`.
- **SSM Parameter Store** (`signalsd/<env>/<param>`): non-sensitive runtime config —
  `public-base-url`, `allowed-origins`, plus any of the optional tuning variables listed in [Environment Variables](#environment-variables). The SSM parameter name maps to an env var (e.g. `db-max-connections` -> `DB_MAX_CONNECTIONS`). 
  The app uses its internal defaults where optional params are not set.

A separate `signalsd/admin/master-password` secret holds the RDS master password, (used only during initial database setup)

### Database and migrations

A single `db.t4g.small` PostgreSQL 18 RDS instance hosts both `signalsd_prod` and
`signalsd_staging` databases, with one application DB user per database. The instance runs with `rds.force_ssl` on and clients connect with `sslmode=require`.

ECS tasks reach the database through a security-group rule pinned to the auto-managed ECS task security group. RDS is kept publicly accessible so that GitHub Actions runners can apply schema migrations: the deploy workflow temporarily authorizes its own runner IP on the RDS security group, runs `goose` against the public endpoint, and revokes the rule before continuing.

### Networking

The VPC uses two public subnets across two AZs with an Internet Gateway:
ECS tasks receive public IPs and egress through the Internet Gateway, but inbound traffic is restricted to the ALB security group.

Note:

- **ALB type is fixed by the first service.** When the first ECS Express service is
  created in a VPC, ECS auto-provisions a shared ALB and picks its type from that
  service's subnets: public subnets produce an internet-facing ALB, private subnets an internal one. Every subsequent Express service in the VPC inherits the same ALB, and the choice cannot easily be reversed. This project needs an internet-facing ALB, so the first service deployed must use public subnets — both services here share the same two public subnets to ensure they bind to the same internet-facing ALB.

- **Task security group is auto-created and must be wired to RDS manually.** ECS Express creates a task security group tagged `AmazonECSManaged=true` the first time a service is deployed. The application cannot reach RDS until that SG is added as an ingress rule on the RDS security group (`signalsd-rds`). This is a one-off step after the first service is created.

### Container image

A single ECR repository (`signalsd`) holds images for both environments, distinguished
by tag. Staging builds push `staging-<sha>` and `staging-latest`; production deploys
re-tag the matching staging image as `<sha>` and `latest` rather than rebuilding.


ECR basic image scanning is enabled on the repository (`scanOnPush=true`).

The application image contains only a statically compiled Go binary in a scratch image (no operating system or shell) — so basic scanning (which looks for OS-package CVEs) is not expected to return any useful information.

The configuration is in place in case different image configs are loaded at a later date.  Today's vulnerability checks on the go app come from CI: `govulncheck` and `gosec` run on every build before the image is created.

### Setup checklist

All resources are tagged `Project=signalsd`. The workflow relies on this tag (combined with the security group's name) to discover the RDS security group at deploy time, so the convention must be followed throughout.

To set up an equivalent environment, an admin needs:

- An ECR repository (`signalsd`) with image scanning on push.
- The GitHub OIDC provider registered for the AWS account.
- The three IAM roles described above (deploy, task-execution, infrastructure).
- A VPC with two public subnets across two AZs, an Internet Gateway, and a security
  group for RDS that allows the ECS task SG on port 5432.
- An RDS PostgreSQL instance with the two databases and per-env application users.
- Secrets Manager entries for `signalsd/<env>/database-url` and
  `signalsd/<env>/secret-key`, and SSM parameters under `signalsd/<env>/` for at least
  `public-base-url` and `allowed-origins`.
- Two ECS Express services (`signalsd`, `signalsd-staging`) with appropriate CPU/memory
  and the auto-scaling minimum/maximum each environment needs.
- Two GitHub repository secrets:
  - `AWS_ACCOUNT_ID` — used to construct ECR registry URLs and role ARNs in the
  - `AWS_ROLE_ARN` — the deploy role ARN.
    workflows.

Get the values for the gihub secrets using
```bash
# AWS_ACCOUNT_ID
aws sts get-caller-identity --query Account --output text

# ARW_ROLE_ARN
aws iam get-role --role-name github-actions-signalsd --query 'Role.Arn' --output text
```
