### Fixed

- On Linux, the hardware check counts a memory limit set on Toskar's container or service (cgroup v2 `memory.max`, such as `docker run --memory` or systemd `MemoryMax=`), so model recommendations fit the memory Toskar may use instead of the whole machine's.
