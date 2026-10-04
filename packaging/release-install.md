## Install

One line, on Linux or macOS. It installs the package or archive below for this computer, checks it against `SHA256SUMS.txt`, and starts Yggdrasil as a service:

```bash
curl -fsSL https://github.com/yeixio/yggdrasil-core/releases/latest/download/install.sh | sh
```

On Windows, in PowerShell: `irm https://github.com/yeixio/yggdrasil-core/releases/latest/download/install.ps1 | iex`. To add a computer to an existing network, use the command `toskarctl join-token create` prints.

macOS. Homebrew installs Yggdrasil Core from this repository. It does not install Yggdrasil Desktop. The short command `brew install yggdrasil` is a different Homebrew cask.

```bash
brew tap yeixio/yggdrasil https://github.com/yeixio/yggdrasil-core
brew install yeixio/yggdrasil/yggdrasil
toskar
```

Or use the headless archive attached to this release:

```bash
tar -xzf yggdrasil-*-darwin-*-headless.tar.gz
cd yggdrasil-*-darwin-*-headless
./toskar
```

Open `http://127.0.0.1:7331`.

Debian and Ubuntu:

```bash
echo "deb [trusted=yes] https://raw.githubusercontent.com/yeixio/yggdrasil-core/apt stable main" | sudo tee /etc/apt/sources.list.d/yggdrasil.list
sudo apt-get update
sudo apt-get install yggdrasil
```

RPM packages for x86_64 and aarch64 are attached to this release. Install one with `sudo rpm -i` or `sudo dnf install`.

Windows amd64. The headless archive attached to this release is not code-signed.

```bash
tar -xzf yggdrasil-*-windows-amd64-headless.tar.gz
cd yggdrasil-*-windows-amd64-headless
./toskar.exe
```

Open `http://127.0.0.1:7331`.

## Known limits

Binaries are not code-signed. The apt repository is unsigned (`trusted=yes`). A bearer token on plain HTTP does not encrypt traffic. A clean install of this Windows archive, and of the Intel Mac archive, has not been recorded in the repository.
