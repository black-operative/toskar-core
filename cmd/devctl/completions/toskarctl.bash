# bash completion for toskarctl

_toskarctl() {
  local cur prev
  cur="${COMP_WORDS[COMP_CWORD]}"
  prev="${COMP_WORDS[COMP_CWORD-1]}"

  if [[ ${COMP_CWORD} -eq 1 ]]; then
    COMPREPLY=($(compgen -W "version about paths automations mcp join join-token network leave completion" -- "$cur"))
    return
  fi

  case "${COMP_WORDS[1]}" in
    mcp)
      if [[ "$cur" == -* ]]; then
        COMPREPLY=($(compgen -W "--url" -- "$cur"))
      fi
      ;;
    join)
      if [[ "$cur" == -* ]]; then
        COMPREPLY=($(compgen -W "--server --token --fingerprint --name --wait --output" -- "$cur"))
      fi
      ;;
    join-token)
      if [[ ${COMP_CWORD} -eq 2 ]]; then
        COMPREPLY=($(compgen -W "create list revoke" -- "$cur"))
      elif [[ "$cur" == -* ]]; then
        COMPREPLY=($(compgen -W "--ttl --output" -- "$cur"))
      fi
      ;;
    network|leave)
      if [[ "$cur" == -* ]]; then
        COMPREPLY=($(compgen -W "--output" -- "$cur"))
      fi
      ;;
    completion)
      if [[ ${COMP_CWORD} -eq 2 ]]; then
        COMPREPLY=($(compgen -W "bash zsh fish" -- "$cur"))
      fi
      ;;
    automations)
      if [[ ${COMP_CWORD} -eq 2 ]]; then
        COMPREPLY=($(compgen -W "list get create update delete run pause resume hook" -- "$cur"))
        return
      fi
      case "${COMP_WORDS[2]}" in
        create|update)
          case "$prev" in
            --schedule) COMPREPLY=($(compgen -W "once daily weekly monthly interval cron manual" -- "$cur")); return ;;
            --notify) COMPREPLY=($(compgen -W "always condition change none" -- "$cur")); return ;;
            --condition-kind) COMPREPLY=($(compgen -W "threshold available significant" -- "$cur")); return ;;
            --condition-op) COMPREPLY=($(compgen -W "below above" -- "$cur")); return ;;
            --after-when) COMPREPLY=($(compgen -W "succeeded notified" -- "$cur")); return ;;
            --trigger) COMPREPLY=($(compgen -W "page feed folder webhook after none" -- "$cur")); return ;;
            --weekday) COMPREPLY=($(compgen -W "weekdays 0 1 2 3 4 5 6" -- "$cur")); return ;;
          esac
          if [[ "$cur" == -* ]]; then
            COMPREPLY=($(compgen -W "--name --prompt --profile --model --schedule --at --every --weekday --day --cron --zone --tool --notify --condition-kind --condition-op --condition-value --save-folder --trigger --trigger-url --trigger-path --after --after-when --disabled" -- "$cur"))
          fi
          ;;
      esac
      ;;
  esac
}

complete -F _toskarctl toskarctl yggctl
