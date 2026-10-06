# GPU acceleration

A model answers several times faster on a graphics card than on the CPU. Toskar uses the GPU when one is usable, and shows whether each model actually runs on it.

## Check whether a model is on the GPU

Each running model has a status on **Models › Running** and on the **Performance** overview:

| Status | Meaning |
| --- | --- |
| **GPU** (green) | The whole model is on the graphics card. |
| **Partly on GPU** (amber) | The model doesn't fit in the GPU's memory, so part of it runs on the slower CPU. A smaller model, or a smaller version of the same one, fits completely. |
| **CPU only** (red) | This computer has a GPU that isn't being used. See [Troubleshooting](#troubleshooting). |
| **CPU** (grey) | This computer has no GPU Toskar can use. That's expected, not an error. |

Select the status to see the device, how many of the model's layers are on the GPU, and the GPU memory it uses. Toskar reads all of this from llama.cpp's own report when it loads the model, so it shows what happened, not what the hardware could do.

**Diagnostics** has a **GPU acceleration** row with the same states. When something this computer needs for its GPU is missing, such as the Vulkan drivers, NVIDIA's driver, or permission to use the card, the row lists each piece with the command that fixes it here, ready to copy. `GET /api/v1/health` gives the same summary in `acceleration`, and `GET /api/v1/models/running` gives the details for each model (see [the API](api.md#gpu-acceleration)).

The **Performance** overview also shows each computer's live CPU and memory, and each GPU's use, video memory, temperature, and power, with the last hour as a line. **Activity** marks each reply GPU or CPU.

## What each computer needs

| Computer | GPU support | What Toskar installs | What you install |
| --- | --- | --- | --- |
| Mac with Apple silicon | Metal, built in | Everything | Nothing |
| Mac with Intel and an AMD card | Metal, built in | Everything | Nothing |
| Linux with AMD or Intel graphics | Vulkan | The `.deb` and `.rpm` recommend the Vulkan loader, Mesa's Vulkan drivers, `vulkan-tools`, and `pciutils`, so apt and dnf install them; the service user joins the `render` and `video` groups | Nothing, unless your package manager skips recommended packages |
| Linux with an NVIDIA card | Vulkan, through NVIDIA's driver | Everything else | NVIDIA's driver (the installer prints the command for your distribution) |
| Windows | Vulkan, through the card maker's driver | Everything else | The card maker's driver, if Windows is still using its basic display driver (the installer says so) |
| Docker | Vulkan, with the card passed in | Nothing | Build the `gpu` image and pass the card in ([below](#docker)) |

llama.cpp, which runs the models, is downloaded the first time a model starts. On Linux and Windows Toskar picks its Vulkan build when it finds a usable GPU, and its CPU build otherwise. If a GPU becomes usable later, such as after installing a driver, Toskar switches to the Vulkan build the next time it starts with no model running.

### Linux: AMD and Intel

The packages take care of this. To check, or to set it up by hand where recommended packages were skipped:

```bash
sudo apt-get install libvulkan1 mesa-vulkan-drivers vulkan-tools pciutils
```

On Fedora: `sudo dnf install vulkan-loader mesa-vulkan-drivers vulkan-tools pciutils`. On Arch: `sudo pacman -S vulkan-icd-loader vulkan-radeon vulkan-intel vulkan-tools pciutils`.

The card should appear as a GPU here:

```bash
vulkaninfo --summary | grep -E "deviceName|deviceType"
```

The system service runs as the `yggdrasil` user, which needs the `render` group to open the card. The package adds it; to check:

```bash
id -nG yggdrasil
```

If `render` is missing, add it and restart:

```bash
sudo usermod -a -G render,video yggdrasil && sudo systemctl restart toskar
```

Toskar run from your own login, such as the desktop app, already has access to the card.

### Linux: NVIDIA

Install NVIDIA's driver, then restart the computer:

- Ubuntu: `sudo ubuntu-drivers install`
- Fedora: `sudo dnf install akmod-nvidia`, from [RPM Fusion](https://rpmfusion.org/Howto/NVIDIA)
- Debian: `sudo apt-get install nvidia-driver`, from [non-free-firmware](https://wiki.debian.org/NvidiaGraphicsDrivers)

`nvidia-smi` should then list the card. The driver brings Vulkan with it, which Toskar uses.

### Windows

The card maker's driver provides Vulkan (`C:\Windows\System32\vulkan-1.dll`). Windows' basic display driver doesn't. If the installer says the card needs its driver, get it from [NVIDIA](https://www.nvidia.com/Download/index.aspx), [AMD](https://www.amd.com/en/support/download/drivers.html), or [Intel](https://www.intel.com/content/www/us/en/download-center/home.html), then restart Toskar.

### Docker

The default image runs on the CPU. For an AMD or Intel card, build the `gpu` target, which adds Vulkan and Mesa's drivers, and pass the card in:

```bash
docker build --target gpu -t toskar:gpu .
```

```bash
docker run -d -p 7331:7331 -e TOSKAR_API_KEY=… -v toskar-data:/data \
  --device /dev/dri --group-add "$(getent group render | cut -d: -f3)" toskar:gpu
```

For an NVIDIA card, install the [NVIDIA Container Toolkit](https://docs.nvidia.com/datacenter/cloud-native/container-toolkit/latest/install-guide.html) and replace the device options with `--gpus all -e NVIDIA_DRIVER_CAPABILITIES=compute,utility,graphics`.

Rootless Docker drops your supplementary groups inside the container, so `--group-add` doesn't help there. Let every user on the computer open render devices instead (they give GPU compute and rendering, not the display):

```bash
echo 'SUBSYSTEM=="drm", KERNEL=="renderD*", MODE="0666"' | sudo tee /etc/udev/rules.d/70-render-nodes.rules
sudo udevadm control --reload && sudo udevadm trigger --subsystem-match=drm
```

A model's status says whether the card was found inside the container. Without the card, the daemon doesn't count it, so it doesn't recommend models sized for memory it can't use.

## Troubleshooting

When a model isn't fully on the GPU, its status says why. Diagnostics shows the same reason.

**The installed llama.cpp runs on the CPU only** (`cpu_build`). It was installed before a usable GPU was found, such as before a driver was installed. Stop your models, then quit and reopen Toskar: it installs the Vulkan build when it starts with no model running.

**The model doesn't fit in the GPU's memory** (`gpu_memory`). Part of it runs on the CPU. Choose a smaller model, or a smaller version (a lower quantization) of the same one. Closing other programs that use the GPU, such as games, frees memory too. Models shows how well each model fits.

**llama.cpp couldn't use the GPU** (`gpu_unavailable`). The Vulkan build found no GPU it could use. On Linux, check the [Vulkan packages](#linux-amd-and-intel) and the [render group](#linux-amd-and-intel), or install [NVIDIA's driver](#linux-nvidia). On Windows, install the [card maker's driver](#windows). Then restart Toskar.

**This computer has no GPU Toskar can use** (`no_gpu`). Models run on the CPU, which works; smaller models answer faster. A computer with a GPU can join this one ([Connect your computers](clustering.md)) and run the models for it.
