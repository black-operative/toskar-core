# Homebrew formula written by the core release workflow.
class Toskar < Formula
  desc "Local AI daemon and web UI"
  homepage "https://toskar.ai"
  version "1.6.0"
  license "AGPL-3.0-or-later"

  on_arm do
    url "https://github.com/yeixio/toskar-core/releases/download/v1.6.0/toskar-1.6.0-darwin-arm64-headless.tar.gz"
    sha256 "4751b82fd0e07fba19ceabca5ba4a7962d3a6bcc89b989abf7a89bb6612eabc7"
  end

  on_intel do
    url "https://github.com/yeixio/toskar-core/releases/download/v1.6.0/toskar-1.6.0-darwin-amd64-headless.tar.gz"
    sha256 "cdd823fa31898a99d626ad3361652c700249a8245cc65ae5cc34e6edb43a37ef"
  end

  def install
    bin.install "toskar"
    bin.install "toskarctl"
    # The names from before the rename, for launchd agents and MCP settings
    # that run them by path.
    bin.install_symlink bin/"toskar" => "yggdrasil-daemon"
    bin.install_symlink bin/"toskarctl" => "yggctl"
    bash_completion.install "completions/toskarctl.bash" => "toskarctl"
    bash_completion.install_symlink bash_completion/"toskarctl" => "yggctl"
    zsh_completion.install "completions/_toskarctl"
    fish_completion.install "completions/toskarctl.fish"
    fish_completion.install_symlink fish_completion/"toskarctl.fish" => "yggctl.fish"
    (share/"yggdrasil").install "web"
  end

  def caveats
    <<~EOS
      Start the daemon, then open the web UI:

        toskar
        open http://127.0.0.1:7331
    EOS
  end
end
