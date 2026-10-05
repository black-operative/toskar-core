### Fixed

- On Linux, AMD and Intel graphics cards are found from the kernel's card list, without `lspci`, and an AMD card's memory is counted, so model recommendations use it. A card counts only when its render device is present, so a container without the card isn't told it has one.
