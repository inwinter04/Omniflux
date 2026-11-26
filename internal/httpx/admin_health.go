package httpx

func deriveHealthStatus(resourceStatus string, requests int64, successRate, averageLatencyMS *float64, activeCandidates int) string {
	if resourceStatus == "disabled" {
		return "disabled"
	}
	if activeCandidates == 0 {
		return "no_candidates"
	}
	if requests == 0 || successRate == nil {
		return "no_data"
	}
	if *successRate >= 0.99 && (averageLatencyMS == nil || *averageLatencyMS < 5000) {
		return "healthy"
	}
	if *successRate >= 0.95 && (averageLatencyMS == nil || *averageLatencyMS < 15000) {
		return "degraded"
	}
	return "unhealthy"
}

func deriveAccountHealthStatus(resourceStatus, activeCheckStatus string, requests int64, successRate, averageLatencyMS *float64, activeCandidates int) string {
	if resourceStatus == "disabled" {
		return "disabled"
	}
	passive := ""
	if requests > 0 && successRate != nil {
		switch {
		case *successRate >= 0.99 && (averageLatencyMS == nil || *averageLatencyMS < 5000):
			passive = "healthy"
		case *successRate >= 0.95 && (averageLatencyMS == nil || *averageLatencyMS < 15000):
			passive = "degraded"
		default:
			passive = "unhealthy"
		}
	}

	if passive == "unhealthy" || activeCheckStatus == "unhealthy" {
		return "unhealthy"
	}
	if passive == "degraded" || activeCheckStatus == "degraded" {
		return "degraded"
	}
	if activeCheckStatus == "healthy" || passive == "healthy" {
		return "healthy"
	}
	if activeCandidates == 0 {
		return "no_candidates"
	}
	return "no_data"
}
