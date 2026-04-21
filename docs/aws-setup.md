# notes
All resources are tagged `Project=signalsd`

## Step 1 — ECR Repository

ECR (Elastic Container Registry) is the AWS equivalent of GCP Artifact Registry. A single repository is used for both environments, distinguished by image tag.

```bash
aws ecr create-repository \
  --repository-name signalsd \
  --image-scanning-configuration scanOnPush=true \
  --tags Key=Project,Value=signalsd \
  --region eu-west-1
```

The repository URI in the output will have the form:
`<ACCOUNT_ID>.dkr.ecr.eu-west-1.amazonaws.com/signalsd`

---

## Step 2 — GitHub OIDC Provider

Allows GitHub Actions to authenticate to AWS without storing any static credentials — the
equivalent of using Workload Identity Federation on GCP instead of a service account key.
This is a one-time step per AWS account and may already be present in a shared account.

Check whether it already exists and create it if not:

```bash
aws iam list-open-id-connect-providers \
  --query "OpenIDConnectProviderList[?contains(Arn, 'token.actions.githubusercontent.com')].Arn" \
  --output text
 ```

create if needed:
```bash

# note the cert thumbprint is valid as of April 2026 
aws iam create-open-id-connect-provider \
  --url https://token.actions.githubusercontent.com \
  --client-id-list sts.amazonaws.com \
  --thumbprint-list 6938fd4d98bab03faadb97b34396831e3780aea1 \
  --query 'OpenIDConnectProviderArn' \
  --output text
```
---

## Step 3 — GitHub Actions IAM Role

this role is is assumed by the GitHub Actions workflows via OIDC. It has two parts:
- a trust policy scoped to the signalsd repository only, and a permissions
- policy limited to ECR push and App Runner deploy actions.

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
        "apprunner:ListServices",
        "apprunner:UpdateService",
        "apprunner:DescribeService"
      ],
      "Resource": "*"
    },
    {
      "Sid": "SSMConfig",
      "Effect": "Allow",
      "Action": "ssm:GetParameter",
      "Resource": "arn:aws:ssm:eu-west-1:${ACCOUNT_ID}:parameter/signalsd/*"
    },
    {
      "Sid": "SecretsManagerDeploy",
      "Effect": "Allow",
      "Action": [
        "secretsmanager:GetSecretValue",
        "secretsmanager:DescribeSecret"
      ],
      "Resource": [
        "arn:aws:secretsmanager:eu-west-1:${ACCOUNT_ID}:secret:signalsd/staging/database-url*",
        "arn:aws:secretsmanager:eu-west-1:${ACCOUNT_ID}:secret:signalsd/staging/secret-key*",
        "arn:aws:secretsmanager:eu-west-1:${ACCOUNT_ID}:secret:signalsd/prod/database-url*",
        "arn:aws:secretsmanager:eu-west-1:${ACCOUNT_ID}:secret:signalsd/prod/secret-key*"
      ]
    }
  ]
}
EOF

# Create the role with the trust policy, then attach the permissions policy
aws iam create-role \
  --role-name github-actions-signalsd \
  --assume-role-policy-document file:///tmp/trust-policy.json \
  --tags Key=Project,Value=signalsd

aws iam put-role-policy \
  --role-name github-actions-signalsd \
  --policy-name signalsd-deploy \
  --policy-document file:///tmp/deploy-policy.json
```

the role ARN will be needed for GitHub secret `AWS_ROLE_ARN`.

use  the following to get it retrospectively
```bash
 aws iam get-role --role-name github-actions-signalsd --query 'Role.Arn' --output text
 ```


---

## Step 4 — App Runner ECR Access Role

App Runner needs its own IAM role to pull images from ECR at runtime. This is separate from
the GitHub Actions role because it is assumed by the App Runner service itself (not by the
workflow), and only needs read access to ECR — not push access.

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
  --assume-role-policy-document file:///tmp/apprunner-trust.json \
  --tags Key=Project,Value=signalsd

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
is always running.

> to retrieve the ARNs after creation use:
> `aws apprunner list-auto-scaling-configurations --region eu-west-1`

```bash
# Production: 1–4 instances, up to 100 concurrent requests per instance
aws apprunner create-auto-scaling-configuration \
  --auto-scaling-configuration-name signalsd-prod \
  --min-size 1 \
  --max-size 4 \
  --max-concurrency 100 \
  --tags Key=Project,Value=signalsd \
  --region eu-west-1 \
  --query 'AutoScalingConfiguration.AutoScalingConfigurationArn' \
  --output text

