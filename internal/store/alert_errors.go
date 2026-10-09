package store

import "errors"

// ErrAlertAttemptUnavailable means the incident or attempt is no longer eligible
// for the requested transition, rather than a storage failure.
var ErrAlertAttemptUnavailable = errors.New("notification is not eligible for retry or dispatch")
