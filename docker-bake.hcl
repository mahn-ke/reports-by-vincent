group "default" {
  targets = ["nginx"]
}

target "nginx" {
  context = "nginx"
  tags = ["ghcr.io/mahn-ke/reports-by-vincent/nginx:latest"]
  platforms = ["linux/amd64"]
  labels = {
    "org.opencontainers.image.source" = "https://github.com/mahn-ke/reports-by-vincent"
  }
}
