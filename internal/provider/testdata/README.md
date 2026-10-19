# testdata

## wizard.json

An unmodified copy of the latest released Varnish CDN wizard version, currently
v25.

The provider supports exactly one wizard shape, so only the latest version is kept.
`TestSettingsSchemaCoversWizardVersion` asserts that the settings schema in
`cdn_wizard_settings_schema.go` and the `wizardDefaultSettings` fixture describe
the same fields, field types and select options as this file.

### Refreshing

When a new wizard version is released:

1. Replace this file with the new version's definition.
2. Run `go test ./internal/provider -run TestSettingsSchemaCoversWizardVersion`.
3. Until it passes, update the schema table and `kindForWizardType` in
   `cdn_wizard_settings_schema.go` and `cdn_wizard_settings_test.go`, and the
   `wizardDefaultSettings` fixture.
4. Update the version named above.
