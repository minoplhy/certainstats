package lifecycle

// ShutdownChan is a generic channel used to request clean application shutdowns
// with a specific exit code (e.g. for supervisor restarts).
var ShutdownChan = make(chan int, 1)

// TriggerRestart sends an exit code to the ShutdownChan to trigger a graceful supervisor restart.
func TriggerRestart(exitCode int) {
	select {
	case ShutdownChan <- exitCode:
	default:
		// Channel already has a pending shutdown request
	}
}
