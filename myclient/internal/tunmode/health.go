package tunmode

type healthFailureTracker struct {
	failures int
}

func (tracker *healthFailureTracker) record(err error) bool {
	if err == nil {
		tracker.failures = 0
		return false
	}
	tracker.failures++
	return tracker.failures >= healthFailureLimit
}
