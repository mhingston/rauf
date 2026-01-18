package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

type runtimeExec struct {
	Runtime         string
	DockerImage     string
	DockerArgs      []string
	DockerContainer string
	WorkDir         string
}

func (r runtimeExec) isDocker() bool {
	return strings.ToLower(r.Runtime) == "docker"
}

func (r runtimeExec) isDockerPersist() bool {
	value := strings.ToLower(r.Runtime)
	return value == "docker-persist" || value == "docker_persist"
}

func (r runtimeExec) command(ctx context.Context, name string, args ...string) (*exec.Cmd, error) {
	if !r.isDocker() && !r.isDockerPersist() {
		return exec.CommandContext(ctx, name, args...), nil
	}
	if r.DockerImage == "" {
		return nil, errors.New("docker runtime requires docker_image")
	}
	workdir, err := r.resolveWorkDir()
	if err != nil {
		return nil, err
	}
	if r.isDockerPersist() {
		return r.commandDockerPersist(ctx, workdir, name, args...)
	}
	return r.commandDockerRun(ctx, workdir, name, args...)
}

func (r runtimeExec) runShell(ctx context.Context, command string, stdout, stderr io.Writer) (string, error) {
	buffer := &limitedBuffer{max: 16 * 1024}
	name, args := r.shellArgs(command)
	cmd, err := r.command(ctx, name, args...)
	if err != nil {
		return "", err
	}
	cmd.Stdout = io.MultiWriter(stdout, buffer)
	cmd.Stderr = io.MultiWriter(stderr, buffer)
	cmd.Env = os.Environ()
	return buffer.String(), cmd.Run()
}

func (r runtimeExec) shellArgs(command string) (string, []string) {
	if r.isDocker() || r.isDockerPersist() {
		return "sh", []string{"-c", command}
	}
	if runtime.GOOS == "windows" {
		return "cmd", []string{"/C", command}
	}
	return "sh", []string{"-c", command}
}

func (r runtimeExec) resolveWorkDir() (string, error) {
	workdir := r.WorkDir
	if workdir == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return "", err
		}
		workdir = cwd
	}
	return filepath.Clean(workdir), nil
}

func (r runtimeExec) commandDockerRun(ctx context.Context, workdir string, name string, args ...string) (*exec.Cmd, error) {
	dockerArgs := []string{"run", "--rm", "-i"}
	if uid, gid := hostUIDGID(); uid >= 0 && gid >= 0 {
		dockerArgs = append(dockerArgs, "-u", fmt.Sprintf("%d:%d", uid, gid))
	}
	// Format volume mount path appropriately for the platform
	volumeMount := formatDockerVolume(workdir, "/workspace")
	dockerArgs = append(dockerArgs, "-v", volumeMount, "-w", "/workspace")
	if len(r.DockerArgs) > 0 {
		if err := validateDockerArgs(r.DockerArgs); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: %v\n", err)
		}
		dockerArgs = append(dockerArgs, r.DockerArgs...)
	}
	dockerArgs = append(dockerArgs, r.DockerImage, name)
	dockerArgs = append(dockerArgs, args...)
	return exec.CommandContext(ctx, "docker", dockerArgs...), nil
}

func (r runtimeExec) commandDockerPersist(ctx context.Context, workdir string, name string, args ...string) (*exec.Cmd, error) {
	container := r.dockerContainerName(workdir)
	if err := r.ensureDockerContainer(ctx, container, workdir); err != nil {
		return nil, err
	}
	dockerArgs := []string{"exec", "-i"}
	if uid, gid := hostUIDGID(); uid >= 0 && gid >= 0 {
		dockerArgs = append(dockerArgs, "-u", fmt.Sprintf("%d:%d", uid, gid))
	}
	dockerArgs = append(dockerArgs, container, name)
	dockerArgs = append(dockerArgs, args...)
	return exec.CommandContext(ctx, "docker", dockerArgs...), nil
}

func (r runtimeExec) dockerContainerName(workdir string) string {
	if strings.TrimSpace(r.DockerContainer) != "" {
		return r.DockerContainer
	}
	base := filepath.Base(workdir)
	if base == "" || base == "." || base == string(filepath.Separator) {
		base = "workspace"
	}
	// Include a short hash of the full path to avoid collisions when
	// different projects have the same directory name
	hash := sha256.Sum256([]byte(workdir))
	shortHash := fmt.Sprintf("%x", hash[:4])
	return "rauf-" + slugify(base) + "-" + shortHash
}

