resource "varnish_cdn_certificate" "example" {
  name        = "example-com"
  certificate = file("${path.module}/example.com.crt")
  private_key = file("${path.module}/example.com.key")

  min_version = "TLSv1.2"
}
