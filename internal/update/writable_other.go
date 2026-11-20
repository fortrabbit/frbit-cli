//go:build !unix

package update

// writable cannot check without unix; Windows gets the docs link either way.
func writable(string) bool { return true }
