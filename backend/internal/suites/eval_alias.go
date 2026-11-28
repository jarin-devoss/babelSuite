package suites

import "github.com/babelsuite/babelsuite/internal/suites/eval"

// The evaluator of suite.star lives in suites/eval.
type (
	rawTopologyNode = eval.RawNode
	ModuleResolver  = eval.ModuleResolver
)

func evalStarlarkTopology(suiteStar string, resolve ModuleResolver) ([]rawTopologyNode, error) {
	return eval.Eval(suiteStar, resolve)
}

func evalStarlarkTopologyWithEnv(suiteStar string, resolve ModuleResolver, env map[string]string) ([]rawTopologyNode, error) {
	return eval.EvalWithEnv(suiteStar, resolve, env)
}
