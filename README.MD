# reports.by.vincent.mahn.ke

Hosts the container serving [reports.by.vincent.mahn.ke](https://reports.by.vincent.mahn.ke).

## Container configuration

The shared deployment workflow builds `docker-bake.hcl` and publishes
`ghcr.io/mahn-ke/reports-by-vincent/{nginx,app,api,jobs,garmin}:latest` before
running Compose, which pulls them (`--pull always`). Compose never rebuilds
`build:` services on deploy, so every service with code must be a bake target.
The nginx image contains `nginx/nginx.conf`, so restarting NGINX does not require
the runner checkout. Database data remains in its named volume.
Deploy once to replace the old bind-mounted container. For local image builds,
run `docker buildx bake --file docker-bake.hcl --load`.

## Local Template Iteration

The app normally uses Go embedded templates, which are compiled into the binary and require a rebuild after HTML changes.

For local development, use the compose override file to mount templates and switch runtime loading to disk:

```sh
docker compose -f docker-compose.yml -f docker-compose.local.yml up -d app nginx
```

With this override active, edits in `app/templates/*.html` are picked up on refresh without rebuilding the app image.

## wger workouts and calendar

The `jobs` service syncs workout logs from wger ([fitness.by.vincent.mahn.ke](https://fitness.by.vincent.mahn.ke))
through wger's own OAuth2/OIDC provider. wger logs in through Keycloak, so connecting takes one click:
open `/workout/calendar` and choose **Connect wger**. The app stores the token through the API's internal
routes, and the worker refreshes it and keeps the rotated refresh token. "Resync" on the calendar page queues
a sync right away (`POST /trigger/wger` on the `jobs` service, internal network only).

The OAuth client (`reports-by-vincent`) is created by Terraform in `mahn-ke/repos`
(`infrastructure/modules/wger_oauth`). Terraform sets this repository's `WGER_OAUTH_CLIENT_ID` and
`WGER_OAUTH_CLIENT_SECRET` secrets, and fitness-by-vincent registers the client in wger on deploy.

`/workout/calendar` labels each training day push, pull or legs. It uses the wger routine day name if
that contains push/pull/leg, and otherwise the exercises' muscles. The day's volume (Σ kg × reps, or reps
when both days are bodyweight-only) is compared with the previous day of the same type: green = more,
yellow = identical, red = less.
