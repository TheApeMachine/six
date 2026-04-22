package network

/*
finishMonitoredRW runs one read or write syscall (or equivalent) after Allow()
and transport-specific setup have succeeded. On error it classifies and
records failure; on success it records success.
*/
func finishMonitoredRW(
	monitor *transportMonitor,
	classify func(error) (TransportFailureMode, bool),
	work func() (int, error),
) (int, error) {
	n, err := work()

	if err != nil {
		mode, systemic := classify(err)

		monitor.RecordFailure(mode, err, systemic)

		return n, err
	}

	monitor.RecordSuccess()

	return n, nil
}

