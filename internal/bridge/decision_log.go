package bridge

// routeDecisionLogArgs builds the native_route_decision log attributes.
// Classifier fields are added only when a classifier ran.
func routeDecisionLogArgs(request string, d RouteDecision) []any {
	args := []any{"request", request, "route", d.Route, "source", d.Source}
	if d.Backend == "" {
		return args
	}
	args = append(args, "backend", d.Backend, "latency_ms", d.LatencyMS)
	if d.Backend == "decisions" {
		args = append(args, "confidence", d.Confidence)
	}
	if d.ErrorClass != "" {
		args = append(args, "error_class", d.ErrorClass)
	}
	if d.DecisionsMS != 0 || d.ConnReused != nil {
		args = append(args, "decisions_ms", d.DecisionsMS)
	}
	if d.ConnReused != nil {
		args = append(args, "conn_reused", *d.ConnReused)
	}
	if d.ErrorCode != "" {
		args = append(args, "error_code", d.ErrorCode)
	}
	if d.ErrorClass != "" && d.RequestID != "" {
		args = append(args, "request_id", d.RequestID)
	}
	return args
}
