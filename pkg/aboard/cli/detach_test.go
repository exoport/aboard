package cli

import (
	"testing"

	"github.com/spf13/cobra"

	"github.com/exoport/aboard/pkg/aboard"
)

// --detach starts the same command again from the same binary, so it has to be
// able to SAY that command. Standalone the binary is `aboard` and the command is
// `serve`; mounted in ape the binary is `ape` and the command is `aboard serve`,
// and a detach that dropped the middle word would start ape's own root with a
// flag it has never heard of.
func TestDetachArgsSayTheSameCommandAgain(t *testing.T) {
	for _, tc := range []struct {
		name  string
		mount func(board *cobra.Command) *cobra.Command
		want  []string
	}{
		{
			name:  "standalone",
			mount: func(board *cobra.Command) *cobra.Command { return board },
			want:  []string{"serve", "--cwd=/somewhere", "--dev=true", "--name=review", "--port=48000"},
		},
		{
			name: "mounted in a host",
			mount: func(board *cobra.Command) *cobra.Command {
				host := &cobra.Command{Use: "ape"}
				host.AddCommand(board)
				return host
			},
			want: []string{"aboard", "serve", "--cwd=/somewhere", "--dev=true", "--name=review", "--port=48000"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			board := NewRootCmd(Options{Host: aboard.HostStandalone})
			tc.mount(board)
			serve, _, err := board.Find([]string{"serve"})
			if err != nil {
				t.Fatalf("finding serve: %v", err)
			}
			// Parsed through the command itself, so the root's persistent flags
			// are merged in exactly as they are on a real run.
			if err := serve.ParseFlags([]string{"--cwd", "/somewhere", "--name", "review", "--detach", "--port", "48000", "--dev"}); err != nil {
				t.Fatalf("parsing: %v", err)
			}
			got := detachArgs(serve)
			if len(got) != len(tc.want) {
				t.Fatalf("detachArgs = %q, want %q", got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Fatalf("detachArgs = %q, want %q", got, tc.want)
				}
			}
		})
	}
}
