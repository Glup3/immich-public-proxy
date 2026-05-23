package sanitize

import (
	"regexp"
	"strings"
)

var (
	illegalRe         = regexp.MustCompile(`[/?<>\\:*|"]`)
	controlRe         = regexp.MustCompile(`[\x00-\x1f\x80-\x9f]`)
	reservedRe        = regexp.MustCompile(`^\.+$`)
	windowsReservedRe = regexp.MustCompile(`(?i)^(con|prn|aux|nul|com[0-9]|lpt[0-9])(\..*)?$`)
	windowsTrailingRe = regexp.MustCompile(`[. ]+$`)
)

func Filename(input string) string {
	output := illegalRe.ReplaceAllString(input, "")
	output = controlRe.ReplaceAllString(output, "")
	output = reservedRe.ReplaceAllString(output, "")
	output = windowsReservedRe.ReplaceAllString(output, "")
	output = windowsTrailingRe.ReplaceAllString(output, "")
	if len(output) > 254 {
		output = output[:254]
	}
	return strings.TrimSpace(output)
}
