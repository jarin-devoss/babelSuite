package dockerdb

import "testing"

func TestHostPortPrefersTheIPv4Binding(t *testing.T) {
	t.Parallel()
	if got := hostPort(t, "[::]:49300\n127.0.0.1:49301\n"); got != 49301 {
		t.Fatalf("hostPort = %d, want the IPv4 binding 49301", got)
	}
	if got := hostPort(t, "[::1]:49302\n"); got != 49302 {
		t.Fatalf("hostPort = %d, want the only binding 49302", got)
	}
}

// Pull progress on stdout or stderr must not be taken for the container id.
func TestTheContainerIDIsTheLastLineAndLooksLikeOne(t *testing.T) {
	t.Parallel()
	out := "Unable to find image 'postgres:16' locally\n16: Pulling from library/postgres\n1f2e3d4c5b6a7988\n"
	if got := lastLine(out); got != "1f2e3d4c5b6a7988" || !containerID.MatchString(got) {
		t.Fatalf("lastLine = %q", got)
	}
	if containerID.MatchString("16: Pulling from library/postgres") {
		t.Fatal("pull progress was taken for a container id")
	}
}
