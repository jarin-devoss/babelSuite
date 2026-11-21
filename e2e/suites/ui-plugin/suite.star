load("@babelsuite/runtime", "mock")
load("@plugins/e2e-echo", "echo")

gateway = mock.api(name="plugin-mock")
hello = echo(name="hello", message="hi from the plugin", after=[gateway])
rejected = echo(name="rejected", message="not this one", fail=True, after=[hello])
