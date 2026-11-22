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

`/workout/calendar` labels each training day Push, Pull, Legs, Full Body or Upper, trying these in order:

1. Match the day's logged exercises against the planned exercises of your wger routine days (Jaccard overlap ≥ 50%), and use that day's name. Lower counts as Legs.
2. Otherwise go by the exercises' muscles. A group with ≥ 60% of the sets decides the day. A spread over several groups is Full Body, or Upper if legs aren't among them. Rear-delt work and pullovers count as Pull.
3. Otherwise use the routine day that wger attached the session to. This comes last because wger assigns it from the schedule, not from what was trained.

The type is only a label. Overload is scored the same way as the suggestions in the wger app
([ViMaSter/wger-flutter](https://github.com/ViMaSter/wger-flutter), `lib/features/routines/progressive_overload.dart`):

- Each exercise is compared with **its own previous session**.
- The baseline is the **first N sets** of that session, where N is the number of sets planned for the exercise today (from the routine's sets config at that visit). Extra sets done last time don't raise the bar.
- A set's volume is kg × reps, or the reps for sets without weight. Overload means strictly more volume.

Each exercise counts towards each of its primary muscles in wger, or its category if wger lists none. A muscle group is overloaded when its volume beats its baseline. The day is:

- **green** when every group was overloaded
- **yellow** when at least half were, or when every group repeated last time exactly
- **red** when fewer than half were
