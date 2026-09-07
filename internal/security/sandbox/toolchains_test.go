package sandbox

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestPolicyDiscoversHostToolchainsThroughGenericExposure(t *testing.T) {
	root := t.TempDir()
	workspace := filepath.Join(root, "workspace")
	bin := filepath.Join(root, "host-tools", "bin")
	rustup := filepath.Join(root, "host-state", "rustup")
	for _, directory := range []string{workspace, bin, rustup} {
		if err := os.MkdirAll(directory, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	private := filepath.Join(root, "private")
	if err := os.Mkdir(private, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"rustup", "cargo", "rustc"} {
		if err := os.WriteFile(
			filepath.Join(bin, name),
			[]byte("#!/bin/sh\n"),
			0o755,
		); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", bin)
	t.Setenv("RUSTUP_HOME", rustup)

	policy, err := BuildPolicy(Options{
		WorkspaceRoot: workspace,
		PrivateTemp:   private,
	})
	if err != nil {
		t.Fatal(err)
	}
	canonicalBin, err := filepath.EvalSymlinks(bin)
	if err != nil {
		t.Fatal(err)
	}
	canonicalRustup, err := filepath.EvalSymlinks(rustup)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(policy.Toolchains.BinDirs, canonicalBin) {
		t.Fatalf("toolchain bins = %v, want %s", policy.Toolchains.BinDirs, canonicalBin)
	}
	if !slices.Contains(policy.Toolchains.ReadRoots, canonicalRustup) ||
		!slices.Contains(policy.HostReadRoots, canonicalRustup) {
		t.Fatalf(
			"toolchain roots = %v host roots = %v, want %s",
			policy.Toolchains.ReadRoots,
			policy.HostReadRoots,
			canonicalRustup,
		)
	}
	if !slices.Contains(
		policy.Toolchains.Environment,
		"RUSTUP_HOME="+canonicalRustup,
	) {
		t.Fatalf(
			"toolchain environment = %v",
			policy.Toolchains.Environment,
		)
	}
}

func TestPolicyDiscoversEveryInstalledToolchainEntry(t *testing.T) {
	root := t.TempDir()
	workspace := filepath.Join(root, "workspace")
	nodeRoot := filepath.Join(root, "node")
	npmRoot := filepath.Join(root, "npm")
	private := filepath.Join(root, "private")
	for _, directory := range []string{
		workspace,
		filepath.Join(nodeRoot, "bin"),
		filepath.Join(npmRoot, "bin"),
		private,
	} {
		if err := os.MkdirAll(directory, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for path, content := range map[string]string{
		filepath.Join(nodeRoot, "bin", "node"): "#!/bin/sh\n",
		filepath.Join(npmRoot, "bin", "npm"):   "#!/bin/sh\n",
	} {
		if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv(
		"PATH",
		strings.Join(
			[]string{
				filepath.Join(nodeRoot, "bin"),
				filepath.Join(npmRoot, "bin"),
			},
			string(os.PathListSeparator),
		),
	)

	policy, err := BuildPolicy(Options{
		WorkspaceRoot: workspace,
		PrivateTemp:   private,
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{nodeRoot, npmRoot} {
		want, err = filepath.EvalSymlinks(want)
		if err != nil {
			t.Fatal(err)
		}
		if !slices.Contains(policy.Toolchains.ReadRoots, want) {
			t.Fatalf(
				"toolchain roots = %v, want %s",
				policy.Toolchains.ReadRoots,
				want,
			)
		}
	}
}

func TestGoToolchainFollowsSelectedExecutableNotRuntimeBuildRoot(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	installation := filepath.Join(root, "go", "libexec")
	bin := filepath.Join(root, "bin")
	for _, directory := range []string{
		filepath.Join(installation, "bin"), filepath.Join(installation, "src"),
		filepath.Join(installation, "pkg", "tool"), bin,
	} {
		if err := os.MkdirAll(directory, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	executable := filepath.Join(installation, "bin", "go")
	if err := os.WriteFile(executable, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(executable, filepath.Join(bin, "go")); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)
	t.Setenv("GOROOT", "")
	exposure := discoverToolchains(filepath.Join(root, "workspace"), nil, nil)
	canonical, err := filepath.EvalSymlinks(installation)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(exposure.ReadRoots, canonical) ||
		!slices.Contains(exposure.Environment, "GOROOT="+canonical) {
		t.Fatalf("incomplete selected toolchain: %+v", exposure)
	}
	t.Setenv("GOROOT", root)
	// Invalid parent/home exposure is still rejected by the normal policy.
	exposure = discoverToolchains(filepath.Join(root, "workspace"), nil, nil)
	if slices.Contains(exposure.ReadRoots, root) {
		t.Fatal("workspace parent was exposed as a toolchain")
	}
}
