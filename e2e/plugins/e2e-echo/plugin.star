load("@babelsuite/runtime", "plugin")

_ref = plugin("e2e-echo")

def echo(name, message = "hello", fail = False, after = []):
    return _ref.echo(
        name = name,
        after = after,
        message = message,
        fail = fail,
    )
