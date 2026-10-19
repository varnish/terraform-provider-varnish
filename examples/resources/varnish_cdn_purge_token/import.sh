# Purge tokens are imported by their token ID.
# An imported token has no access_token. Setting "expire" in config replaces
# the token on the next apply.
terraform import varnish_cdn_purge_token.ci 12