# Staging: 1–2 instances
aws apprunner create-auto-scaling-configuration \
  --auto-scaling-configuration-name signalsd-staging \
  --min-size 1 \
  --max-size 2 \
  --max-concurrency 100 \
  --tags Key=Project,Value=signalsd \
  --region eu-west-1 \
  --query 'AutoScalingConfiguration.AutoScalingConfigurationArn' \
  --output text

echo "Staging ASG ARN: $STAGING_ASG_ARN"
```

---

## Steps 6 & 7 — App Runner Services

App Runner separates service creation (`create-service`, run once here) from deployment
(`update-service`, run by the CD workflow on every deploy).

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

PROD_ASG_ARN=$(aws apprunner list-auto-scaling-configurations \
  --region eu-west-1 \
  --query "AutoScalingConfigurationSummaryList[?AutoScalingConfigurationName=='signalsd-prod'].AutoScalingConfigurationArn" \
  --output text)

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
  --health-check-configuration "Protocol=HTTP,Path=/health/live,Interval=10,Timeout=5,HealthyThreshold=1,UnhealthyThreshold=3" \
  --auto-scaling-configuration-arn "$PROD_ASG_ARN" \
  --tags Key=Project,Value=signalsd \
  --region eu-west-1
```

To retrieve the `ServiceArn` later: `aws apprunner list-services --region eu-west-aws apprunner list-services --region eu-west-11`

### Step 7 — Staging Service

```bash
ACCOUNT_ID=$(aws sts get-caller-identity --query Account --output text)
APPRUNNER_ROLE_ARN=$(aws iam get-role \
  --role-name apprunner-ecr-access \
  --query 'Role.Arn' --output text)

STAGING_ASG_ARN=$(aws apprunner list-auto-scaling-configurations \
  --region eu-west-1 \
  --query "AutoScalingConfigurationSummaryList[?AutoScalingConfigurationName=='signalsd-staging'].AutoScalingConfigurationArn" \
  --output text)

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
  --health-check-configuration "Protocol=HTTP,Path=/health/live,Interval=10,Timeout=5,HealthyThreshold=1,UnhealthyThreshold=3" \
  --auto-scaling-configuration-arn "$STAGING_ASG_ARN" \
  --tags Key=Project,Value=signalsd \
  --region eu-west-1
```

To retrieve the `ServiceArn` later: `aws apprunner list-services --region eu-west-1`

The services will initially fail health checks because no environment variables (DATABASE_URL
etc.) are set yet. 

Once both services are created and the GitHub variables from Step 10 are configured,
trigger the staging workflow via `workflow_dispatch` in GitHub Actions to verify the full
deployment pipeline end-to-end.

---

## Step 8 — VPC and RDS PostgreSQL

the service is setup to use a single `db.t4g.small` instance (2 vCPU burstable / 2 GB RAM) hosts both `signalsd_prod` and `signalsd_staging` as separate databases.

### Subnet architecture

The VPC uses four subnets across two AZs:

| Subnet | CIDR | AZ | Purpose |
|---|---|---|---|
| public-1a | 10.42.1.0/24 | eu-west-1a | RDS, NAT Gateway |
| public-1b | 10.42.2.0/24 | eu-west-1b | RDS |
| private-1a | 10.42.3.0/24 | eu-west-1a | App Runner VPC connector |
| private-1b | 10.42.4.0/24 | eu-west-1b | App Runner VPC connector |

