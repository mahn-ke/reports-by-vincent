# reports.by.vincent.mahn.ke

Hosts the container serving [reports.by.vincent.mahn.ke](https://reports.by.vincent.mahn.ke).

## Local Template Iteration

The app normally uses Go embedded templates, which are compiled into the binary and require a rebuild after HTML changes.

For local development, use the compose override file to mount templates and switch runtime loading to disk:

```sh
docker compose -f docker-compose.yml -f docker-compose.local.yml up -d app nginx
```

With this override active, edits in `app/templates/*.html` are picked up on refresh without rebuilding the app image.