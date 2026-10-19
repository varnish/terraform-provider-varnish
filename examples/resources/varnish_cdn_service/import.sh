# Services are imported by their service ID.
terraform import varnish_cdn_service.web 42

# Older services may have no wizard_answer_template_id. Setting it after import
# does not recreate the service.
