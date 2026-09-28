package model

// LogOtherFromLegacyMap adapts downstream log producers to the audience-scoped
// metadata contract. Historical sensitive fields remain administrator-only.
func LogOtherFromLegacyMap(values map[string]any) *LogOther {
	other := NewLogOther()
	for key, value := range values {
		switch key {
		case logOtherAdminInfoKey:
			if fields, ok := value.(map[string]any); ok {
				other.MergeAdmin(fields)
			}
		case logOtherRootInfoKey:
			if fields, ok := value.(map[string]any); ok {
				other.MergeRoot(fields)
			}
		case logOtherAuditInfoKey:
			if fields, ok := value.(map[string]any); ok {
				other.MergeAudit(fields)
			}
		default:
			if !other.SetPublic(key, value) {
				other.SetAdmin(key, value)
			}
		}
	}
	return other
}
