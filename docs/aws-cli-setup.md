# AWS CLI Setup

### Install the AWS CLI (v2)

**macOS**

```bash
curl "https://awscli.amazonaws.com/AWSCLIV2.pkg" -o /tmp/AWSCLIV2.pkg
sudo installer -pkg /tmp/AWSCLIV2.pkg -target /
```

Or via Homebrew:

```bash
brew install awscli
```

### Configure a profile

Run this once:

```bash
aws configure sso
```

When prompted:

```
SSO session name:         signalsd
SSO start URL:            https://tu-aws.awsapps.com/start/
SSO region:               eu-west-1
SSO registration scopes:  sso:account:access
```

A browser window will open. Log in, select your AWS account, and select the
`AdministratorAccess` permission set. The CLI will then ask:

```
CLI default client Region:  eu-west-1
CLI default output format:  json
CLI profile name:           signalsd
```

### Log in 

```bash
aws sso login --profile signalsd
```

### env
set this var if you don't want to keep specifying `--profile signalsd`

```bash
export AWS_PROFILE=signalsd
```

If you skip this and run commands without `--profile`, the CLI falls back to the default
profile which has no credentials and returns a `NoCredentials` error.

---

## Verify You Are in the Right Account

```bash
aws sts get-caller-identity
```

### Quick permissions spot-check

These commands should return empty lists (not errors) on a fresh account:

```bash
# IAM is a global service — no --region needed
aws iam list-open-id-connect-providers

# ECR and App Runner
aws ecr describe-repositories --region eu-west-1
aws apprunner list-services --region eu-west-1
```