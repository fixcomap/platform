# tflint se ejecuta por capa desde pr-checks.yml. El ruleset google necesita la versión fijada
# porque tflint no resuelve rangos y el plugin se descarga en cada run de CI.
plugin "terraform" {
  enabled = true
  preset  = "recommended"
}

plugin "google" {
  enabled = true
  version = "0.39.0"
  source  = "github.com/terraform-linters/tflint-ruleset-google"
}
