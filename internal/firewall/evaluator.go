package firewall

type Evaluator interface {
	EvaluateURL(targetURL string) (bool, string, error)
}
