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