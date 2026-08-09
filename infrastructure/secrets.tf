resource "random_password" "admin_password" {
  length           = 24
  special          = true
  override_special = "!#%&*-_=+"
}

resource "github_actions_secret" "admin_username" {
  repository      = "reports-by-vincent"
  secret_name     = "REPORTS_ADMIN_USERNAME"
  plaintext_value = "admin"
}

resource "github_actions_secret" "admin_password" {
  repository      = "reports-by-vincent"
  secret_name     = "REPORTS_ADMIN_PASSWORD"
  plaintext_value = random_password.admin_password.result
}
