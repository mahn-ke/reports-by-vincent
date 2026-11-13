<#
.SYNOPSIS
One-time migration of the reports database from PostgreSQL 17 to PostgreSQL 18.

.DESCRIPTION
PostgreSQL major versions cannot open each other's data directories, and the
postgres:18 image expects a single mount at /var/lib/postgresql. This script
dumps the PostgreSQL 17 volume and restores it into the new
<project>_postgres18_data volume used by docker-compose.yml.

Run it on the Docker host directly BEFORE the deployment that switches
docker-compose.yml to postgres:18. It stops the database and its writers
(api, jobs, app); the deployment starts them again on PostgreSQL 18.

Data never leaves Docker volumes. The PostgreSQL 17 volume is not modified,
so rollback is: revert docker-compose.yml and redeploy.

.EXAMPLE
.\scripts\Upgrade-Postgres18.ps1
#>
param(
    [string]$Project = 'reports',
    [string]$DatabaseUser = 'reports'
)

$ErrorActionPreference = 'Stop'

$oldVolume = "${Project}_postgres_data"
$newVolume = "${Project}_postgres18_data"
$dumpVolume = "${Project}_postgres18_migration"
$dumpContainer = "${Project}-pg17-dump"
$restoreContainer = "${Project}-pg18-restore"
$stackContainers = @('api', 'jobs', 'app', 'db') | ForEach-Object { "${Project}-$_-1" }

# Row count of every base table, one "schema.table=count" line each, for before/after comparison
$countSql = "select table_schema || '.' || table_name || '=' || (xpath('/row/c/text()', query_to_xml(format('select count(*) as c from %I.%I', table_schema, table_name), false, true, '')))[1]::text from information_schema.tables where table_type = 'BASE TABLE' and table_schema not in ('pg_catalog', 'information_schema') order by 1"

function Invoke-Docker {
    & docker @args
    if ($LASTEXITCODE -ne 0) { throw "docker $($args -join ' ') failed with exit code $LASTEXITCODE" }
}

function Test-DockerObject([string]$Kind, [string]$Name) {
    $previous = $ErrorActionPreference
    $ErrorActionPreference = 'Continue'
    & docker $Kind inspect $Name *> $null
    $exists = $LASTEXITCODE -eq 0
    $ErrorActionPreference = $previous
    return $exists
}

function Wait-Postgres([string]$Container) {
    # TCP readiness: the image's first-run initialization server listens on the socket only
    for ($i = 0; $i -lt 90; $i++) {
        & docker exec $Container pg_isready -q -h 127.0.0.1 -U $DatabaseUser
        if ($LASTEXITCODE -eq 0) { return }
        Start-Sleep -Seconds 2
    }
    throw "$Container did not become ready"
}

function Get-RowCounts([string]$Container) {
    $counts = Invoke-Docker exec $Container psql -U $DatabaseUser -d $Project -tA -c $countSql
    return ($counts | Where-Object { $_ }) -join "`n"
}

function Remove-TemporaryContainers {
    foreach ($name in @($dumpContainer, $restoreContainer)) {
        if (Test-DockerObject container $name) { & docker rm -f $name | Out-Null }
    }
}

if (-not (Test-DockerObject volume $oldVolume)) { throw "PostgreSQL 17 volume $oldVolume does not exist" }
if (Test-DockerObject volume $newVolume) { throw "$newVolume already exists; migration has already run or must be cleaned up first" }
foreach ($name in @($dumpContainer, $restoreContainer)) {
    if (Test-DockerObject container $name) { throw "Temporary container $name already exists" }
}

$stopped = @($stackContainers | Where-Object { Test-DockerObject container $_ })
# Start in reverse stop order: the database before its clients
$startOrder = @($stopped)
[array]::Reverse($startOrder)
try {
    Write-Host "Stopping $($stopped -join ', ')"
    foreach ($name in $stopped) { Invoke-Docker stop $name | Out-Null }

    Write-Host 'Dumping PostgreSQL 17'
    Invoke-Docker run -d --name $dumpContainer -v "${oldVolume}:/var/lib/postgresql/data" -v "${dumpVolume}:/migration" postgres:17 | Out-Null
    Wait-Postgres $dumpContainer
    $before = Get-RowCounts $dumpContainer
    Invoke-Docker exec $dumpContainer pg_dumpall -U $DatabaseUser -f /migration/dumpall.sql
    Invoke-Docker rm -f $dumpContainer | Out-Null

    Write-Host 'Restoring into PostgreSQL 18'
    # The bootstrap password is replaced by the role's original password hash from the dump
    $bootstrapPassword = [guid]::NewGuid().ToString('N')
    Invoke-Docker run -d --name $restoreContainer -e "POSTGRES_USER=$DatabaseUser" -e "POSTGRES_PASSWORD=$bootstrapPassword" -e POSTGRES_DB=postgres -v "${newVolume}:/var/lib/postgresql" -v "${dumpVolume}:/migration" postgres:18 | Out-Null
    Wait-Postgres $restoreContainer
    Invoke-Docker exec $restoreContainer sh -c "psql -U $DatabaseUser -d postgres -q -f /migration/dumpall.sql > /dev/null 2> /migration/restore.err"
    # The bootstrap role necessarily exists already; any other error aborts the migration
    # (no embedded double quotes: Windows PowerShell 5.1 does not escape them for native commands)
    $unexpected = Invoke-Docker exec $restoreContainer sh -c "grep 'ERROR:' /migration/restore.err | grep -v 'role .$DatabaseUser. already exists' || true"
    if ($unexpected) { throw "Restore reported errors:`n$($unexpected -join "`n")" }
    Invoke-Docker exec $restoreContainer vacuumdb -U $DatabaseUser --all --analyze-only --quiet

    $after = Get-RowCounts $restoreContainer
    if ($before -ne $after) { throw "Row counts differ after restore.`nBefore:`n$before`nAfter:`n$after" }
    Write-Host "Row counts match:`n$before"

    Invoke-Docker rm -f $restoreContainer | Out-Null
    # The dump holds a full copy of the data; do not leave it lying around
    Invoke-Docker volume rm $dumpVolume | Out-Null
}
catch {
    Write-Warning "Migration failed; restoring the PostgreSQL 17 stack: $_"
    Remove-TemporaryContainers
    foreach ($volume in @($newVolume, $dumpVolume)) {
        if (Test-DockerObject volume $volume) { & docker volume rm $volume | Out-Null }
    }
    foreach ($name in $startOrder) { & docker start $name | Out-Null }
    throw
}

Write-Host "Migration complete. $newVolume is ready; deploy the postgres:18 docker-compose.yml now."
Write-Host "Until then, $($stopped -join ', ') stay stopped. To abort instead: docker volume rm $newVolume; docker start $($startOrder -join ' ')"
