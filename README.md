# pipery-deploy-bot

HTTP service for scheduling GitHub Actions `workflow_dispatch` deploys through a GitHub App installation token.

## Environment

| Variable | Required | Description |
| --- | --- | --- |
| `DATABASE_URL` | yes | Postgres connection string. |
| `PIPERY_DEPLOY_CONFIG` | yes | JSON config file containing GitHub App installations. |
| `LISTEN_ADDR` | no | HTTP address, default `:8080`. |
| `SCHEDULER_INTERVAL` | no | Scheduler poll interval, default `30s`. |
| `PIPERY_DEPLOY_API_TOKEN` | no | If set, `/v1` APIs and dashboard require `Authorization: Bearer <token>`. |

Config file:

```json
{
  "installations": {
    "default": {
      "app_id": 12345,
      "installation_id": 67890,
      "private_key_file": "/run/secrets/github-app.pem"
    }
  }
}
```

## Helm

Install the chart with existing secrets for Postgres, API token, and GitHub App private key:

```sh
helm upgrade --install pipery-deploy-bot ./charts/pipery-deploy-bot \
  --namespace pipery \
  --create-namespace \
  --set database.existingSecret=pipery-deploy-bot-database \
  --set privateKey.existingSecret=pipery-deploy-bot-private-key \
  --set apiToken.existingSecret=pipery-deploy-bot-api-token
```

The chart can run the Postgres migration as a Helm pre-install/pre-upgrade hook. Set `migrations.enabled=false` if migrations are managed elsewhere.

## GitHub Actions

The repository includes:

- `.github/workflows/ci.yml` using `pipery-dev/pipery-golang-ci@v1`
- `.github/workflows/deploy.yml` using `pipery-dev/pipery-helm-cd@v1`

Set `KUBECONFIG_B64` as a repository or environment secret for the deploy workflow.

On every push to `main` or `v*` tag, the CI workflow uses `pipery-dev/pipery-argocd-cd` to publish an ArgoCD Application and values override to `pipery-dev/pipery-argocd` under `applications/pipery-deploy-bot/`. Set `PIPERY_ARGOCD_TOKEN` to a token that can write to that private repository.

Run migrations before starting the service:

```sh
psql "$DATABASE_URL" -f migrations/001_init.sql
```

## API examples

```sh
curl http://localhost:8080/healthz
```

```sh
curl -X POST http://localhost:8080/v1/scheduled-deploys \
  -H 'Content-Type: application/json' \
  -d '{
    "idempotency_key": "prod-v1.2.3",
    "installation_key": "default",
    "owner": "pipery-dev",
    "repo": "example",
    "workflow_id": "deploy.yml",
    "ref": "main",
    "scheduled_at": "2026-05-17T12:00:00Z",
    "inputs": {"environment": "production", "version": "v1.2.3"}
  }'
```

```sh
curl 'http://localhost:8080/v1/scheduled-deploys?status=pending'
curl 'http://localhost:8080/v1/trigger-attempts?deploy_id=<id>'
```

`scheduled_at` must be RFC3339 and is stored/processed in UTC. Duplicate `idempotency_key` values return the existing scheduled deploy.

Open `/dashboard` to view scheduled deploys and trigger attempts in a simple HTML dashboard.

## GitHub Actions helper

This repository also ships a composite action that schedules a one-time deploy through the bot API:

```yaml
name: Schedule production deploy

on:
  workflow_dispatch:
    inputs:
      deploy_at:
        required: true
        description: UTC RFC3339 timestamp, for example 2026-05-17T12:00:00Z
      version:
        required: true

jobs:
  schedule:
    runs-on: ubuntu-latest
    steps:
      - uses: pipery-dev/pipery-deploy-bot@v1
        with:
          api-url: ${{ secrets.PIPERY_DEPLOY_BOT_URL }}
          api-token: ${{ secrets.PIPERY_DEPLOY_BOT_TOKEN }}
          idempotency-key: production-${{ inputs.version }}-${{ inputs.deploy_at }}
          workflow-id: deploy.yml
          scheduled-at: ${{ inputs.deploy_at }}
          inputs-json: '{"environment":"production","version":"${{ inputs.version }}"}'
```

The action defaults `repository` to `${{ github.repository }}` and `ref` to `${{ github.ref_name }}`. Use `repository`, or explicit `owner` and `repo`, only when scheduling a deploy for another repository.
