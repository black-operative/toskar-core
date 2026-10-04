# Homebrew formula written by the core release workflow.
class Toskar < Formula
  desc "Local AI daemon and web UI"
  homepage "https://yggdrasil.yeix.io"
  version "1.5.0"
  license "AGPL-3.0-or-later"

  on_arm do
    url "https://github.com/yeixio/yggdrasil-core/releases/download/v1.5.0/yggdrasil-1.5.0-darwin-arm64-headless.tar.gz"
    sha256 "5613faa1361349a1ba8b5ff1170e2c7e2f64472a11e71a6bae65c693801f6a34"
  end

  on_intel do
    url "https://github.com/yeixio/yggdrasil-core/releases/download/v1.5.0/yggdrasil-1.5.0-darwin-amd64-headless.tar.gz"
    sha256 "3f745508013bff6f549b0aca6a1d3f5f12911cdc0ec09a1f5f52d3a65857f447"
  end

  def install
    bin.install "yggdrasil-daemon"
    bin.install "yggctl"
    bash_completion.install "completions/yggctl.bash" => "yggctl"
    zsh_completion.install "completions/_yggctl"
    fish_completion.install "completions/yggctl.fish"
    (share/"yggdrasil").install "web"
  end

  def caveats
    <<~EOS
      Start the daemon, then open the web UI:

        yggdrasil-daemon
        open http://127.0.0.1:7331
    EOS
  end
end
