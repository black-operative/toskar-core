package automations

// SetHomeDir points the home folder at home for a test.
func SetHomeDir(home string) func() {
	saved := homeDir
	homeDir = func() (string, error) { return home, nil }
	return func() { homeDir = saved }
}
