package topology

import "github.com/babelsuite/babelsuite/internal/suites/eval"

type (
	rawTopologyNode = eval.RawNode
	ModuleResolver  = eval.ModuleResolver
)

func evalStarlarkTopologyWithEnv(suiteStar string, resolve ModuleResolver, env map[string]string) ([]rawTopologyNode, error) {
	return eval.EvalWithEnv(suiteStar, resolve, env)
}
