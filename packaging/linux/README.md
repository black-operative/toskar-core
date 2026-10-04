# Linux packages

Release CI builds a `.deb` and `.rpm` for amd64 and arm64, and publishes an unsigned apt repository on the `apt` branch.

```bash
echo "deb [trusted=yes] https://raw.githubusercontent.com/yeixio/yggdrasil-core/apt stable main" | sudo tee /etc/apt/sources.list.d/yggdrasil.list
sudo apt-get update
sudo apt-get install yggdrasil
```

Each package installs:

- `/usr/bin/toskar`
- `/usr/bin/toskarctl`
- bash, zsh, and fish completion for `toskarctl`
- `/usr/share/yggdrasil/web`
- a systemd service, `yggdrasil.service`

Open `http://127.0.0.1:7331` after the service starts.

Build the packages locally with `VERSION=1.1.0 ./scripts/build/package-core-release.sh`. That also writes the macOS archives used by the Homebrew formula.