RDS lives in public subnets (with public accessibility disabled — no public IP is assigned).
App Runner connector lives in private subnets and routes outbound internet traffic (e.g.
GitHub schema validation) through a NAT Gateway in the public subnet. This requires
`EgressType=VPC` on the App Runner services, which is incompatible with `EgressType=DEFAULT`.

The NAT Gateway costs ~$35/month fixed regardless of traffic.

### Create the VPC and subnets

```bash
REGION=eu-west-1
PROJECT_TAG="Key=Project,Value=signalsd"

VPC_ID=$(aws ec2 create-vpc \
  --cidr-block 10.42.0.0/16 \
  --region $REGION \
  --query 'Vpc.VpcId' --output text)

aws ec2 create-tags --resources $VPC_ID \
  --tags Key=Name,Value=signalsd $PROJECT_TAG

# DNS hostnames required for RDS endpoint resolution
aws ec2 modify-vpc-attribute --vpc-id $VPC_ID --enable-dns-hostnames

# Public subnets (RDS)
PUBLIC_SUBNET_1=$(aws ec2 create-subnet \
  --vpc-id $VPC_ID --cidr-block 10.42.1.0/24 \
  --availability-zone eu-west-1a \
  --query 'Subnet.SubnetId' --output text)

PUBLIC_SUBNET_2=$(aws ec2 create-subnet \
  --vpc-id $VPC_ID --cidr-block 10.42.2.0/24 \
  --availability-zone eu-west-1b \
  --query 'Subnet.SubnetId' --output text)

# Private subnets (App Runner VPC connector)
PRIVATE_SUBNET_1=$(aws ec2 create-subnet \
  --vpc-id $VPC_ID --cidr-block 10.42.3.0/24 \
  --availability-zone eu-west-1a \
  --query 'Subnet.SubnetId' --output text)

PRIVATE_SUBNET_2=$(aws ec2 create-subnet \
  --vpc-id $VPC_ID --cidr-block 10.42.4.0/24 \
  --availability-zone eu-west-1b \
  --query 'Subnet.SubnetId' --output text)

aws ec2 create-tags --resources $PUBLIC_SUBNET_1 \
  --tags Key=Name,Value=signalsd-public-1a $PROJECT_TAG
aws ec2 create-tags --resources $PUBLIC_SUBNET_2 \
  --tags Key=Name,Value=signalsd-public-1b $PROJECT_TAG
aws ec2 create-tags --resources $PRIVATE_SUBNET_1 \
  --tags Key=Name,Value=signalsd-private-1a $PROJECT_TAG
aws ec2 create-tags --resources $PRIVATE_SUBNET_2 \
  --tags Key=Name,Value=signalsd-private-1b $PROJECT_TAG
```

### Internet Gateway and public route table

```bash
IGW_ID=$(aws ec2 create-internet-gateway \
  --region $REGION \
  --query 'InternetGateway.InternetGatewayId' --output text)

aws ec2 create-tags --resources $IGW_ID \
  --tags Key=Name,Value=signalsd $PROJECT_TAG

aws ec2 attach-internet-gateway \
  --internet-gateway-id $IGW_ID \
  --vpc-id $VPC_ID

PUBLIC_RTB=$(aws ec2 create-route-table \
  --vpc-id $VPC_ID \
  --query 'RouteTable.RouteTableId' --output text)

aws ec2 create-tags --resources $PUBLIC_RTB \
  --tags Key=Name,Value=signalsd-public $PROJECT_TAG

aws ec2 create-route \
  --route-table-id $PUBLIC_RTB \
  --destination-cidr-block 0.0.0.0/0 \
  --gateway-id $IGW_ID

aws ec2 associate-route-table --route-table-id $PUBLIC_RTB --subnet-id $PUBLIC_SUBNET_1
aws ec2 associate-route-table --route-table-id $PUBLIC_RTB --subnet-id $PUBLIC_SUBNET_2
```

### NAT Gateway and private route table

The NAT Gateway gives App Runner internet access (GitHub etc.) while keeping it in a
private subnet alongside RDS.

