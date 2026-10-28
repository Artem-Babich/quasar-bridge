package bridge

// routeDecisionLogArgs builds the native_route_decision log attributes.
// Classifier fields are added only when a classifier ran.
func routeDecisionLogArgs(request string, d RouteDecision) []any {
	args := []any{"request", request, "route", d.Route, "source", d.Source}
	if d.Backend == "" {
		return args
	}
	args = append(args, "backend", d.Backend, "latency_ms", d.LatencyMS)
	if d.Confidence != 0 {
		args = append(args, "confidence", d.Confidence)
	}
	if d.ErrorClass != "" {
		args = append(args, "error_class", d.ErrorClass)
	}
	return args
}
