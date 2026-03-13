package resources

import (
	"fmt"
	"regexp"
)

// Shell-safe patterns for values interpolated into sh -c commands.
var (
	safeInterfaceRe = regexp.MustCompile(`^[a-zA-Z0-9._-]+$`)
	safeSubnetV4Re  = regexp.MustCompile(`^[0-9]{1,3}\.[0-9]{1,3}\.[0-9]{1,3}\.[0-9]{1,3}/[0-9]{1,2}$`)
	safeCommandRe   = regexp.MustCompile(`^[a-zA-Z0-9._/-]+$`)
	safeHostnameRe  = regexp.MustCompile(`^[a-zA-Z0-9._-]+$`)
)

// ValidateNADShellInputs checks that NADInterface, NADSubnet, and Command are
// safe to interpolate into a shell command. This is a defense-in-depth measure
// on top of kubebuilder CRD validation markers.
func ValidateNADShellInputs(nadInterface, nadSubnet, command string) error {
	if nadInterface != "" && !safeInterfaceRe.MatchString(nadInterface) {
		return fmt.Errorf("invalid NAD interface name %q: must match %s", nadInterface, safeInterfaceRe.String())
	}
	if nadSubnet != "" && !safeSubnetV4Re.MatchString(nadSubnet) {
		return fmt.Errorf("invalid NAD subnet %q: must match %s", nadSubnet, safeSubnetV4Re.String())
	}
	if command != "" && !safeCommandRe.MatchString(command) {
		return fmt.Errorf("invalid command %q: must match %s", command, safeCommandRe.String())
	}
	return nil
}

// ValidateHostname checks that a hostname is safe to interpolate into shell
// commands (sed, grep, nslookup). Hostnames must contain only alphanumeric
// characters, hyphens, underscores, and dots.
func ValidateHostname(hostname string) error {
	if !safeHostnameRe.MatchString(hostname) {
		return fmt.Errorf("invalid hostname %q: must match %s", hostname, safeHostnameRe.String())
	}
	return nil
}
