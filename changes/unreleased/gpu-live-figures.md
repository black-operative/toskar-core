### Added

- `GET /api/v1/performance/live`: each computer's CPU, memory, and GPU figures (busy %, video memory, temperature, power) now, over the last hour, and as per-minute averages over the last day, on Linux, macOS, and Windows without root or admin rights. A figure a computer can't give is left out rather than shown as 0. Paired computers' figures are included (#317).