```bash
# Allocate a static public IP for the NAT Gateway
EIP_ALLOC=$(aws ec2 allocate-address \
  --domain vpc \
  --region $REGION \
  --query 'AllocationId' --output text)

aws ec2 create-tags --resources $EIP_ALLOC \
  --tags Key=Name,Value=signalsd $PROJECT_TAG

NAT_GW=$(aws ec2 create-nat-gateway \
  --subnet-id $PUBLIC_SUBNET_1 \
  --allocation-id $EIP_ALLOC \
  --region $REGION \
  --query 'NatGateway.NatGatewayId' --output text)

aws ec2 create-tags --resources $NAT_GW \
  --tags Key=Name,Value=signalsd $PROJECT_TAG

# Wait for NAT Gateway to become available (~1 minute)
aws ec2 wait nat-gateway-available \
  --nat-gateway-ids $NAT_GW \
  --region $REGION
echo "NAT Gateway ready"

PRIVATE_RTB=$(aws ec2 create-route-table \
  --vpc-id $VPC_ID \
  --query 'RouteTable.RouteTableId' --output text)

aws ec2 create-tags --resources $PRIVATE_RTB \
  --tags Key=Name,Value=signalsd-private $PROJECT_TAG

aws ec2 create-route \
  --route-table-id $PRIVATE_RTB \
  --destination-cidr-block 0.0.0.0/0 \
  --nat-gateway-id $NAT_GW

aws ec2 associate-route-table --route-table-id $PRIVATE_RTB --subnet-id $PRIVATE_SUBNET_1
aws ec2 associate-route-table --route-table-id $PRIVATE_RTB --subnet-id $PRIVATE_SUBNET_2
```

### Security groups

```bash
# RDS: inbound 5432 from the App Runner connector (and temporarily from your IP)
RDS_SG=$(aws ec2 create-security-group \
  --group-name signalsd-rds \
  --description "RDS access for signalsd" \
  --vpc-id $VPC_ID \
  --query 'GroupId' --output text)

aws ec2 create-tags --resources $RDS_SG --tags $PROJECT_TAG

# App Runner VPC connector
CONNECTOR_SG=$(aws ec2 create-security-group \
  --group-name signalsd-apprunner-connector \
  --description "App Runner VPC connector for signalsd" \
  --vpc-id $VPC_ID \
  --query 'GroupId' --output text)

aws ec2 create-tags --resources $CONNECTOR_SG --tags $PROJECT_TAG

# Permanent rule: App Runner connector -> RDS
aws ec2 authorize-security-group-ingress \
  --group-id $RDS_SG \
  --protocol tcp --port 5432 \
  --source-group $CONNECTOR_SG

# Temporary rule: your local IP -> RDS (for initial database setup)
MY_IP=$(curl -s https://checkip.amazonaws.com)
aws ec2 authorize-security-group-ingress \
  --group-id $RDS_SG \
  --protocol tcp --port 5432 \
  --cidr ${MY_IP}/32
```

### DB subnet group

```bash
aws rds create-db-subnet-group \
  --db-subnet-group-name signalsd \
  --db-subnet-group-description "signalsd RDS subnet group" \
  --subnet-ids $PUBLIC_SUBNET_1 $PUBLIC_SUBNET_2 \
  --tags Key=Project,Value=signalsd
```

### Create the RDS instance

PostgreSQL 18 — `rds.force_ssl` and `--auto-minor-version-upgrade` are on by default.
Storage autoscales from 20 GB up to 100 GB as needed (~$0.115/GB/month on gp3).

The master user (`signalsd_admin`) is used only for initial database setup and is not used
by the application. A self-managed password is supplied at creation and stored in your
password manager — this avoids the unpredictable secret name generated by
`--manage-master-user-password`.

Generate a strong random password:

```bash
# Generates a 32-character alphanumeric password 
ADMIN_PASSWORD=$(LC_ALL=C tr -dc 'A-Za-z0-9' < /dev/urandom | head -c 32)
echo $ADMIN_PASSWORD
```

