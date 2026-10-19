# Wizard templates available to the organization, as raw JSON.
data "varnish_cdn_wizard_templates" "all" {}

# The JSON is a list of wizards, each carrying its answer templates:
#   [
#     {
#       "id": 3,                       # wizard id
#       "name": "Web API",
#       "answerTemplates": [
#         { "id": 21, "name": "default", "description": "...", "icon": "..." }
#       ]
#     }
#   ]
#
# varnish_cdn_service.wizard_answer_template_id takes answerTemplates[].id (21 above),
# not the wizard id.
locals {
  wizards = jsondecode(data.varnish_cdn_wizard_templates.all.json)

  answer_templates = flatten([
    for w in local.wizards : [
      for t in w.answerTemplates : {
        wizard = w.name
        name   = t.name
        id     = t.id
      }
    ]
  ])

  # Latest wins, matching what the provider picks when the attribute is omitted.
  web_api_template_id = max([for t in local.answer_templates : t.id if t.wizard == "Web API"]...)
}

output "answer_templates" {
  value = local.answer_templates
}

resource "varnish_cdn_service" "web" {
  # Omit wizard_answer_template_id to use the latest available template.
  wizard_answer_template_id = local.web_api_template_id

  domain {
    fqdn = "www.example.com"
    tls  = true
  }

  origin {
    name       = "default"
    host       = "origin.example.com"
    port       = 443
    enable_tls = true
    is_default = true
  }
}
