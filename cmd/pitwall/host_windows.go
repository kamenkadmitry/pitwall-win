//go:build windows

package main

import (
	"bytes"
	"fmt"
	"net"
	"os/exec"
	"time"

	"github.com/quanticstudios/pitwall/internal/nowindow"
)

// control is Windows' forward: its ssh cannot add or drop a forward on an
// existing connection, so "forward" runs a dedicated ssh -N -L that holds
// it, and "cancel" kills that process.
func (r *sshHost) control(op, sock string) error {
	switch op {
	case "forward":
		return r.forwardStart(sock)
	case "cancel":
		r.forwardStop()
		return nil
	}
	return fmt.Errorf("control %q", op)
}

// forwardStart runs ssh -N -L r.local:sock and returns once the forwarded
// port answers a TCP connect, or ssh exits. An earlier forward process of
// this sshHost, live or dead, is stopped first.
func (r *sshHost) forwardStart(sock string) error {
	r.forwardStop()
	// why: ExitOnForwardFailure makes a bind failure (the port got taken
	// between our probe and ssh's bind) exit the process instead of
	// leaving a connection without the forward.
	cmd := exec.Command(r.ssh, r.sshArgs([]string{"-N", "-o", "ExitOnForwardFailure=yes", "-L", r.local + ":" + sock})...)
	nowindow.Set(cmd)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("run ssh: %w", err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	deadline := time.Now().Add(15 * time.Second)
	for {
		select {
		case err := <-done:
			msg := lastLine(stderr.String())
			if msg == "" && err != nil {
				msg = err.Error()
			}
			return fmt.Errorf("ssh forward exited: %s", msg)
		default:
		}
		// A probe connect reaches the host's daemon through the forward and
		// disconnects without a Hello, which the daemon tolerates.
		if c, err := net.DialTimeout("tcp", r.local, 200*time.Millisecond); err == nil {
			c.Close()
			r.mu.Lock()
			r.fwd = cmd
			r.mu.Unlock()
			return nil
		}
		if time.Now().After(deadline) {
			cmd.Process.Kill()
			<-done
			return fmt.Errorf("ssh forward of %s to %s did not come up", sock, r.local)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// forwardStop kills the forward's ssh process when one runs. The wait
// goroutine from forwardStart reaps it.
func (r *sshHost) forwardStop() {
	r.mu.Lock()
	cmd := r.fwd
	r.fwd = nil
	r.mu.Unlock()
	if cmd != nil && cmd.Process != nil {
		cmd.Process.Kill()
	}
}