func (r runtimeExec) ensureDockerContainer(ctx context.Context, name, workdir string) error {
	if r.DockerImage == "" {
		return errors.New("docker runtime requires docker_image")
	}
	running, exists, err := r.dockerContainerState(ctx, name)
	if err != nil {
		return err
	}
	if exists && running {
		return nil
	}
	if exists && !running {
		cmd := exec.CommandContext(ctx, "docker", "start", name)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		cmd.Env = os.Environ()
		return cmd.Run()
	}

	dockerArgs := []string{"run", "-d", "--name", name}
	if uid, gid := hostUIDGID(); uid >= 0 && gid >= 0 {
		dockerArgs = append(dockerArgs, "-u", fmt.Sprintf("%d:%d", uid, gid))
	}
	// Format volume mount path appropriately for the platform
	volumeMount := formatDockerVolume(workdir, "/workspace")
	dockerArgs = append(dockerArgs, "-v", volumeMount, "-w", "/workspace")
	if len(r.DockerArgs) > 0 {
		if err := validateDockerArgs(r.DockerArgs); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: %v\n", err)
		}
		dockerArgs = append(dockerArgs, r.DockerArgs...)
	}
	dockerArgs = append(dockerArgs, r.DockerImage, "sh", "-c", "tail -f /dev/null")
	cmd := exec.CommandContext(ctx, "docker", dockerArgs...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = os.Environ()
	return cmd.Run()
}

func (r runtimeExec) dockerContainerState(ctx context.Context, name string) (running bool, exists bool, err error) {
	cmd := exec.CommandContext(ctx, "docker", "inspect", "-f", "{{.State.Running}}", name)
	var stderrBuf bytes.Buffer
	cmd.Stderr = &stderrBuf
	output, cmdErr := cmd.Output()
	if cmdErr != nil {
		// Check if the error is because the container doesn't exist
		stderr := stderrBuf.String()
		if strings.Contains(stderr, "No such object") || strings.Contains(stderr, "not found") {
			return false, false, nil
		}
		if exitErr, ok := cmdErr.(*exec.ExitError); ok {
			// Exit code 1 with no matching stderr message - container doesn't exist
			if exitErr.ExitCode() == 1 && stderr == "" {
				return false, false, nil
			}
		}
		// Some other error (Docker daemon down, permissions, etc.)
		return false, false, fmt.Errorf("docker inspect failed: %w", cmdErr)
	}
	value := strings.TrimSpace(string(output))
	return value == "true", true, nil
}

// validateDockerArgs checks for potentially dangerous Docker arguments
func validateDockerArgs(args []string) error {
	// List of flags that could override security-sensitive settings
	dangerousFlags := map[string]string{
		"--privileged":   "grants full host access",
		"--cap-add":      "adds container capabilities",
		"--security-opt": "modifies security settings",
		"--pid":          "shares host PID namespace",
		"--network=host": "shares host network namespace",
		"--ipc":          "shares IPC namespace",
	}
	for _, arg := range args {
		for flag, desc := range dangerousFlags {
			if arg == flag || strings.HasPrefix(arg, flag+"=") {
				return fmt.Errorf("docker_args contains %s which %s", flag, desc)
			}
		}
	}
	return nil
}

// formatDockerVolume formats a volume mount string appropriately for the platform.
// On Windows, Docker Desktop expects paths in a specific format.
// On Unix, warns if the path contains colons which could be misinterpreted.
func formatDockerVolume(hostPath, containerPath string) string {
	if runtime.GOOS == "windows" {
		// Docker Desktop on Windows accepts Windows paths directly in most cases,
		// but for WSL2 backend, paths may need conversion. Docker Desktop handles
		// this automatically for standard paths like C:\path.
		// Just warn about unusual paths that might cause issues.
		if !isWindowsAbsPath(hostPath) && strings.Contains(hostPath, ":") {
			fmt.Fprintf(os.Stderr, "Warning: workdir %q has unusual format for Windows Docker volume mount\n", hostPath)
		}
	} else {
		// On Unix, colons in paths are problematic as Docker uses colons as delimiters
		if strings.Contains(hostPath, ":") {
			fmt.Fprintf(os.Stderr, "Warning: workdir %q contains colons which may be misinterpreted by Docker volume mount\n", hostPath)
		}
	}
	return hostPath + ":" + containerPath
}

// isWindowsAbsPath checks if a path looks like a Windows absolute path (e.g., C:\foo)
func isWindowsAbsPath(path string) bool {
	if len(path) < 3 {
		return false
	}
	// Check for drive letter pattern: single letter followed by colon and backslash or slash
	drive := path[0]
	if (drive >= 'A' && drive <= 'Z') || (drive >= 'a' && drive <= 'z') {
		if path[1] == ':' && (path[2] == '\\' || path[2] == '/') {
			return true
		}
	}
	return false
}
