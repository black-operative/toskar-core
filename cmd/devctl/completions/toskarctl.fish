# fish completion for toskarctl

complete -c toskarctl -f

complete -c toskarctl -n __fish_use_subcommand -a version -d 'Print the version, license, and source URL'
complete -c toskarctl -n __fish_use_subcommand -a about -d 'Print the version, license, and source URL'
complete -c toskarctl -n __fish_use_subcommand -a paths -d 'Print the data, model, runtime, log, and database directories'
complete -c toskarctl -n __fish_use_subcommand -a automations -d 'Manage scheduled automations on the daemon'
complete -c toskarctl -n __fish_use_subcommand -a mcp -d 'Connect an app such as Claude Desktop to Yggdrasil over MCP'
complete -c toskarctl -n __fish_use_subcommand -a join -d 'Join this computer to a Yggdrasil network'
complete -c toskarctl -n __fish_use_subcommand -a join-token -d 'Make, list, or revoke join tokens'
complete -c toskarctl -n __fish_use_subcommand -a network -d "Show this computer's network and paired computers"
complete -c toskarctl -n __fish_use_subcommand -a leave -d 'Leave the network and forget paired computers'
complete -c toskarctl -n __fish_use_subcommand -a completion -d 'Print a shell completion script'

complete -c toskarctl -n '__fish_seen_subcommand_from completion' -a 'bash zsh fish'
complete -c toskarctl -n '__fish_seen_subcommand_from mcp' -l url -r -d 'Yggdrasil address'
complete -c toskarctl -n '__fish_seen_subcommand_from join' -l server -r -d 'Address of the computer that made the command'
complete -c toskarctl -n '__fish_seen_subcommand_from join' -l token -r -d 'Join token'
complete -c toskarctl -n '__fish_seen_subcommand_from join' -l fingerprint -r -d "That computer's fingerprint"
complete -c toskarctl -n '__fish_seen_subcommand_from join' -l name -r -d 'Rename this computer as it joins'
complete -c toskarctl -n '__fish_seen_subcommand_from join' -l wait -r -d 'Wait for Yggdrasil to start, such as 60s'
complete -c toskarctl -n '__fish_seen_subcommand_from join-token; and not __fish_seen_subcommand_from create list revoke' -a 'create list revoke'
complete -c toskarctl -n '__fish_seen_subcommand_from join-token' -l ttl -r -d 'How long the token lasts, such as 15m'
complete -c toskarctl -n '__fish_seen_subcommand_from join join-token network leave' -l output -x -a 'text json' -d 'Output format'

set -l ygg_automation_cmds list get create update delete run pause resume hook
complete -c toskarctl -n "__fish_seen_subcommand_from automations; and not __fish_seen_subcommand_from $ygg_automation_cmds" -a "$ygg_automation_cmds"

set -l ygg_edit '__fish_seen_subcommand_from automations; and __fish_seen_subcommand_from create update'
complete -c toskarctl -n $ygg_edit -l name -r -d 'Automation name'
complete -c toskarctl -n $ygg_edit -l prompt -r -d 'Prompt to run'
complete -c toskarctl -n $ygg_edit -l profile -r -d 'Profile id'
complete -c toskarctl -n $ygg_edit -l model -r -d 'Installed model id'
complete -c toskarctl -n $ygg_edit -l schedule -x -a 'once daily weekly monthly interval cron manual' -d 'Schedule kind'
complete -c toskarctl -n $ygg_edit -l at -r -d 'HH:MM, several such as 08:00,17:00, or RFC3339'
complete -c toskarctl -n $ygg_edit -l every -r -d 'Interval duration, such as 6h'
complete -c toskarctl -n $ygg_edit -l weekday -x -a 'weekdays 0 1 2 3 4 5 6' -d 'Days 0-6 or names, such as 1,3,5 or weekdays'
complete -c toskarctl -n $ygg_edit -l day -r -d 'Day of the month, 1-31'
complete -c toskarctl -n $ygg_edit -l cron -r -d 'Cron expression'
complete -c toskarctl -n $ygg_edit -l zone -r -d 'IANA time zone'
complete -c toskarctl -n $ygg_edit -l tool -r -d 'Tool id allowed for this automation, repeatable'
complete -c toskarctl -n $ygg_edit -l notify -x -a 'always condition change none' -d 'Notification mode'
complete -c toskarctl -n $ygg_edit -l condition-kind -x -a 'threshold available significant' -d 'Condition kind'
complete -c toskarctl -n $ygg_edit -l condition-op -x -a 'below above' -d 'Condition operator'
complete -c toskarctl -n $ygg_edit -l condition-value -r -d 'Threshold value'
complete -c toskarctl -n $ygg_edit -l save-folder -r -a '(__fish_complete_directories)' -d 'Also save each result in this folder'
complete -c toskarctl -n $ygg_edit -l trigger -x -a 'page feed folder webhook none' -d 'Run only when it changed'
complete -c toskarctl -n $ygg_edit -l trigger-url -r -d 'The page or feed to watch'
complete -c toskarctl -n $ygg_edit -l trigger-path -r -d 'The folder or file to watch'
complete -c toskarctl -n $ygg_edit -l disabled -d 'Create the automation paused'

# yggctl, the name from before the rename, completes the same way.
complete -c yggctl -w toskarctl