Store the password in Secrets Manager before proceeding:

```bash
aws secretsmanager create-secret \
  --name "signalsd/admin/master-password" \
  --secret-string "$ADMIN_PASSWORD" \
  --tags Key=Project,Value=signalsd \
  --region $REGION
```

```bash
aws rds create-db-instance \
  --db-instance-identifier signalsd \
  --db-instance-class db.t4g.small \
  --engine postgres \
  --engine-version 18.3 \
  --master-username signalsd_admin \
  --master-user-password "$ADMIN_PASSWORD" \
  --allocated-storage 20 \
  --max-allocated-storage 100 \
  --storage-type gp3 \
  --db-subnet-group-name signalsd \
  --vpc-security-group-ids $RDS_SG \
  --no-multi-az \
  --publicly-accessible \
  --backup-retention-period 7 \
  --tags $PROJECT_TAG \
  --region $REGION
```

Wait for the instance to become available 

```bash
aws rds wait db-instance-available \
  --db-instance-identifier signalsd \
  --region $REGION
echo "RDS ready"
```

Retrieve the endpoint:

```bash
RDS_ENDPOINT=$(aws rds describe-db-instances \
  --db-instance-identifier signalsd \
  --region $REGION \
  --query 'DBInstances[0].Endpoint.Address' --output text)
echo $RDS_ENDPOINT
```

### Create databases and application users

Connect via psql (requires `psql` installed locally - `brew install libpq`)

```bash
ADMIN_PASSWORD=$(aws secretsmanager get-secret-value \
  --secret-id "signalsd/admin/master-password" \
  --region $REGION \
  --query SecretString --output text)

PGPASSWORD=$ADMIN_PASSWORD psql \
  -h $RDS_ENDPOINT -U signalsd_admin -d postgres
```

Run the following SQL (choose strong passwords for the two application users — these go into
Secrets Manager in Step 9):

PROD_DB_PASSWORD=$(LC_ALL=C tr -dc 'A-Za-z0-9' < /dev/urandom | head -c 32)
STAGING_DB_PASSWORD=$(LC_ALL=C tr -dc 'A-Za-z0-9' < /dev/urandom | head -c 32)

```sql
CREATE DATABASE signalsd_prod;
CREATE DATABASE signalsd_staging;

CREATE USER signalsd_prod WITH PASSWORD 'CHOOSE_PROD_PASSWORD';
CREATE USER signalsd_staging WITH PASSWORD 'CHOOSE_STAGING_PASSWORD';

GRANT ALL PRIVILEGES ON DATABASE signalsd_prod TO signalsd_prod;
GRANT ALL PRIVILEGES ON DATABASE signalsd_staging TO signalsd_staging;

\c signalsd_prod
GRANT ALL ON SCHEMA public TO signalsd_prod;

\c signalsd_staging
GRANT ALL ON SCHEMA public TO signalsd_staging;

\q
```

The connection strings for Step 9 will be:
- **Prod**: `postgresql://signalsd_prod:$PROD_DB_PASSWORD@$RDS_ENDPOINT:5432/signalsd_prod?sslmode=require`
- **Staging**: `postgresql://signalsd_staging:$STAGING_DB_PASSWORD@$RDS_ENDPOINT:5432/signalsd_staging?sslmode=require`

### Remove temporary local access

Remove the temporary rule that allowed your local IP during initial setup:

```bash
# Remove temporary local IP rule
aws ec2 revoke-security-group-ingress \
  --group-id $RDS_SG \
  --protocol tcp --port 5432 \
  --cidr ${MY_IP}/32
```

RDS is kept publicly accessible so that GitHub Actions runners can reach it during
migrations. Access is controlled entirely by the security group — the only permanent
inbound rule allows port 5432 from the App Runner connector. The CD workflows add and
remove a `/32` rule for the runner IP for the duration of each migration step.

### Create App Runner VPC Connector

The connector uses the private subnets so App Runner can reach RDS within the VPC.
Outbound internet traffic routes through the NAT Gateway in the public subnet.

