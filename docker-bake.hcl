group "default" {
  targets = ["nginx", "app", "api", "jobs", "garmin"]
}

target "_common" {
  platforms = ["linux/amd64"]
  labels = {
    "org.opencontainers.image.source" = "https://github.com/mahn-ke/reports-by-vincent"
  }
}

target "nginx" {
  inherits = ["_common"]
  context  = "nginx"
  tags     = ["ghcr.io/mahn-ke/reports-by-vincent/nginx:latest"]
}

target "app" {
  inherits = ["_common"]
  context  = "app"
  tags     = ["ghcr.io/mahn-ke/reports-by-vincent/app:latest"]
}

target "api" {
  inherits = ["_common"]
  context  = "api"
  tags     = ["ghcr.io/mahn-ke/reports-by-vincent/api:latest"]
}

target "jobs" {
  inherits = ["_common"]
  context  = "jobs"
  tags     = ["ghcr.io/mahn-ke/reports-by-vincent/jobs:latest"]
}

target "garmin" {
  inherits = ["_common"]
  context  = "garmin"
  tags     = ["ghcr.io/mahn-ke/reports-by-vincent/garmin:latest"]
}
