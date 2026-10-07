### Added

- Automations can run when files in a folder in your home folder, or one
  file, change. The task is told which files were added, changed, and
  removed, with the start of each new or changed text file. Choose "When a
  file or folder changes" under "Runs", or `toskarctl automations create
  --trigger folder --trigger-path ~/Documents/Invoices`.

### Fixed

- The Folder summary template now watches its folder, so its runs see the
  files; before, a run couldn't open files outside Toskar's workspace.