```bash
VPC_CONNECTOR_ARN=$(aws apprunner create-vpc-connector \
  --vpc-connector-name signalsd \
  --subnets $PRIVATE_SUBNET_1 $PRIVATE_SUBNET_2 \
  --security-groups $CONNECTOR_SG \
  --tags Key=Project,Value=signalsd \
  --region $REGION \
  --query 'VpcConnector.VpcConnectorArn' --output text)

echo "VPC Connector ARN: $VPC_CONNECTOR_ARN"
```

To retrieve this ARN later:

```bash
aws apprunner list-vpc-connectors --region $REGION \
  --query "VpcConnectors[?VpcConnectorName=='signalsd'].VpcConnectorArn" --output text
```

### Attach the VPC connector to both App Runner services

`EgressType=VPC` routes all App Runner traffic through the VPC connector. Internet access
is provided by the NAT Gateway rather than App Runner's default routing.

```bash
PROD_SERVICE_ARN=$(aws apprunner list-services --region $REGION \
  --query "ServiceSummaryList[?ServiceName=='signalsd'].ServiceArn" --output text)

STAGING_SERVICE_ARN=$(aws apprunner list-services --region $REGION \
  --query "ServiceSummaryList[?ServiceName=='signalsd-staging'].ServiceArn" --output text)

for ARN in "$PROD_SERVICE_ARN" "$STAGING_SERVICE_ARN"; do
  aws apprunner update-service \
    --service-arn "$ARN" \
    --network-configuration "EgressConfiguration={EgressType=VPC,VpcConnectorArn=${VPC_CONNECTOR_ARN}}" \
    --region $REGION
done


---

## Step 9 — Secrets Manager

Application secrets (database credentials and the application secret key) are stored in AWS
Secrets Manager.

App Runner fetches secrets directly from Secrets Manager at container startup using an
instance role created below. 

### Create the secrets

The RDS endpoint is retrieved from AWS. The database passwords are those set in Step 8.
Secret keys are generated randomly.

```bash
ACCOUNT_ID=$(aws sts get-caller-identity --query Account --output text)
REGION=eu-west-1

RDS_ENDPOINT=$(aws rds describe-db-instances \
  --db-instance-identifier signalsd \
  --region $REGION \
  --query 'DBInstances[0].Endpoint.Address' --output text)

# check the DB password envs are set (step 8)
echo $STAGING_DB_PASSWORD
echo $PROD_DB_PASSWORD

# Staging
STAGING_DB_ARN=$(aws secretsmanager create-secret \
  --name "signalsd/staging/database-url" \
  --secret-string "postgresql://signalsd_staging:${STAGING_DB_PASSWORD}@${RDS_ENDPOINT}:5432/signalsd_staging?sslmode=require" \
  --tags Key=Project,Value=signalsd \
  --region $REGION \
  --query ARN --output text)

STAGING_KEY_ARN=$(aws secretsmanager create-secret \
  --name "signalsd/staging/secret-key" \
  --secret-string "$(LC_ALL=C tr -dc 'A-Za-z0-9' < /dev/urandom | head -c 64)" \
  --tags Key=Project,Value=signalsd \
  --region $REGION \
  --query ARN --output text)

# Production
PROD_DB_ARN=$(aws secretsmanager create-secret \
  --name "signalsd/prod/database-url" \
  --secret-string "postgresql://signalsd_prod:${PROD_DB_PASSWORD}@${RDS_ENDPOINT}:5432/signalsd_prod?sslmode=require" \
  --tags Key=Project,Value=signalsd \
  --region $REGION \
  --query ARN --output text)

PROD_KEY_ARN=$(aws secretsmanager create-secret \
  --name "signalsd/prod/secret-key" \
  --secret-string "$(LC_ALL=C tr -dc 'A-Za-z0-9' < /dev/urandom | head -c 64)" \
  --tags Key=Project,Value=signalsd \
  --region $REGION \
  --query ARN --output text)

