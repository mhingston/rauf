package main

import (
	"context"
	"os"
	"os/exec"
	"testing"
)

func TestRuntimeExecHelpers(t *testing.T) {
	r := runtimeExec{
		WorkDir:         "/test/dir",
		DockerContainer: "my-container",
	}

	t.Run("docker container name override", func(t *testing.T) {
		name := r.dockerContainerName("/other/dir")
		if name != "my-container" {
			t.Errorf("got %q, want 'my-container'", name)
		}
	})

	t.Run("docker container name compute", func(t *testing.T) {
		r2 := runtimeExec{WorkDir: "/projects/rauf"}
		name := r2.dockerContainerName("/projects/rauf")
		if name == "" {
			t.Error("expected non-empty container name")
		}
		// Should start with rauf-slugified-base
		if !contains(name, "rauf-rauf-") {
			t.Errorf("got %q, expected prefix rauf-rauf-", name)
		}
	})

	t.Run("validate docker args", func(t *testing.T) {
		err := validateDockerArgs([]string{"--rm", "-v", "/a:/b"})
		if err != nil {
			t.Errorf("valid args should pass: %v", err)
		}

		err = validateDockerArgs([]string{"--privileged"})
		if err == nil {
			t.Error("dangerous arg --privileged should fail")
		}

		err = validateDockerArgs([]string{"--cap-add=SYS_ADMIN"})
		if err == nil {
			t.Error("dangerous arg --cap-add should fail")
		}
	})

	t.Run("format docker volume", func(t *testing.T) {
		v := formatDockerVolume("/host/path", "/container/path")
		if v != "/host/path:/container/path" {
			t.Errorf("got %q, want '/host/path:/container/path'", v)
		}
	})
}

func TestIsDocker(t *testing.T) {
	if (runtimeExec{Runtime: "docker"}).isDocker() != true {
		t.Error("docker should be true")
	}
	if (runtimeExec{Runtime: "DOCKER"}).isDocker() != true {
		t.Error("DOCKER should be true")
	}
	if (runtimeExec{Runtime: "shell"}).isDocker() != false {
		t.Error("shell should be false")
	}
}

func TestIsDockerPersist(t *testing.T) {
	if (runtimeExec{Runtime: "docker-persist"}).isDockerPersist() != true {
		t.Error("docker-persist should be true")
	}
	if (runtimeExec{Runtime: "docker_persist"}).isDockerPersist() != true {
		t.Error("docker_persist should be true")
	}
}

func TestRuntimeExec_Execution(t *testing.T) {
	orig := execCommand
	defer func() { execCommand = orig }()

	ctx := context.Background()

	t.Run("simple command", func(t *testing.T) {
		r := runtimeExec{Runtime: "shell"}
		execCommand = func(ctx context.Context, name string, args ...string) *exec.Cmd {
			return exec.Command("echo", "test")
		}
		cmd, err := r.command(ctx, "ls")
		if err != nil {
			t.Fatal(err)
		}
		if cmd.Args[0] != "echo" {
			t.Errorf("expected echo, got %s", cmd.Args[0])
		}
	})

	t.Run("docker-run command", func(t *testing.T) {
		r := runtimeExec{Runtime: "docker", DockerImage: "alpine"}
		execCommand = func(ctx context.Context, name string, args ...string) *exec.Cmd {
			if name == "docker" && args[0] == "run" {
				return exec.Command("true")
			}
			return exec.Command("false")
		}
		cmd, err := r.command(ctx, "ls")
		if err != nil {
			t.Fatal(err)
		}
		if cmd.Args[0] != "true" {
			t.Error("expected docker run command")
		}
	})

	t.Run("ensureDockerContainer - running", func(t *testing.T) {
		r := runtimeExec{Runtime: "docker-persist", DockerImage: "alpine", DockerContainer: "test-c"}
		execCommand = func(ctx context.Context, name string, args ...string) *exec.Cmd {
			// Mock docker inspect status
			return exec.Command("sh", "-c", "echo running")
		}
		err := r.ensureDockerContainer(ctx, "test-c", "/tmp")
		if err != nil {
			t.Error(err)
		}
	})

	t.Run("dockerContainerState - not found", func(t *testing.T) {
		r := runtimeExec{}
		execCommand = func(ctx context.Context, name string, args ...string) *exec.Cmd {
			// Return error that looks like "not found"
			return exec.Command("sh", "-c", "echo 'Error: No such object' >&2; exit 1")
		}
		status, err := r.dockerContainerState(ctx, "missing")
		if err != nil {
			t.Fatal(err)
		}
		if status != "none" {
			t.Errorf("expected none, got %q", status)
		}
	})

	t.Run("resolveWorkDir", func(t *testing.T) {
		r := runtimeExec{WorkDir: "/tmp/foo"}
		wd, err := r.resolveWorkDir()
		if err != nil {
			t.Fatal(err)
		}
		if !contains(wd, "foo") {
			t.Errorf("got %q, want it to contain 'foo'", wd)
		}

		r2 := runtimeExec{WorkDir: ""}
		wd2, err := r2.resolveWorkDir()
		if err != nil {
			t.Fatal(err)
		}
		if wd2 == "" {
			t.Error("expected non-empty default workdir")
		}
	})

	t.Run("runShell", func(t *testing.T) {
		r := runtimeExec{Runtime: "shell"}
		execCommand = func(ctx context.Context, name string, args ...string) *exec.Cmd {
			return exec.Command("echo", "shell-output")
		}
		output, err := r.runShell(ctx, "echo 1", os.Stdout, os.Stderr)
		if err != nil {
			t.Fatal(err)
		}
		if !contains(output, "shell-output") {
			t.Errorf("got %q, want 'shell-output'", output)
		}
	})

	t.Run("commandDockerPersist", func(t *testing.T) {
		r := runtimeExec{
			Runtime:         "docker-persist",
			DockerImage:     "alpine",
			DockerContainer: "test-cont",
		}
		execCommand = func(ctx context.Context, name string, args ...string) *exec.Cmd {
			if name == "docker" && args[0] == "inspect" {
				return exec.Command("sh", "-c", "echo running")
			}
			return exec.Command("echo", "execed")
		}
		cmd, err := r.command(ctx, "ls")
		if err != nil {
			t.Fatal(err)
		}
		if cmd.Args[0] != "echo" {
			t.Error("expected mock execed command")
		}
	})
}

