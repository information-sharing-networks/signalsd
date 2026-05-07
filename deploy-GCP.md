
### 1. Create Google Cloud Resources
- Create a project called `signalsd`
- Create an artifact registry called `signalsd`

### 2. Create Service Accounts (IAM > Service Accounts)
- `cloud-run-deploy`
- `cloud-run-runtime`

### 3. Configure cloud-run-deploy Account
The `cloud-run-deploy` account will:
- Build an image each time there is a push on main
- Push the image to the artifact registry (the image is tagged with the commit id and 'latest')
- Create a container based on the latest image and deploy to Cloud Run (the container will run under the cloud-run-runtime account)

### 4. Set Service Account Permissions
- `cloud-run-deploy` needs the **Artifact Registry Writer** and **Cloud Run Admin** roles
- The `cloud-run-runtime` account does not need access to any of the Google APIs and therefore doesn't have any roles. You do however need to configure it to allow the cloud-run-deploy account to use it:

  **IAM > Service Accounts > cloud-run-runtime account > manage details > Principals with access > grant access**

  Add the cloud-run-deploy account email address - give it the **Service Account User** role.

### 5. Download Service Account Key
Download a JSON key for the cloud-run-deploy account:
**IAM > service accounts > cloud-run-deploy account > keys > add key > Create New**

**Note** - this is no longer recommended.  Use github OICD authentication to avoid storing the GCP credentials as github Secrets.

### 6. Configure GitHub Secrets and Variables
The deployment workflows (`.github/workflows`) are configured to use GitHub variables for non-sensitive configuration and GitHub secrets for sensitive configuration.


#### Required GitHub Secrets
Set up GitHub secrets in your fork of the repo:
**repo > settings > secrets and variables > actions > secrets tab > new repository secret**

You will need three secrets:
- `GCP_CREDENTIALS` (upload the contents of the JSON key downloaded earlier)
- `DATABASE_URL` (URL of your postgres service - we are using Neon.tech, but any provider that supports current postgres versions should work)
- `SECRET_KEY` (random secret key for your app - used by the signalsd server to sign JWT tokens)

if you are deploying to a staging environment you need to create a separate database and two additional secrets:
- `STAGING_DATABASE_URL`
- `STAGING_SECRET_KEY`

You need to create two github environment variables: 
- `PUBLIC_BASE_URL` (e.g. https://yourdomain.com) to hold the public base url for your app
- `ALLOWED_ORIGINS` (e.g. https://yourdomain.com) to hold a pipe-separated list of allowed CORS origins

If you are deploying to a staging environment you need to create separate variable to hold these values:
- `STAGING_PUBLIC_BASE_URL` (e.g. https://staging.yourdomain.com)
- `STAGING_ALLOWED_ORIGINS` (e.g. https://staging.yourdomain.com)

The public base URL is used by the back-end server when generating password reset and service account setup links.

#### DNS
google will automatically assign a *.run.app domain name to your Gcloud run service(s) and these can be mapped to custom domains using Google Cloud run DNS (see https://cloud.google.com/run/docs/mapping-custom-domains)

#### Optional GitHub Variables 
For production tuning, you can set GitHub variables:
**repo > settings > secrets and variables > actions > variables tab > new repository variable**

if you don't set these variables, app defautls are used.

Recommended production variables:
```
# Database Pool Configuration
DB_MAX_CONNECTIONS=25
DB_MIN_CONNECTIONS=0
DB_MAX_CONN_LIFETIME=120m
DB_MAX_CONN_IDLE_TIME=20m
DB_CONNECT_TIMEOUT=10s

# Performance Configuration
RATE_LIMIT_RPS=2000
RATE_LIMIT_BURST=5000

# Server Configuration
READ_TIMEOUT=30s
WRITE_TIMEOUT=30s
IDLE_TIMEOUT=120s
```
