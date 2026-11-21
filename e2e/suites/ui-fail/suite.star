load("@babelsuite/runtime", "task", "test")

prep = task.run(name="prep", image="busybox:latest", commands=["echo UI-FAIL prepared"])
boom = test.run(name="boom", image="busybox:latest", commands=["echo UI-FAIL about to fail", "exit 3"], expect_exit=0, after=[prep])