echo "Staging DB ARN:  $STAGING_DB_ARN"
echo "Staging Key ARN: $STAGING_KEY_ARN"
echo "Prod DB ARN:     $PROD_DB_ARN"
echo "Prod Key ARN:    $PROD_KEY_ARN"
```


### Create the App Runner instance role

App Runner needs an IAM role to fetch secrets at container startup. This is distinct from
`apprunner-ecr-access` (which handles ECR image pulls) — it is assumed by the running
container process, equivalent to a GCP Cloud Run runtime service account.

```bash
cat > /tmp/instance-trust.json << 'EOF'
{
  "Version": "2012-10-17",
  "Statement": [{
    "Effect": "Allow",
    "Principal": { "Service": "tasks.apprunner.amazonaws.com" },
    "Action": "sts:AssumeRole"
  }]
}
EOF

aws iam create-role \
  --role-name apprunner-instance-role \
  --assume-role-policy-document file:///tmp/instance-trust.json \
  --tags Key=Project,Value=signalsd

aws iam put-role-policy \
  --role-name apprunner-instance-role \
  --policy-name secrets-manager-read \
  --policy-document "{
    \"Version\": \"2012-10-17\",
    \"Statement\": [{
      \"Effect\": \"Allow\",
      \"Action\": \"secretsmanager:GetSecretValue\",
      \"Resource\": [
        \"arn:aws:secretsmanager:${REGION}:${ACCOUNT_ID}:secret:signalsd/staging/database-url*\",
        \"arn:aws:secretsmanager:${REGION}:${ACCOUNT_ID}:secret:signalsd/staging/secret-key*\",
        \"arn:aws:secretsmanager:${REGION}:${ACCOUNT_ID}:secret:signalsd/prod/database-url*\",
        \"arn:aws:secretsmanager:${REGION}:${ACCOUNT_ID}:secret:signalsd/prod/secret-key*\"
      ]
    }]
  }"

INSTANCE_ROLE_ARN=$(aws iam get-role \
  --role-name apprunner-instance-role \
  --query 'Role.Arn' --output text)

echo "Instance role ARN: $INSTANCE_ROLE_ARN"
```

### Create SSM parameters for non-sensitive config

The UI is served by the same process on the same port as the API, so `PUBLIC_BASE_URL` and
`ALLOWED_ORIGINS` are the same value — the App Runner service URL. Retrieve both service
URLs before setting the parameters:

> **Custom domain:** if you add a custom domain in front of App Runner, both parameters
> must be updated to the custom domain — `PUBLIC_BASE_URL` because it is used to construct
> absolute URLs in API responses, and `ALLOWED_ORIGINS` because browsers send the domain
> the user is actually on as the `Origin` header, which must match exactly. Update both
> with `--overwrite` and redeploy:
> ```bash
> aws ssm put-parameter --name "/signalsd/prod/public-base-url" \
>   --value "https://your-custom-domain.com" --type String --overwrite --region $REGION
> aws ssm put-parameter --name "/signalsd/prod/allowed-origins" \
>   --value "https://your-custom-domain.com" --type String --overwrite --region $REGION
> ```

```bash
PROD_URL=$(aws apprunner describe-service \
  --service-arn "$PROD_SERVICE_ARN" \
  --region $REGION \
  --query 'Service.ServiceUrl' --output text)

STAGING_URL=$(aws apprunner describe-service \
  --service-arn "$STAGING_SERVICE_ARN" \
  --region $REGION \
  --query 'Service.ServiceUrl' --output text)

echo "Prod:    https://${PROD_URL}"
echo "Staging: https://${STAGING_URL}"
```

```bash
# Staging
aws ssm put-parameter \
  --name "/signalsd/staging/public-base-url" \
  --value "https://${STAGING_URL}" \
  --type String --tags Key=Project,Value=signalsd \
  --region $REGION

aws ssm put-parameter \
  --name "/signalsd/staging/allowed-origins" \
  --value "https://${STAGING_URL}" \
  --type String --tags Key=Project,Value=signalsd \
  --region $REGION

