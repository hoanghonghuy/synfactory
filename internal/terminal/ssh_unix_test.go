//go:build !windows

package terminal

import "testing"

func TestClassifySSHFailure(t *testing.T) {
	tests := []struct {
		name    string
		message string
		want    SSHFailureClass
	}{
		{name: "host key rejected", message: "Host key verification failed.", want: SSHFailureHostKey},
		{name: "host key changed", message: "WARNING: REMOTE HOST IDENTIFICATION HAS CHANGED!", want: SSHFailureHostKey},
		{name: "public key auth rejected", message: "Permission denied (publickey).", want: SSHFailureAuth},
		{name: "network refused", message: "ssh: connect to host example port 22: Connection refused", want: SSHFailureNetwork},
		{name: "unknown", message: "ssh exited unexpectedly", want: SSHFailureUnknown},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := ClassifySSHFailure(test.message); got != test.want {
				t.Fatalf("ClassifySSHFailure(%q) = %q, want %q", test.message, got, test.want)
			}
		})
	}
}
