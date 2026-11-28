package suites

import (
	"go.starlark.net/starlark"

	"github.com/babelsuite/babelsuite/internal/suites/starkit"
)

// The Starlark glue is shared with the traffic-plan evaluator, in suites/starkit.
const starlarkMaxSteps = starkit.MaxSteps

var starlarkFileOptions = starkit.FileOptions

type (
	starlarkBuilderFunc = starkit.Builder
	starlarkNamespace   = starkit.Namespace
)

func frozenEnvDict(env map[string]string) *starlark.Dict { return starkit.FrozenEnv(env) }
