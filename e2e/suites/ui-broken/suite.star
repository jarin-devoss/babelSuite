load("@babelsuite/runtime", "test")

broken = test.run(name="broken", image="busybox:latest", commands=["echo never runs"], after=[no_such_node])