# Production
aws ssm put-parameter \
  --name "/signalsd/prod/public-base-url" \
  --value "https://${PROD_URL}" \
  --type String --tags Key=Project,Value=signalsd \
  --region $REGION

aws ssm put-parameter \
  --name "/signalsd/prod/allowed-origins" \
  --value "https://${PROD_URL}" \
  --type String --tags Key=Project,Value=signalsd \
  --region $REGION
```

Optional tuning variables can be added at any time using the same pattern, under
`/signalsd/staging/<PARAM>` or `/signalsd/prod/<PARAM>`. The workflow checks for each one
at deploy time and includes it if present. Supported parameters: `DB_MAX_CONNECTIONS`,
`DB_MIN_CONNECTIONS`, `DB_MAX_CONN_LIFETIME`, `DB_MAX_CONN_IDLE_TIME`, `DB_CONNECT_TIMEOUT`,
`RATE_LIMIT_RPS`, `RATE_LIMIT_BURST`, `MAX_SIGNAL_PAYLOAD_SIZE`, `READ_TIMEOUT`,
`WRITE_TIMEOUT`, `IDLE_TIMEOUT`.

To update any parameter, use `--overwrite`:
```bash
aws ssm put-parameter \
  --name "/signalsd/prod/public-base-url" \
  --value "https://api.newdomain.com" \
  --type String --overwrite --region $REGION
```

### Attach the instance role to both services

```bash
PROD_SERVICE_ARN=$(aws apprunner list-services --region $REGION \
  --query "ServiceSummaryList[?ServiceName=='signalsd'].ServiceArn" --output text)

STAGING_SERVICE_ARN=$(aws apprunner list-services --region $REGION \
  --query "ServiceSummaryList[?ServiceName=='signalsd-staging'].ServiceArn" --output text)

INSTANCE_ROLE_ARN=$(aws iam get-role \
  --role-name apprunner-instance-role \
  --query 'Role.Arn' --output text)

aws apprunner update-service \
  --service-arn "$PROD_SERVICE_ARN" \
  --instance-configuration "InstanceRoleArn=${INSTANCE_ROLE_ARN}" \
  --region $REGION

aws apprunner update-service \
  --service-arn "$STAGING_SERVICE_ARN" \
  --instance-configuration "InstanceRoleArn=${INSTANCE_ROLE_ARN}" \
  --region $REGION
```

The services will not reach `RUNNING` at this point — `DATABASE_URL` and `SECRET_KEY` are
only injected into the App Runner service configuration by the CD workflow on first deploy
(via `RuntimeEnvironmentSecrets`). Proceed to Steps 10–12, then trigger the staging
workflow manually via `workflow_dispatch` to complete the setup.

---

## Step 10 — GitHub Repository Secrets and Variables

The AWS workflows derive all configuration from AWS at runtime. `REGION`, `ENVIRONMENT`,
and `LOG_LEVEL` are hardcoded in the workflow `env:` block; the account ID is obtained via
`aws sts get-caller-identity`; and everything else (service ARNs, secret ARNs, SSM
parameters) is fetched from AWS by name at deploy time. The only value that must be stored
in GitHub is the IAM role ARN, which is needed before AWS authentication can be established.

### Secrets
recommended to store as a secret - although not senesitive it is better not to expose the account id unless neeeded

```bash
# get te aws role arn
aws iam get-role --role-name github-actions-signalsd --query 'Role.Arn' --output text
```

save the role as: `AWS_ROLE_ARN`

---

## Step 11 — Dockerfile Change

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

## Step 12 — CD Workflow Files

The workflow files are:

- [.github/workflows/cd-staging-aws.yml](../.github/workflows/cd-staging-aws.yml) — triggers on CI success on `main`
- [.github/workflows/cd-production-aws.yml](../.github/workflows/cd-production-aws.yml) — triggers on `v*` tag push

Both support `workflow_dispatch` for manual runs. The existing GCP workflows are unchanged — both clouds deploy independently from the same CI pipeline.
