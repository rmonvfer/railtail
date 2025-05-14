// Package config provides configuration loading and parsing for railtail.
package config

import (
	"os"
	"strings"
)

// checkForFlag checks if a flag is present in command line arguments.
// It supports both short (-h) and long (--help) flag formats, as well as
// flags with values (--help=true or --help true).
func checkForFlag(flagName string) bool {
	// Safety check for empty flag name
	if flagName == "" {
		return false
	}
	
	helpFlags := []string{
		"-" + flagName,
		"--" + flagName,
	}
	
	// Only add short flag if flagName is long enough
	if len(flagName) > 0 {
		helpFlags = append(helpFlags, 
			"-"+flagName[:1],
			"--"+flagName[:1],
		)
	}

	for _, arg := range os.Args[1:] {
		// Direct match
		for _, helpFlag := range helpFlags {
			if arg == helpFlag {
				return true
			}
		}

		// Check for flags with attached values
		for _, helpFlag := range helpFlags {
			if strings.HasPrefix(arg, helpFlag+"=") {
				return true
			}
		}

		// Check for flags with spaces between flag and value
		// If we're not at the last argument
		if len(os.Args) > 2 {
			for i, arg := range os.Args[1 : len(os.Args)-1] {
				for _, helpFlag := range helpFlags {
					if arg == helpFlag && !strings.HasPrefix(os.Args[i+2], "-") {
						return true
					}
				}
			}
		}
	}

	return false
}