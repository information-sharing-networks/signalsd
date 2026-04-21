# AWS Resources Overview

This document summarises every resource created by the signalsd AWS setup and explains why each exists.

---

## Container Registry

**ECR Repository** (`signalsd`)
Stores Docker images built by the CI/CD pipeline. A single repository serves both environments, distinguished by image tag (`staging-<sha>` vs `prod-<sha>`). Equivalent to GCP Artifact Registry.

---

## Identity and Access Management

**GitHub OIDC Provider**
Allows GitHub Actions to authenticate to AWS without storing long-lived credentials. GitHub presents a short-lived token; AWS validates it against the registered provider. Equivalent to GCP Workload Identity Federation. This is an account-level resource and may already exist in a shared account.

**IAM Role: `github-actions-signalsd`**
Assumed by GitHub Actions workflows via OIDC. Grants the minimum permissions needed to run a deployment: ECR push, App Runner update/describe, and read access to the Secrets Manager and SSM Parameter Store paths used by the deploy scripts.

**IAM Role: `apprunner-ecr-access`**
Assumed by the App Runner *build service* (not the running container) to pull images from ECR. Required because ECR is a private registry — App Runner needs explicit permission to read from it.

**IAM Role: `apprunner-instance-role`**
Assumed by the running container process at startup. Grants read access to the four Secrets Manager secrets (`database-url` and `secret-key` for each environment). Equivalent to a GCP Cloud Run runtime service account.

---

## Compute

**App Runner Auto-scaling Configurations** (`signalsd-prod`, `signalsd-staging`)
Define the scaling bounds for each service. Production allows 1–4 instances with up to 100 concurrent requests per instance; staging allows 1–2. Unlike Cloud Run, App Runner does not scale to zero — there is always at least one instance running.

**App Runner Services** (`signalsd`, `signalsd-staging`)
The managed compute layer. App Runner handles container scheduling, load balancing, TLS termination, and health checks. Each service runs the same Docker image with environment-specific configuration injected at deploy time via environment variables and secrets.

---

## Networking

The networking layer exists because App Runner services need to reach the RDS database (private, inside the VPC) while also making outbound internet connections (GitHub schema validation). These two requirements force a specific architecture.

**VPC** (`10.42.0.0/16`)
An isolated virtual network. The CIDR is chosen to avoid collisions with other tenants in the shared account. All network resources below live inside this VPC.

**Public Subnets** (`signalsd-public-1a`, `signalsd-public-1b` — two AZs)
Host the RDS instance and the NAT Gateway. RDS requires two subnets across two availability zones for the DB subnet group, even with `--no-multi-az`. Public accessibility on the RDS instance is disabled — these subnets are "public" only in the sense that they have a route to the Internet Gateway, not that RDS is reachable from the internet.

**Private Subnets** (`signalsd-private-1a`, `signalsd-private-1b` — two AZs)
Host the App Runner VPC connector. Traffic from App Runner enters the VPC here.

**Internet Gateway**
Provides the VPC with a route to the internet. Required for the NAT Gateway to function.

**NAT Gateway + Elastic IP**
Allows App Runner (running in private subnets) to make outbound internet connections — specifically to GitHub, to validate JSON schemas. Without the NAT Gateway, `EgressType=VPC` would cut off all internet access. Fixed cost of ~$35/month regardless of traffic.

**Route Tables** (public and private)
The public route table sends `0.0.0.0/0` traffic to the Internet Gateway (for the NAT Gateway itself). The private route table sends `0.0.0.0/0` traffic to the NAT Gateway (for App Runner outbound internet).

**Security Groups** (`signalsd-rds`, `signalsd-apprunner-connector`)
Firewall rules. The RDS security group permanently allows inbound port 5432 from the App Runner connector security group only — the database is not reachable from anywhere else. A temporary rule allowing access from a local IP is added during initial database setup and then removed.

**App Runner VPC Connector**
Attaches App Runner services to the VPC private subnets. Required for App Runner to reach RDS. Configured with `EgressType=VPC`, which routes all App Runner traffic through the VPC (and thus through the NAT Gateway for internet-bound traffic).

---

## Database

**RDS Instance** (`signalsd`, `db.t4g.small`, PostgreSQL 18)
A single database instance hosting both `signalsd_prod` and `signalsd_staging` as separate databases with separate users. `db.t4g.small` (2 vCPU / 2 GB RAM) provides ~170 max connections. Storage autoscales from 20 GB to 100 GB on demand. SSL is enforced. 7-day automated backups are enabled.

**DB Subnet Group**
An RDS prerequisite that registers the two public subnets as eligible placement targets. Required even when multi-AZ is disabled.

---

## Configuration and Secrets

**Secrets Manager Secrets** (4 application + 1 admin)
- `signalsd/staging/database-url` — PostgreSQL connection string for staging
- `signalsd/staging/secret-key` — application signing key for staging
- `signalsd/prod/database-url` — PostgreSQL connection string for production
- `signalsd/prod/secret-key` — application signing key for production
- `signalsd/admin/master-password` — RDS master user password (setup use only)

Application secrets are injected into running containers by the CD workflow via App Runner `RuntimeEnvironmentSecrets`. The admin password is used only for initial database setup via psql.

**SSM Parameter Store Parameters** (4)
- `/signalsd/staging/public-base-url` — base URL included in API responses
- `/signalsd/staging/allowed-origins` — CORS allowed origin
- `/signalsd/prod/public-base-url`
- `/signalsd/prod/allowed-origins`

Non-sensitive config is kept in SSM rather than Secrets Manager to allow inspection without secret-read permissions. The CD workflow reads these by name at deploy time.

---

## Summary Table

| Resource | Count | Monthly cost (approx) |
|---|---|---|
| ECR repository | 1 | ~$0.10/GB stored |
| IAM roles | 3 | Free |
| App Runner services | 2 | ~$5/month per instance (1 vCPU/2 GB) |
| App Runner auto-scaling configs | 2 | Free |
| VPC + subnets + route tables | 1 VPC, 4 subnets, 2 RTBs | Free |
| Internet Gateway | 1 | Free |
| NAT Gateway + EIP | 1 | ~$35/month fixed |
| RDS instance (db.t4g.small) | 1 | ~$25/month |
| RDS storage (20 GB gp3) | 1 | ~$2.30/month |
| Secrets Manager secrets | 5 | ~$0.40/month |
| SSM parameters (Standard) | 4 | Free |

Dominant costs are the NAT Gateway (~$35/month) and the always-on App Runner instance (~$5/month minimum per service).
