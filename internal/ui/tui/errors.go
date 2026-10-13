package tui

import (
	"errors"
	"fmt"
	"os"
	"strings"
)

func actionError(action string, err error) string {
	reason := err.Error()
	if errors.Is(err, os.ErrPermission) {
		reason = "permission denied"
	} else {
		var pathErr *os.PathError
		if errors.As(err, &pathErr) {
			reason = pathErr.Err.Error()
		}
	}
	reason = strings.Join(strings.Fields(reason), " ")
	const maxReasonLength = 80
	if len(reason) > maxReasonLength {
		reason = reason[:maxReasonLength-3] + "..."
	}
	return fmt.Sprintf("[red]Failed to %s: %s[-]", action, reason)
}
