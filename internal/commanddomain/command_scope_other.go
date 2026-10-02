//go:build !linux

package commanddomain

func initializeCommandScope(*commandScopeState, Config) error { return ErrUnavailable }
func checkCommandScopeLive(*commandScopeState) error          { return ErrUnavailable }
