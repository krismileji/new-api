package middleware

// HeaderNavModuleIsPublic reports whether a module can be reached without a session.
func HeaderNavModuleIsPublic(module string) bool {
	access := getHeaderNavAccess(module)
	return access.Enabled && !access.RequireAuth
}
