//go:build !darwin

package sandbox

func discoverPlatformToolchains(_ *ToolchainExposure, _ string, _ map[string]bool) {}