func TestEnsureDockerContainer(t *testing.T) {
	origExec := execCommand
	defer func() { execCommand = origExec }()

	ctx := context.Background()
	runner := runtimeExec{
		Runtime:         "docker-persist",
		DockerImage:     "alpine",
		DockerContainer: "test-cont",
	}

	t.Run("already running", func(t *testing.T) {
		execCommand = func(ctx context.Context, name string, args ...string) *exec.Cmd {
			if name == "docker" && args[0] == "inspect" {
				return exec.Command("echo", "running")
			}
			return exec.Command("true")
		}
		err := runner.ensureDockerContainer(ctx, "test-cont", "/tmp")
		if err != nil {
			t.Fatal(err)
		}
	})

	t.Run("exited - should restart", func(t *testing.T) {
		restarted := false
		execCommand = func(ctx context.Context, name string, args ...string) *exec.Cmd {
			if name == "docker" && args[0] == "inspect" {
				return exec.Command("echo", "exited")
			}
			if name == "docker" && args[0] == "start" && args[1] == "test-cont" {
				restarted = true
				return exec.Command("true")
			}
			return exec.Command("true")
		}
		err := runner.ensureDockerContainer(ctx, "test-cont", "/tmp")
		if err != nil {
			t.Fatal(err)
		}
		if !restarted {
			t.Error("expected container to be restarted")
		}
	})

	t.Run("missing - should create", func(t *testing.T) {
		created := false
		execCommand = func(ctx context.Context, name string, args ...string) *exec.Cmd {
			if name == "docker" && args[0] == "inspect" {
				return exec.Command("sh", "-c", "echo 'not found' >&2; exit 1")
			}

			if name == "docker" && (args[0] == "run" || args[0] == "start") {
				created = true
				return exec.Command("echo", "cont-id")
			}
			return exec.Command("true")
		}

		err := runner.ensureDockerContainer(ctx, "test-cont", "/tmp")
		if err != nil {
			t.Fatal(err)
		}
		if !created {
			t.Error("expected container to be created")
		}
	})

}

func TestFormatDockerVolume(t *testing.T) {
	t.Run("simple path", func(t *testing.T) {
		got := formatDockerVolume("/tmp/foo", "/bar")
		if got != "/tmp/foo:/bar" {
			t.Errorf("got %q, want /tmp/foo:/bar", got)
		}
	})

	t.Run("path with spaces", func(t *testing.T) {
		got := formatDockerVolume("/tmp/my project", "/app")
		if got != "/tmp/my project:/app" {
			t.Errorf("got %q, want '/tmp/my project:/app'", got)
		}
	})

	t.Run("windows path (simulated)", func(t *testing.T) {
		// Even on unix, this string manip should work
		got := formatDockerVolume("C:\\Users\\Project", "/workspace")
		if got != "C:\\Users\\Project:/workspace" {
			t.Errorf("got %q, expected simple concatenation", got)
		}
	})
}

func TestShellArgs(t *testing.T) {
	t.Run("shell runtime", func(t *testing.T) {
		r := runtimeExec{Runtime: "shell"}
		name, args := r.shellArgs("echo hello")
		if name != "sh" {
			t.Errorf("expected sh, got %s", name)
		}
		if len(args) != 2 || args[0] != "-c" || args[1] != "echo hello" {
			t.Errorf("unexpected args: %v", args)
		}
	})

	t.Run("docker runtime", func(t *testing.T) {
		r := runtimeExec{Runtime: "docker"}
		name, args := r.shellArgs("ls -la")
		if name != "sh" {
			t.Errorf("expected sh for docker, got %s", name)
		}
		if len(args) != 2 || args[0] != "-c" {
			t.Errorf("unexpected args: %v", args)
		}
	})

	t.Run("docker-persist runtime", func(t *testing.T) {
		r := runtimeExec{Runtime: "docker-persist"}
		name, args := r.shellArgs("cat /etc/hosts")
		if name != "sh" {
			t.Errorf("expected sh for docker-persist, got %s", name)
		}
		if len(args) != 2 || args[0] != "-c" {
			t.Errorf("unexpected args: %v", args)
		}
	})
}
