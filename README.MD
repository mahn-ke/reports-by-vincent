# reports.by.vincent.mahn.ke

Hosts the container serving [reports.by.vincent.mahn.ke](https://reports.by.vincent.mahn.ke).

## Container configuration

The shared deployment workflow builds `docker-bake.hcl` and publishes
`ghcr.io/mahn-ke/reports-by-vincent/nginx:latest` before running Compose.
The image contains `nginx/nginx.conf`, so restarting NGINX does not require the
runner checkout. Database data remains in its named volume.
Deploy once to replace the old bind-mounted container. For local image builds,
run `docker buildx bake --file docker-bake.hcl --load`.

## Local Template Iteration

The app normally uses Go embedded templates, which are compiled into the binary and require a rebuild after HTML changes.

For local development, use the compose override file to mount templates and switch runtime loading to disk:

```sh
docker compose -f docker-compose.yml -f docker-compose.local.yml up -d app nginx
```

With this override active, edits in `app/templates/*.html` are picked up on refresh without rebuilding the app image.
## PostgreSQL 18 upgrade

PostgreSQL 18 cannot open the PostgreSQL 17 data directory, and its image
expects a single mount at `/var/lib/postgresql`. The database therefore moves
to the new `postgres18_data` volume. On the Docker host, run
`.\scripts\Upgrade-Postgres18.ps1` from the checkout directly before deploying
the `postgres:18` configuration. It stops the database and its writers, dumps
PostgreSQL 17, restores into `reports_postgres18_data`, and verifies row counts.
The old `reports_postgres_data` volume is left unchanged for rollback.
