package httpx

func deriveHealthStatus(resourceStatus string, requests int64, successRate *float64, activeCandidates int) (string, string) {
	if resourceStatus == "disabled" {
		return "disabled", "disabled"
	}
	if activeCandidates == 0 {
		return "no_candidates", "no_candidates"
	}
	if requests == 0 || successRate == nil {
		return "no_data", "no_traffic"
	}
	if *successRate >= 0.99 {
		return "healthy", "healthy"
	}
	if *successRate >= 0.95 {
		return "degraded", "passive_degraded_success_rate"
	}
	return "unhealthy", "passive_low_success_rate"
}

func deriveAccountHealthStatus(
	resourceStatus, activeCheckStatus, activeCheckReason string,
	requests int64,
	successRate *float64,
	activeCandidates int,
) (string, string) {
	if resourceStatus == "disabled" {
		return "disabled", "disabled"
	}

	passive := ""
	if requests > 0 && successRate != nil {
		switch {
		case *successRate >= 0.99:
			passive = "healthy"
		case *successRate >= 0.95:
			passive = "degraded"
		default:
			passive = "unhealthy"
		}
	}

	if passive == "unhealthy" {
		if activeCheckStatus == "unhealthy" {
			return "unhealthy", "active_and_passive_unhealthy"
		}
		return "unhealthy", "passive_low_success_rate"
	}
	if activeCheckStatus == "unhealthy" {
		if activeCheckReason == "credentials_rejected" || activeCheckReason == "v1_suffix_suspected" {
			return "unhealthy", "active_probe_unhealthy"
		}
		if passive == "healthy" {
			return "degraded", "probe_traffic_mismatch"
		}
		return "unhealthy", "active_probe_unhealthy"
	}
	if passive == "degraded" {
		if activeCheckStatus == "degraded" {
			return "degraded", "active_and_passive_degraded"
		}
		return "degraded", "passive_degraded_success_rate"
	}
	if activeCheckStatus == "degraded" {
		return "degraded", "active_probe_degraded"
	}
	if activeCheckStatus == "healthy" || passive == "healthy" {
		return "healthy", "healthy"
	}
	if activeCandidates == 0 {
		return "no_candidates", "no_candidates"
	}
	return "no_data", "no_data"
}
