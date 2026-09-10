# Shell Tab Completion

Two commands complete on the command line in this repository. `cargoship` generates its own completion scripts, and `mage` installs its own for targets and flags — but neither knows about the positional arguments the `generate:` targets take, which is what the overlay in this document adds.

## cargoship

Cobra generates these. Write one to wherever your shell keeps completions:

```console
cargoship completion fish > ~/.config/fish/completions/cargoship.fish
cargoship completion bash > ~/.local/share/bash-completion/completions/cargoship
cargoship completion zsh  > ~/.zsh/completions/_cargoship
```

`cargoship completion --help` covers powershell and the per-shell loading details. Nothing else in this document applies to `cargoship`.

## mage: targets and flags

Mage installs its own completion:

```console
mage -install fish       # also: bash, zsh, powershell, pwsh
```

This completes every target — the same list `mage -autocomplete` prints — and every mage flag, marking `-d`, `-w` and `--compile` as taking a path. Do this first; the overlay below only adds arguments.

## mage: positional arguments

Two targets take arguments mage has no way to know about:

```console
mage generate:exampleLine <distro> <minor>
mage generate:latestTag <distro> <minor>
```

Both arguments are readable off the tree: `<distro>` is a directory under `thirdparty-src/`, and `<minor>` is a directory under `thirdparty-src/<distro>/` with the underscore written as a dot (`v1_36` on disk, `v1.36` on the command line). Deriving them keeps completion correct as release lines are added and dropped, which a hardcoded list does not.

Three things worth knowing before installing any of these:

*   `mage -autocomplete` prints target names lowercased (`generate:exampleline`). Mage matches targets case-insensitively, so either spelling runs, but completion only ever produces the lowercase one. The scripts below lowercase before matching so a hand-typed `generate:exampleLine` still completes its arguments.
*   `generate:exampleLine` accepts `1.36` as well as `v1.36`; `generate:latestTag` passes the prefix straight through and needs the `v`. The scripts offer the `v` form, which both accept.
*   `thirdparty-src/` is read relative to the current directory, so the argument lists are empty outside a checkout.

### Fish

Fish is the one shell where the overlay coexists with `mage -install fish`, because it goes in `conf.d/` rather than the `mage.fish` that `-install` owns and overwrites.

File: `~/.config/fish/conf.d/cargoship-mage.fish`

```fish
# The mage target being completed, lowercased. Empty until one is typed.
function __cargoship_mage_target
    set -l tokens (string replace -r --filter '^([^-].*)' '$1' -- (commandline -pxc))
    test (count $tokens) -ge 2; and string lower -- $tokens[2]
end

# The distro argument, once it is on the line.
function __cargoship_mage_distro
    set -l tokens (string replace -r --filter '^([^-].*)' '$1' -- (commandline -pxc))
    test (count $tokens) -ge 3; and echo -- $tokens[3]
end

function __cargoship_mage_takes_args
    set -l target (__cargoship_mage_target)
    contains -- "$target" generate:exampleline generate:latesttag
end

# Subdirectory names of $argv[1], with underscores as dots.
function __cargoship_mage_subdirs --argument-names parent
    test -d "$parent"; or return
    for dir in $parent/*/
        string replace -r '^.*/([^/]+)/$' '$1' -- $dir | string replace _ .
    end
end

complete -c mage -f -n '__cargoship_mage_takes_args; and __fish_is_nth_token 2' \
    -a '(__cargoship_mage_subdirs thirdparty-src)'
complete -c mage -f -n '__cargoship_mage_takes_args; and __fish_is_nth_token 3' \
    -a '(__cargoship_mage_subdirs thirdparty-src/(__cargoship_mage_distro))'
```

Reload with `exec fish`, or `source ~/.config/fish/conf.d/cargoship-mage.fish` for the current shell.

One caveat: the completion written by `mage -install fish` registers its target list with no position condition, so at the distro and minor positions fish offers the full list of targets alongside the values this overlay adds. Typing a prefix (`rk`, `v1.3`) narrows it immediately. The alternative is to drop mage's own completion entirely, which costs you flag completion for `-l`, `-d`, `-v` and the rest, so the noise is the better trade.

### Bash

This one supersedes `mage -install bash` rather than adding to it — bash allows a single `complete -F` per command, so the overlay has to serve targets too, and mage's flag completion is lost. Install this file instead of running `mage -install bash`.

File: `~/.local/share/bash-completion/completions/mage`

```bash
_cargoship_mage() {
    declare -F _init_completion >/dev/null || return

    local cur prev words cword
    # -n : is load-bearing. Colon is in COMP_WORDBREAKS and _init_completion does not
    # exclude it by default, so without this "generate:exampleline" arrives as three
    # words and every index below is off.
    _init_completion -n : || return

    if ((cword == 1)); then
        COMPREPLY=($(compgen -W "$(mage -autocomplete 2>/dev/null)" -- "$cur"))
        # Readline still replaces only the text after the last colon.
        if declare -F _comp_ltrim_colon_completions >/dev/null; then
            _comp_ltrim_colon_completions "$cur"
        else
            __ltrim_colon_completions "$cur"
        fi
        return
    fi

    case "${words[1],,}" in
        generate:exampleline | generate:latesttag) ;;
        *) return ;;
    esac

    local dir
    case $cword in
        2) dir=thirdparty-src ;;
        3) dir=thirdparty-src/${words[2]} ;;
        *) return ;;
    esac
    [[ -d $dir ]] || return

    local entry
    local -a values=()
    for entry in "$dir"/*/; do
        [[ -d $entry ]] || continue
        entry=${entry%/}
        entry=${entry##*/}
        values+=("${entry//_/.}")
    done
    COMPREPLY=($(compgen -W "${values[*]}" -- "$cur"))
}
complete -F _cargoship_mage mage
```

Bash-completion autoloads the file on the first `mage<TAB>`; start a new shell to pick it up.

### Zsh

As with bash, this supersedes `mage -install zsh` and drops mage's flag completion.

File: `~/.zsh/completions/_mage`

```zsh
#compdef mage

local -a values
local dir

if (( CURRENT == 2 )); then
    values=(${(f)"$(mage -autocomplete 2>/dev/null)"})
    # compadd, not _describe: target names contain a colon, which _describe reads
    # as a value:description separator and would offer "generate" alone.
    compadd -a values
    return
fi

case "${words[2]:l}" in
    generate:exampleline | generate:latesttag) ;;
    *) return ;;
esac

case $CURRENT in
    3) dir=thirdparty-src ;;
    4) dir=thirdparty-src/${words[3]} ;;
    *) return ;;
esac

values=($dir/*(N/:t))
values=(${values//_/.})
compadd -a values
```

The directory has to be on `fpath` before `compinit` runs, so in `~/.zshrc`:

```zsh
fpath=(~/.zsh/completions $fpath)
autoload -U compinit && compinit
```
