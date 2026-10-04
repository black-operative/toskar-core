package main

import (
	"embed"
	"fmt"
	"io"
)

// The packages install these same files, so the script a user prints and the
// script a package installs do not drift.
//
//go:embed completions/toskarctl.bash completions/_toskarctl completions/toskarctl.fish
var completionFiles embed.FS

const completionUsage = `usage: toskarctl completion <bash|zsh|fish>
  bash: eval "$(toskarctl completion bash)"
  zsh:  toskarctl completion zsh > ~/.zfunc/_toskarctl   (with ~/.zfunc on fpath before compinit)
  fish: toskarctl completion fish > ~/.config/fish/completions/toskarctl.fish`

var completionPaths = map[string]string{
	"bash": "completions/toskarctl.bash",
	"zsh":  "completions/_toskarctl",
	"fish": "completions/toskarctl.fish",
}

func completionCommand(args []string, out io.Writer) error {
	if len(args) != 1 {
		return fmt.Errorf("%s", completionUsage)
	}
	path, ok := completionPaths[args[0]]
	if !ok {
		return fmt.Errorf("%s", completionUsage)
	}
	script, err := completionFiles.ReadFile(path)
	if err != nil {
		return err
	}
	_, err = out.Write(script)
	return err
}
