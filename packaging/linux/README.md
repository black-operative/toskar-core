# Linux packages

Release CI builds a `.deb` and `.rpm` for amd64 and arm64, and publishes an unsigned apt repository on the `apt` branch.

```bash
echo "deb [trusted=yes] https://raw.githubusercontent.com/yeixio/yggdrasil-core/apt stable main" | sudo tee /etc/apt/sources.list.d/yggdrasil.list
sudo apt-get update
sudo apt-get install toskar
```

Each package installs:

- `/usr/bin/toskar`
- `/usr/bin/toskarctl`
- bash, zsh, and fish completion for `toskarctl`
- `/usr/share/yggdrasil/web`
- a systemd service, `toskar.service`, also reachable as `yggdrasil.service`

The package was called `yggdrasil` before the rename. Each release also publishes a transitional `yggdrasil` deb that depends on `toskar`, so `apt-get upgrade` moves an existing install over; the rpm obsoletes `yggdrasil`, so `dnf upgrade` does the same. The system user `yggdrasil`, its home `/var/lib/yggdrasil`, and the data in it stay as they are.

Open `http://127.0.0.1:7331` after the service starts.

Build the packages locally with `VERSION=1.1.0 ./scripts/build/package-core-release.sh`. That also writes the macOS archives used by the Homebrew formula.
