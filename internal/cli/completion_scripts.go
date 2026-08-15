package cli

import "fmt"

// CompletionScript returns the completion shim for the named shell. The shims
// are deliberately thin: at completion time they call `bulle __complete` on
// the installed binary, which answers from the live flag and command tables
// (including user-installed profiles), so an installed script never drifts
// from the binary that answers it.
func CompletionScript(shell string) (string, error) {
	switch shell {
	case "bash":
		return bashCompletion, nil
	case "zsh":
		return zshCompletion, nil
	case "fish":
		return fishCompletion, nil
	default:
		return "", fmt.Errorf("unsupported shell %q; use bash, zsh, or fish", shell)
	}
}

const bashCompletion = `# bash completion for bulle
# Install: source <(bulle completion bash) from ~/.bashrc
_bulle_complete() {
    local cur directive line response
    cur="${COMP_WORDS[COMP_CWORD]}"
    local args=("${COMP_WORDS[@]:1:COMP_CWORD}")
    response=$("${COMP_WORDS[0]}" __complete -- "${args[@]}" 2>/dev/null)
    directive=0
    COMPREPLY=()
    while IFS= read -r line; do
        if [[ "$line" == :* ]]; then
            directive="${line#:}"
            continue
        fi
        line="${line%%$'\t'*}"
        if [[ -n "$line" && "$line" == "$cur"* ]]; then
            COMPREPLY+=("$line")
        fi
    done <<<"$response"
    if [[ ${#COMPREPLY[@]} -eq 0 && "$directive" != "4" ]]; then
        compopt -o default 2>/dev/null
    fi
}
complete -o nosort -F _bulle_complete bulle 2>/dev/null ||
    complete -F _bulle_complete bulle
`

const zshCompletion = `#compdef bulle
# zsh completion for bulle
# Install: source <(bulle completion zsh) from ~/.zshrc (after compinit)
_bulle() {
    local line directive=0
    local -a completions descriptions
    local -a args
    args=("${words[@]:1:CURRENT-1}")
    while IFS= read -r line; do
        if [[ "$line" == :* ]]; then
            directive="${line#:}"
        elif [[ -n "$line" ]]; then
            completions+=("${line%%$'\t'*}")
            if [[ "$line" == *$'\t'* ]]; then
                descriptions+=("${line%%$'\t'*}:${line#*$'\t'}")
            else
                descriptions+=("$line")
            fi
        fi
    done < <("${words[1]}" __complete -- "${args[@]}" 2>/dev/null)
    if (( ${#completions} )); then
        _describe -o 'bulle' descriptions
    fi
    if [[ "$directive" != "4" ]]; then
        _files
    fi
}
if [[ "${funcstack[1]}" == "_bulle" ]]; then
    _bulle
else
    compdef _bulle bulle
fi
`

const fishCompletion = `# fish completion for bulle
# Install: bulle completion fish > ~/.config/fish/completions/bulle.fish
function __bulle_complete
    set -l tokens (commandline -opc)
    set -l cur (commandline -ct)
    set -l response ($tokens[1] __complete -- $tokens[2..-1] $cur 2>/dev/null)
    for line in $response
        if string match -q ':*' -- $line
            if test "$line" = ':0'
                __fish_complete_path $cur
            end
        else
            echo $line
        end
    end
end
complete -c bulle -f -a '(__bulle_complete)'
`
