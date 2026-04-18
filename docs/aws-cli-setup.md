# AWS CLI Setup

Instructions for accessing the AWS CLI in preparation for running the steps in
[aws-setup.md](aws-setup.md).

---

## Option A — AWS CloudShell (recommended for setup)

CloudShell runs directly in your browser using your existing console session — the equivalent
of running `gcloud` after `gcloud auth login`. No installation or local configuration needed.

1. Log in to the AWS console as normal
2. Click the CloudShell icon in the top toolbar (terminal icon, to the right of the search bar)
   or search for "CloudShell" in the console
3. A terminal opens in the browser, already authenticated as your account identity

Verify:

```bash
aws sts get-caller-identity
```

`sts` (Security Token Service) is the AWS service that manages temporary credentials — this
command is the AWS equivalent of `gcloud auth list`. A successful response confirms you are
authenticated.

You can run every command in [aws-setup.md](aws-setup.md) directly in this terminal. No
further setup needed.

---

## Option B — Local AWS CLI with IAM Identity Center (SSO)

Use this if you prefer a local terminal, or if you need ongoing CLI access beyond the
one-time setup.

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

**Linux (x86_64)**

```bash
curl "https://awscli.amazonaws.com/awscli-exe-linux-x86_64.zip" -o /tmp/awscliv2.zip
unzip /tmp/awscliv2.zip -d /tmp
sudo /tmp/aws/install
```

**Windows**

Download and run the MSI installer from:
`https://awscli.amazonaws.com/AWSCLIV2.msi`

Verify:

```bash
aws --version
# aws-cli/2.x.x Python/3.x.x ...
```

### Find your SSO start URL

The SSO start URL is the address you visit *before* reaching the AWS console — the
`awsapps.com` login page. Check:

- Any AWS invitation email you received (subject: "Invitation to join AWS IAM Identity
  Center") — the URL is in the body
- Your browser bookmarks or history for `awsapps.com`
- Your AWS administrator, who can find it under IAM Identity Center → Settings →
  "AWS access portal URL"

The URL will be in one of these formats:
- `https://<name>.awsapps.com/start`
- `https://d-xxxxxxxxxx.awsapps.com/start`

### Configure a profile

Run this once:

```bash
aws configure sso
```

When prompted:

```
SSO session name:         signalsd
SSO start URL:            https://<your-org>.awsapps.com/start
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

### Log in at the start of each session

SSO sessions expire (typically after 8 hours):

```bash
aws sso login --profile signalsd
```

### Activate the profile

**Do this immediately after logging in**, before running any other commands. It applies the
profile to every subsequent command in the current shell session:

```bash
export AWS_PROFILE=signalsd
```

This is roughly equivalent to `gcloud config set project` + `gcloud config set account`
combined — an AWS profile bundles account, region, and credentials into a single named
configuration. The difference is that `gcloud` remembers the active project across shell
sessions, while `AWS_PROFILE` must be exported in each new terminal.

If you skip this and run commands without `--profile`, the CLI falls back to the default
profile which has no credentials and returns a `NoCredentials` error.

---

## Verify You Are in the Right Account

Before running any setup steps, confirm your identity and account:

```bash
aws sts get-caller-identity
```

Expected output shape:

```json
{
    "UserId": "AROAXXXXXXXXXXXXXXXXX:your@email.com",
    "Account": "123456789012",
    "Arn": "arn:aws:sts::123456789012:assumed-role/AWSReservedSSO_AdministratorAccess_xxxx/your@email.com"
}
```

The `Account` field is your AWS account ID. Confirm it matches the account you intend to use.

### Quick permissions spot-check

These commands should return empty lists (not errors) on a fresh account:

```bash
# IAM is a global service — no --region needed
aws iam list-open-id-connect-providers

# ECR and App Runner are regional — --region is required
aws ecr describe-repositories --region eu-west-1
aws apprunner list-services --region eu-west-1
```

If any return `AccessDenied`, check with your AWS administrator before proceeding.

> **Why some commands need `--region` and others don't:** AWS services are either global
> (IAM, Route53, CloudFront) or regional (ECR, App Runner, RDS, most everything else).
> Global services don't need a region flag. Regional service commands always need `--region`
> unless a default is set in your profile.
