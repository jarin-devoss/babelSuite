package babelctl

// Version is the CLI build version. Set at compile time via ldflags:
//
//	go build -ldflags "-X github.com/babelsuite/babelsuite/cli/ctl.Version=v1.2.3" ./cmd/ctl
var Version = "dev"
