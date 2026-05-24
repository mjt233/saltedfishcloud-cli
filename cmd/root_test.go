package cmd

import "testing"

func TestNewRootCommand_RegistersGlobalFlags(t *testing.T) {
	root := NewRootCommand()
	for _, flagName := range []string{"api-ticket", "service-url"} {
		if root.PersistentFlags().Lookup(flagName) == nil {
			t.Fatalf("missing persistent flag %q", flagName)
		}
	}
}
