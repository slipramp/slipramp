variable "KARGO_ONLY" {
  default = false
}

target "acceptance" {
  matrix = {
    image = concat(
      ["acceptance-git", "checkout", "controller", "harness", "kargo-tools", "server", "web"],
      KARGO_ONLY ? [] : ["acceptance-ssh", "ansible-tools"]
    )
  }
  name       = image
  context    = "."
  target     = image
  tags       = ["slipramp-${image}:acceptance"]
  cache-from = ["type=gha,scope=acceptance-${image}"]

  # The kind job builds every image and is the sole writer for these scopes.
  cache-to = KARGO_ONLY ? [] : ["type=gha,scope=acceptance-${image},mode=max,ignore-error=true"]
}
