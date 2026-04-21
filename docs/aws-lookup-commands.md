# AWS Lookup Commands

Quick reference for retrieving resource identifiers after the environment is set up.

```bash
REGION=eu-west-1
ACCOUNT_ID=$(aws sts get-caller-identity --query Account --output text)
```

---

## Account

```bash
# Account ID
aws sts get-caller-identity --query Account --output text

# Caller identity (who you are currently authenticated as)
aws sts get-caller-identity
```

---

## ECR

```bash
# Repository URI
aws ecr describe-repositories \
  --repository-names signalsd \
  --region $REGION \
  --query 'repositories[0].repositoryUri' --output text

# Images (most recent first)
aws ecr describe-images \
  --repository-name signalsd \
  --region $REGION \
  --query 'sort_by(imageDetails, &imagePushedAt)[-10:].imageTags' \
  --output table
```

---

## GitHub Actions Secret

```bash
# AWS_ROLE_ARN — the value to store in GitHub secrets
aws iam get-role --role-name github-actions-signalsd \
  --query 'Role.Arn' --output text
```

---

## IAM Roles

```bash
# GitHub Actions deploy role
aws iam get-role --role-name github-actions-signalsd \
  --query 'Role.Arn' --output text

# App Runner ECR access role (image pulls)
aws iam get-role --role-name apprunner-ecr-access \
  --query 'Role.Arn' --output text

# App Runner instance role (runtime secrets access)
aws iam get-role --role-name apprunner-instance-role \
  --query 'Role.Arn' --output text
```

---

## App Runner

```bash
# Service ARNs
PROD_SERVICE_ARN=$(aws apprunner list-services --region $REGION \
  --query "ServiceSummaryList[?ServiceName=='signalsd'].ServiceArn" --output text)

STAGING_SERVICE_ARN=$(aws apprunner list-services --region $REGION \
  --query "ServiceSummaryList[?ServiceName=='signalsd-staging'].ServiceArn" --output text)

# Service URLs
aws apprunner describe-service --service-arn "$PROD_SERVICE_ARN" \
  --region $REGION --query 'Service.ServiceUrl' --output text

aws apprunner describe-service --service-arn "$STAGING_SERVICE_ARN" \
  --region $REGION --query 'Service.ServiceUrl' --output text

# Service status
aws apprunner describe-service --service-arn "$PROD_SERVICE_ARN" \
  --region $REGION --query 'Service.Status' --output text

aws apprunner describe-service --service-arn "$STAGING_SERVICE_ARN" \
  --region $REGION --query 'Service.Status' --output text

# Auto-scaling configuration ARNs
aws apprunner list-auto-scaling-configurations --region $REGION \
  --query 'AutoScalingConfigurationSummaryList[?starts_with(AutoScalingConfigurationName, `signalsd`)].[AutoScalingConfigurationName,AutoScalingConfigurationArn]' \
  --output table

# VPC connector ARN
aws apprunner list-vpc-connectors --region $REGION \
  --query "VpcConnectors[?VpcConnectorName=='signalsd'].VpcConnectorArn" --output text
```

---

## VPC and Networking

```bash
VPC_ID=$(aws ec2 describe-vpcs --region $REGION \
  --filters Name=tag:Project,Values=signalsd \
  --query 'Vpcs[0].VpcId' --output text)

# Subnets
aws ec2 describe-subnets --region $REGION \
  --filters Name=vpc-id,Values=$VPC_ID \
  --query 'Subnets[*].[Tags[?Key==`Name`].Value|[0],SubnetId,CidrBlock,AvailabilityZone]' \
  --output table

# Security groups
aws ec2 describe-security-groups --region $REGION \
  --filters Name=vpc-id,Values=$VPC_ID \
  --query 'SecurityGroups[*].[GroupName,GroupId]' --output table

# NAT Gateway
aws ec2 describe-nat-gateways --region $REGION \
  --filter Name=tag:Project,Values=signalsd \
  --query 'NatGateways[*].[NatGatewayId,State]' --output table

# Internet Gateway
aws ec2 describe-internet-gateways --region $REGION \
  --filters Name=tag:Project,Values=signalsd \
  --query 'InternetGateways[0].InternetGatewayId' --output text
```

---

## RDS

```bash
# Endpoint
aws rds describe-db-instances \
  --db-instance-identifier signalsd \
  --region $REGION \
  --query 'DBInstances[0].Endpoint.Address' --output text

# Status and instance class
aws rds describe-db-instances \
  --db-instance-identifier signalsd \
  --region $REGION \
  --query 'DBInstances[0].[DBInstanceStatus,DBInstanceClass,EngineVersion,AllocatedStorage]' \
  --output table
```

---

## Secrets Manager

```bash
# List all signalsd secrets
aws secretsmanager list-secrets --region $REGION \
  --filters Key=name,Values=signalsd \
  --query 'SecretList[*].[Name,ARN]' --output table

# Retrieve a secret value (replace <name> with the secret name)
aws secretsmanager get-secret-value \
  --secret-id "signalsd/staging/database-url" \
  --region $REGION --query SecretString --output text

# Retrieve individual secret ARNs
aws secretsmanager describe-secret \
  --secret-id "signalsd/staging/database-url" \
  --region $REGION --query ARN --output text

aws secretsmanager describe-secret \
  --secret-id "signalsd/prod/database-url" \
  --region $REGION --query ARN --output text
```

---

## SSM Parameter Store

```bash
# List all signalsd parameters with values
aws ssm get-parameters-by-path \
  --path "/signalsd" \
  --recursive \
  --region $REGION \
  --query 'Parameters[*].[Name,Value]' --output table
```

---

## All tagged resources

```bash
# Every resource tagged Project=signalsd
aws resourcegroupstaggingapi get-resources \
  --tag-filters Key=Project,Values=signalsd \
  --region $REGION \
  --query 'ResourceTagMappingList[*].ResourceARN' \
  --output table
```
