### Fixed

- On Windows, a graphics card's memory is read from the driver's registry entry, so cards with more than 4 GB are no longer reported as having 4 GB or less, and model recommendations use the real figure. A card the registry has no size for falls back to the old lookup, and basic or virtual display adapters with no memory are left out.
