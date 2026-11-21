load("@babelsuite/runtime", "test")

show = test.run(name="show", image="busybox:latest", commands=["echo PROFILE_VAR=$PROFILE_VAR", "echo GLOBAL_VAR=$GLOBAL_VAR"])
