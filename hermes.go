package hermes

import "fmt"

const Version = "0.0.0"

func UserAgent() string {
	return fmt.Sprintf("Hermes/%s (Go; +https://github.com/gargalloeric/hermes)", Version)
}
