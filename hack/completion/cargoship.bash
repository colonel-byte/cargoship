# Copyright 2026 colonel-byte
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

# bash completion for cargoship                            -*- shell-script -*-

__cargoship_debug()
{
    if [[ -n ${BASH_COMP_DEBUG_FILE:-} ]]; then
        echo "$*" >> "${BASH_COMP_DEBUG_FILE}"
    fi
}

# Homebrew on Macs have version 1.3 of bash-completion which doesn't include
# _init_completion. This is a very minimal version of that function.
__cargoship_init_completion()
{
    COMPREPLY=()
    _get_comp_words_by_ref "$@" cur prev words cword
}

__cargoship_index_of_word()
{
    local w word=$1
    shift
    index=0
    for w in "$@"; do
        [[ $w = "$word" ]] && return
        index=$((index+1))
    done
    index=-1
}

__cargoship_contains_word()
{
    local w word=$1; shift
    for w in "$@"; do
        [[ $w = "$word" ]] && return
    done
    return 1
}

__cargoship_handle_go_custom_completion()
{
    __cargoship_debug "${FUNCNAME[0]}: cur is ${cur}, words[*] is ${words[*]}, #words[@] is ${#words[@]}"

    local shellCompDirectiveError=1
    local shellCompDirectiveNoSpace=2
    local shellCompDirectiveNoFileComp=4
    local shellCompDirectiveFilterFileExt=8
    local shellCompDirectiveFilterDirs=16

    local out requestComp lastParam lastChar comp directive args

    # Prepare the command to request completions for the program.
    # Calling ${words[0]} instead of directly cargoship allows handling aliases
    args=("${words[@]:1}")
    # Disable ActiveHelp which is not supported for bash completion v1
    requestComp="CARGOSHIP_ACTIVE_HELP=0 ${words[0]} __completeNoDesc ${args[*]}"

    lastParam=${words[$((${#words[@]}-1))]}
    lastChar=${lastParam:$((${#lastParam}-1)):1}
    __cargoship_debug "${FUNCNAME[0]}: lastParam ${lastParam}, lastChar ${lastChar}"

    if [ -z "${cur}" ] && [ "${lastChar}" != "=" ]; then
        # If the last parameter is complete (there is a space following it)
        # We add an extra empty parameter so we can indicate this to the go method.
        __cargoship_debug "${FUNCNAME[0]}: Adding extra empty parameter"
        requestComp="${requestComp} \"\""
    fi

    __cargoship_debug "${FUNCNAME[0]}: calling ${requestComp}"
    # Use eval to handle any environment variables and such
    out=$(eval "${requestComp}" 2>/dev/null)

    # Extract the directive integer at the very end of the output following a colon (:)
    directive=${out##*:}
    # Remove the directive
    out=${out%:*}
    if [ "${directive}" = "${out}" ]; then
        # There is not directive specified
        directive=0
    fi
    __cargoship_debug "${FUNCNAME[0]}: the completion directive is: ${directive}"
    __cargoship_debug "${FUNCNAME[0]}: the completions are: ${out}"

    if [ $((directive & shellCompDirectiveError)) -ne 0 ]; then
        # Error code.  No completion.
        __cargoship_debug "${FUNCNAME[0]}: received error from custom completion go code"
        return
    else
        if [ $((directive & shellCompDirectiveNoSpace)) -ne 0 ]; then
            if [[ $(type -t compopt) = "builtin" ]]; then
                __cargoship_debug "${FUNCNAME[0]}: activating no space"
                compopt -o nospace
            fi
        fi
        if [ $((directive & shellCompDirectiveNoFileComp)) -ne 0 ]; then
            if [[ $(type -t compopt) = "builtin" ]]; then
                __cargoship_debug "${FUNCNAME[0]}: activating no file completion"
                compopt +o default
            fi
        fi
    fi

    if [ $((directive & shellCompDirectiveFilterFileExt)) -ne 0 ]; then
        # File extension filtering
        local fullFilter filter filteringCmd
        # Do not use quotes around the $out variable or else newline
        # characters will be kept.
        for filter in ${out}; do
            fullFilter+="$filter|"
        done

        filteringCmd="_filedir $fullFilter"
        __cargoship_debug "File filtering command: $filteringCmd"
        $filteringCmd
    elif [ $((directive & shellCompDirectiveFilterDirs)) -ne 0 ]; then
        # File completion for directories only
        local subdir
        # Use printf to strip any trailing newline
        subdir=$(printf "%s" "${out}")
        if [ -n "$subdir" ]; then
            __cargoship_debug "Listing directories in $subdir"
            __cargoship_handle_subdirs_in_dir_flag "$subdir"
        else
            __cargoship_debug "Listing directories in ."
            _filedir -d
        fi
    else
        while IFS='' read -r comp; do
            COMPREPLY+=("$comp")
        done < <(compgen -W "${out}" -- "$cur")
    fi
}

__cargoship_handle_reply()
{
    __cargoship_debug "${FUNCNAME[0]}"
    local comp
    case $cur in
        -*)
            if [[ $(type -t compopt) = "builtin" ]]; then
                compopt -o nospace
            fi
            local allflags
            if [ ${#must_have_one_flag[@]} -ne 0 ]; then
                allflags=("${must_have_one_flag[@]}")
            else
                allflags=("${flags[*]} ${two_word_flags[*]}")
            fi
            while IFS='' read -r comp; do
                COMPREPLY+=("$comp")
            done < <(compgen -W "${allflags[*]}" -- "$cur")
            if [[ $(type -t compopt) = "builtin" ]]; then
                [[ "${COMPREPLY[0]}" == *= ]] || compopt +o nospace
            fi

            # complete after --flag=abc
            if [[ $cur == *=* ]]; then
                if [[ $(type -t compopt) = "builtin" ]]; then
                    compopt +o nospace
                fi

                local index flag
                flag="${cur%=*}"
                __cargoship_index_of_word "${flag}" "${flags_with_completion[@]}"
                COMPREPLY=()
                if [[ ${index} -ge 0 ]]; then
                    PREFIX=""
                    cur="${cur#*=}"
                    ${flags_completion[${index}]}
                    if [ -n "${ZSH_VERSION:-}" ]; then
                        # zsh completion needs --flag= prefix
                        eval "COMPREPLY=( \"\${COMPREPLY[@]/#/${flag}=}\" )"
                    fi
                fi
            fi

            if [[ -z "${flag_parsing_disabled}" ]]; then
                # If flag parsing is enabled, we have completed the flags and can return.
                # If flag parsing is disabled, we may not know all (or any) of the flags, so we fallthrough
                # to possibly call handle_go_custom_completion.
                return 0;
            fi
            ;;
    esac

    # check if we are handling a flag with special work handling
    local index
    __cargoship_index_of_word "${prev}" "${flags_with_completion[@]}"
    if [[ ${index} -ge 0 ]]; then
        ${flags_completion[${index}]}
        return
    fi

    # we are parsing a flag and don't have a special handler, no completion
    if [[ ${cur} != "${words[cword]}" ]]; then
        return
    fi

    local completions
    completions=("${commands[@]}")
    if [[ ${#must_have_one_noun[@]} -ne 0 ]]; then
        completions+=("${must_have_one_noun[@]}")
    elif [[ -n "${has_completion_function}" ]]; then
        # if a go completion function is provided, defer to that function
        __cargoship_handle_go_custom_completion
    fi
    if [[ ${#must_have_one_flag[@]} -ne 0 ]]; then
        completions+=("${must_have_one_flag[@]}")
    fi
    while IFS='' read -r comp; do
        COMPREPLY+=("$comp")
    done < <(compgen -W "${completions[*]}" -- "$cur")

    if [[ ${#COMPREPLY[@]} -eq 0 && ${#noun_aliases[@]} -gt 0 && ${#must_have_one_noun[@]} -ne 0 ]]; then
        while IFS='' read -r comp; do
            COMPREPLY+=("$comp")
        done < <(compgen -W "${noun_aliases[*]}" -- "$cur")
    fi

    if [[ ${#COMPREPLY[@]} -eq 0 ]]; then
        if declare -F __cargoship_custom_func >/dev/null; then
            # try command name qualified custom func
            __cargoship_custom_func
        else
            # otherwise fall back to unqualified for compatibility
            declare -F __custom_func >/dev/null && __custom_func
        fi
    fi

    # available in bash-completion >= 2, not always present on macOS
    if declare -F __ltrim_colon_completions >/dev/null; then
        __ltrim_colon_completions "$cur"
    fi

    # If there is only 1 completion and it is a flag with an = it will be completed
    # but we don't want a space after the =
    if [[ "${#COMPREPLY[@]}" -eq "1" ]] && [[ $(type -t compopt) = "builtin" ]] && [[ "${COMPREPLY[0]}" == --*= ]]; then
       compopt -o nospace
    fi
}

# The arguments should be in the form "ext1|ext2|extn"
__cargoship_handle_filename_extension_flag()
{
    local ext="$1"
    _filedir "@(${ext})"
}

__cargoship_handle_subdirs_in_dir_flag()
{
    local dir="$1"
    pushd "${dir}" >/dev/null 2>&1 && _filedir -d && popd >/dev/null 2>&1 || return
}

__cargoship_handle_flag()
{
    __cargoship_debug "${FUNCNAME[0]}: c is $c words[c] is ${words[c]}"

    # if a command required a flag, and we found it, unset must_have_one_flag()
    local flagname=${words[c]}
    local flagvalue=""
    # if the word contained an =
    if [[ ${words[c]} == *"="* ]]; then
        flagvalue=${flagname#*=} # take in as flagvalue after the =
        flagname=${flagname%=*} # strip everything after the =
        flagname="${flagname}=" # but put the = back
    fi
    __cargoship_debug "${FUNCNAME[0]}: looking for ${flagname}"
    if __cargoship_contains_word "${flagname}" "${must_have_one_flag[@]}"; then
        must_have_one_flag=()
    fi

    # if you set a flag which only applies to this command, don't show subcommands
    if __cargoship_contains_word "${flagname}" "${local_nonpersistent_flags[@]}"; then
      commands=()
    fi

    # keep flag value with flagname as flaghash
    # flaghash variable is an associative array which is only supported in bash > 3.
    if [[ -z "${BASH_VERSION:-}" || "${BASH_VERSINFO[0]:-}" -gt 3 ]]; then
        if [ -n "${flagvalue}" ] ; then
            flaghash[${flagname}]=${flagvalue}
        elif [ -n "${words[ $((c+1)) ]}" ] ; then
            flaghash[${flagname}]=${words[ $((c+1)) ]}
        else
            flaghash[${flagname}]="true" # pad "true" for bool flag
        fi
    fi

    # skip the argument to a two word flag
    if [[ ${words[c]} != *"="* ]] && __cargoship_contains_word "${words[c]}" "${two_word_flags[@]}"; then
        __cargoship_debug "${FUNCNAME[0]}: found a flag ${words[c]}, skip the next argument"
        c=$((c+1))
        # if we are looking for a flags value, don't show commands
        if [[ $c -eq $cword ]]; then
            commands=()
        fi
    fi

    c=$((c+1))

}

__cargoship_handle_noun()
{
    __cargoship_debug "${FUNCNAME[0]}: c is $c words[c] is ${words[c]}"

    if __cargoship_contains_word "${words[c]}" "${must_have_one_noun[@]}"; then
        must_have_one_noun=()
    elif __cargoship_contains_word "${words[c]}" "${noun_aliases[@]}"; then
        must_have_one_noun=()
    fi

    nouns+=("${words[c]}")
    c=$((c+1))
}

__cargoship_handle_command()
{
    __cargoship_debug "${FUNCNAME[0]}: c is $c words[c] is ${words[c]}"

    local next_command
    if [[ -n ${last_command} ]]; then
        next_command="_${last_command}_${words[c]//:/__}"
    else
        if [[ $c -eq 0 ]]; then
            next_command="_cargoship_root_command"
        else
            next_command="_${words[c]//:/__}"
        fi
    fi
    c=$((c+1))
    __cargoship_debug "${FUNCNAME[0]}: looking for ${next_command}"
    declare -F "$next_command" >/dev/null && $next_command
}

__cargoship_handle_word()
{
    if [[ $c -ge $cword ]]; then
        __cargoship_handle_reply
        return
    fi
    __cargoship_debug "${FUNCNAME[0]}: c is $c words[c] is ${words[c]}"
    if [[ "${words[c]}" == -* ]]; then
        __cargoship_handle_flag
    elif __cargoship_contains_word "${words[c]}" "${commands[@]}"; then
        __cargoship_handle_command
    elif [[ $c -eq 0 ]]; then
        __cargoship_handle_command
    elif __cargoship_contains_word "${words[c]}" "${command_aliases[@]}"; then
        # aliashash variable is an associative array which is only supported in bash > 3.
        if [[ -z "${BASH_VERSION:-}" || "${BASH_VERSINFO[0]:-}" -gt 3 ]]; then
            words[c]=${aliashash[${words[c]}]}
            __cargoship_handle_command
        else
            __cargoship_handle_noun
        fi
    else
        __cargoship_handle_noun
    fi
    __cargoship_handle_word
}

_cargoship_apply()
{
    last_command="cargoship_apply"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--age-identity-file=")
    two_word_flags+=("--age-identity-file")
    local_nonpersistent_flags+=("--age-identity-file")
    local_nonpersistent_flags+=("--age-identity-file=")
    flags+=("--age-recipient=")
    two_word_flags+=("--age-recipient")
    local_nonpersistent_flags+=("--age-recipient")
    local_nonpersistent_flags+=("--age-recipient=")
    flags+=("--age-recipients-file=")
    two_word_flags+=("--age-recipients-file")
    local_nonpersistent_flags+=("--age-recipients-file")
    local_nonpersistent_flags+=("--age-recipients-file=")
    flags+=("--allow-unmanaged-nodes")
    local_nonpersistent_flags+=("--allow-unmanaged-nodes")
    flags+=("--architecture=")
    two_word_flags+=("--architecture")
    flags_with_completion+=("--architecture")
    flags_completion+=("__cargoship_handle_go_custom_completion")
    two_word_flags+=("-a")
    flags_with_completion+=("-a")
    flags_completion+=("__cargoship_handle_go_custom_completion")
    local_nonpersistent_flags+=("--architecture")
    local_nonpersistent_flags+=("--architecture=")
    local_nonpersistent_flags+=("-a")
    flags+=("--certificate-identity=")
    two_word_flags+=("--certificate-identity")
    local_nonpersistent_flags+=("--certificate-identity")
    local_nonpersistent_flags+=("--certificate-identity=")
    flags+=("--certificate-identity-regexp=")
    two_word_flags+=("--certificate-identity-regexp")
    local_nonpersistent_flags+=("--certificate-identity-regexp")
    local_nonpersistent_flags+=("--certificate-identity-regexp=")
    flags+=("--certificate-oidc-issuer=")
    two_word_flags+=("--certificate-oidc-issuer")
    local_nonpersistent_flags+=("--certificate-oidc-issuer")
    local_nonpersistent_flags+=("--certificate-oidc-issuer=")
    flags+=("--certificate-oidc-issuer-regexp=")
    two_word_flags+=("--certificate-oidc-issuer-regexp")
    local_nonpersistent_flags+=("--certificate-oidc-issuer-regexp")
    local_nonpersistent_flags+=("--certificate-oidc-issuer-regexp=")
    flags+=("--concurrency=")
    two_word_flags+=("--concurrency")
    two_word_flags+=("-c")
    local_nonpersistent_flags+=("--concurrency")
    local_nonpersistent_flags+=("--concurrency=")
    local_nonpersistent_flags+=("-c")
    flags+=("--config=")
    two_word_flags+=("--config")
    local_nonpersistent_flags+=("--config")
    local_nonpersistent_flags+=("--config=")
    flags+=("--confirm")
    local_nonpersistent_flags+=("--confirm")
    flags+=("--dry-run")
    local_nonpersistent_flags+=("--dry-run")
    flags+=("--fapolicyd")
    flags+=("-f")
    local_nonpersistent_flags+=("--fapolicyd")
    local_nonpersistent_flags+=("-f")
    flags+=("--firewall")
    flags+=("-F")
    local_nonpersistent_flags+=("--firewall")
    local_nonpersistent_flags+=("-F")
    flags+=("--hosts")
    flags+=("-H")
    local_nonpersistent_flags+=("--hosts")
    local_nonpersistent_flags+=("-H")
    flags+=("--insecure-ignore-tlog")
    local_nonpersistent_flags+=("--insecure-ignore-tlog")
    flags+=("--key=")
    two_word_flags+=("--key")
    two_word_flags+=("-k")
    local_nonpersistent_flags+=("--key")
    local_nonpersistent_flags+=("--key=")
    local_nonpersistent_flags+=("-k")
    flags+=("--kubeconfig=")
    two_word_flags+=("--kubeconfig")
    local_nonpersistent_flags+=("--kubeconfig")
    local_nonpersistent_flags+=("--kubeconfig=")
    flags+=("--label-nodes")
    local_nonpersistent_flags+=("--label-nodes")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")
    local_nonpersistent_flags+=("--timeout")
    local_nonpersistent_flags+=("--timeout=")
    flags+=("--tmpdir=")
    two_word_flags+=("--tmpdir")
    local_nonpersistent_flags+=("--tmpdir")
    local_nonpersistent_flags+=("--tmpdir=")
    flags+=("--trusted-root=")
    two_word_flags+=("--trusted-root")
    local_nonpersistent_flags+=("--trusted-root")
    local_nonpersistent_flags+=("--trusted-root=")
    flags+=("--update-kubeconfig")
    local_nonpersistent_flags+=("--update-kubeconfig")
    flags+=("--use-signed-timestamps")
    local_nonpersistent_flags+=("--use-signed-timestamps")
    flags+=("--values=")
    two_word_flags+=("--values")
    local_nonpersistent_flags+=("--values")
    local_nonpersistent_flags+=("--values=")
    flags+=("--vault-password-file=")
    two_word_flags+=("--vault-password-file")
    local_nonpersistent_flags+=("--vault-password-file")
    local_nonpersistent_flags+=("--vault-password-file=")
    flags+=("--verify=")
    two_word_flags+=("--verify")
    local_nonpersistent_flags+=("--verify")
    local_nonpersistent_flags+=("--verify=")
    flags+=("--work-concurrency=")
    two_word_flags+=("--work-concurrency")
    two_word_flags+=("-w")
    local_nonpersistent_flags+=("--work-concurrency")
    local_nonpersistent_flags+=("--work-concurrency=")
    local_nonpersistent_flags+=("-w")
    flags+=("--zarf-cache=")
    two_word_flags+=("--zarf-cache")
    local_nonpersistent_flags+=("--zarf-cache")
    local_nonpersistent_flags+=("--zarf-cache=")
    flags+=("--log-file")
    flags+=("--log-format=")
    two_word_flags+=("--log-format")
    flags_with_completion+=("--log-format")
    flags_completion+=("__cargoship_handle_go_custom_completion")
    two_word_flags+=("-L")
    flags_with_completion+=("-L")
    flags_completion+=("__cargoship_handle_go_custom_completion")
    flags+=("--log-level=")
    two_word_flags+=("--log-level")
    flags_with_completion+=("--log-level")
    flags_completion+=("__cargoship_handle_go_custom_completion")
    two_word_flags+=("-l")
    flags_with_completion+=("-l")
    flags_completion+=("__cargoship_handle_go_custom_completion")
    flags+=("--no-color")

    must_have_one_flag=()
    must_have_one_flag+=("--config=")
    must_have_one_noun=()
    noun_aliases=()
}

_cargoship_create()
{
    last_command="cargoship_create"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--architecture=")
    two_word_flags+=("--architecture")
    flags_with_completion+=("--architecture")
    flags_completion+=("__cargoship_handle_go_custom_completion")
    two_word_flags+=("-a")
    flags_with_completion+=("-a")
    flags_completion+=("__cargoship_handle_go_custom_completion")
    local_nonpersistent_flags+=("--architecture")
    local_nonpersistent_flags+=("--architecture=")
    local_nonpersistent_flags+=("-a")
    flags+=("--confirm")
    flags+=("-c")
    local_nonpersistent_flags+=("--confirm")
    local_nonpersistent_flags+=("-c")
    flags+=("--insecure-skip-tls-verify")
    local_nonpersistent_flags+=("--insecure-skip-tls-verify")
    flags+=("--oci-concurrency=")
    two_word_flags+=("--oci-concurrency")
    flags_with_completion+=("--oci-concurrency")
    flags_completion+=("__cargoship_handle_go_custom_completion")
    local_nonpersistent_flags+=("--oci-concurrency")
    local_nonpersistent_flags+=("--oci-concurrency=")
    flags+=("--output=")
    two_word_flags+=("--output")
    two_word_flags+=("-o")
    local_nonpersistent_flags+=("--output")
    local_nonpersistent_flags+=("--output=")
    local_nonpersistent_flags+=("-o")
    flags+=("--plain-http")
    local_nonpersistent_flags+=("--plain-http")
    flags+=("--registry-override=")
    two_word_flags+=("--registry-override")
    local_nonpersistent_flags+=("--registry-override")
    local_nonpersistent_flags+=("--registry-override=")
    flags+=("--reproducible")
    local_nonpersistent_flags+=("--reproducible")
    flags+=("--signing-key=")
    two_word_flags+=("--signing-key")
    local_nonpersistent_flags+=("--signing-key")
    local_nonpersistent_flags+=("--signing-key=")
    flags+=("--signing-key-pass=")
    two_word_flags+=("--signing-key-pass")
    local_nonpersistent_flags+=("--signing-key-pass")
    local_nonpersistent_flags+=("--signing-key-pass=")
    flags+=("--tag=")
    two_word_flags+=("--tag")
    local_nonpersistent_flags+=("--tag")
    local_nonpersistent_flags+=("--tag=")
    flags+=("--tmpdir=")
    two_word_flags+=("--tmpdir")
    local_nonpersistent_flags+=("--tmpdir")
    local_nonpersistent_flags+=("--tmpdir=")
    flags+=("--zarf-cache=")
    two_word_flags+=("--zarf-cache")
    local_nonpersistent_flags+=("--zarf-cache")
    local_nonpersistent_flags+=("--zarf-cache=")
    flags+=("--log-file")
    flags+=("--log-format=")
    two_word_flags+=("--log-format")
    flags_with_completion+=("--log-format")
    flags_completion+=("__cargoship_handle_go_custom_completion")
    two_word_flags+=("-L")
    flags_with_completion+=("-L")
    flags_completion+=("__cargoship_handle_go_custom_completion")
    flags+=("--log-level=")
    two_word_flags+=("--log-level")
    flags_with_completion+=("--log-level")
    flags_completion+=("__cargoship_handle_go_custom_completion")
    two_word_flags+=("-l")
    flags_with_completion+=("-l")
    flags_completion+=("__cargoship_handle_go_custom_completion")
    flags+=("--no-color")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_cargoship_engine-config-sync()
{
    last_command="cargoship_engine-config-sync"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--age-identity-file=")
    two_word_flags+=("--age-identity-file")
    local_nonpersistent_flags+=("--age-identity-file")
    local_nonpersistent_flags+=("--age-identity-file=")
    flags+=("--age-recipient=")
    two_word_flags+=("--age-recipient")
    local_nonpersistent_flags+=("--age-recipient")
    local_nonpersistent_flags+=("--age-recipient=")
    flags+=("--age-recipients-file=")
    two_word_flags+=("--age-recipients-file")
    local_nonpersistent_flags+=("--age-recipients-file")
    local_nonpersistent_flags+=("--age-recipients-file=")
    flags+=("--architecture=")
    two_word_flags+=("--architecture")
    flags_with_completion+=("--architecture")
    flags_completion+=("__cargoship_handle_go_custom_completion")
    two_word_flags+=("-a")
    flags_with_completion+=("-a")
    flags_completion+=("__cargoship_handle_go_custom_completion")
    local_nonpersistent_flags+=("--architecture")
    local_nonpersistent_flags+=("--architecture=")
    local_nonpersistent_flags+=("-a")
    flags+=("--certificate-identity=")
    two_word_flags+=("--certificate-identity")
    local_nonpersistent_flags+=("--certificate-identity")
    local_nonpersistent_flags+=("--certificate-identity=")
    flags+=("--certificate-identity-regexp=")
    two_word_flags+=("--certificate-identity-regexp")
    local_nonpersistent_flags+=("--certificate-identity-regexp")
    local_nonpersistent_flags+=("--certificate-identity-regexp=")
    flags+=("--certificate-oidc-issuer=")
    two_word_flags+=("--certificate-oidc-issuer")
    local_nonpersistent_flags+=("--certificate-oidc-issuer")
    local_nonpersistent_flags+=("--certificate-oidc-issuer=")
    flags+=("--certificate-oidc-issuer-regexp=")
    two_word_flags+=("--certificate-oidc-issuer-regexp")
    local_nonpersistent_flags+=("--certificate-oidc-issuer-regexp")
    local_nonpersistent_flags+=("--certificate-oidc-issuer-regexp=")
    flags+=("--concurrency=")
    two_word_flags+=("--concurrency")
    two_word_flags+=("-c")
    local_nonpersistent_flags+=("--concurrency")
    local_nonpersistent_flags+=("--concurrency=")
    local_nonpersistent_flags+=("-c")
    flags+=("--config=")
    two_word_flags+=("--config")
    local_nonpersistent_flags+=("--config")
    local_nonpersistent_flags+=("--config=")
    flags+=("--confirm")
    local_nonpersistent_flags+=("--confirm")
    flags+=("--dry-run")
    local_nonpersistent_flags+=("--dry-run")
    flags+=("--insecure-ignore-tlog")
    local_nonpersistent_flags+=("--insecure-ignore-tlog")
    flags+=("--key=")
    two_word_flags+=("--key")
    two_word_flags+=("-k")
    local_nonpersistent_flags+=("--key")
    local_nonpersistent_flags+=("--key=")
    local_nonpersistent_flags+=("-k")
    flags+=("--kubeconfig=")
    two_word_flags+=("--kubeconfig")
    local_nonpersistent_flags+=("--kubeconfig")
    local_nonpersistent_flags+=("--kubeconfig=")
    flags+=("--label-nodes")
    local_nonpersistent_flags+=("--label-nodes")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")
    local_nonpersistent_flags+=("--timeout")
    local_nonpersistent_flags+=("--timeout=")
    flags+=("--tmpdir=")
    two_word_flags+=("--tmpdir")
    local_nonpersistent_flags+=("--tmpdir")
    local_nonpersistent_flags+=("--tmpdir=")
    flags+=("--trusted-root=")
    two_word_flags+=("--trusted-root")
    local_nonpersistent_flags+=("--trusted-root")
    local_nonpersistent_flags+=("--trusted-root=")
    flags+=("--update-kubeconfig")
    local_nonpersistent_flags+=("--update-kubeconfig")
    flags+=("--use-signed-timestamps")
    local_nonpersistent_flags+=("--use-signed-timestamps")
    flags+=("--values=")
    two_word_flags+=("--values")
    local_nonpersistent_flags+=("--values")
    local_nonpersistent_flags+=("--values=")
    flags+=("--vault-password-file=")
    two_word_flags+=("--vault-password-file")
    local_nonpersistent_flags+=("--vault-password-file")
    local_nonpersistent_flags+=("--vault-password-file=")
    flags+=("--verify=")
    two_word_flags+=("--verify")
    local_nonpersistent_flags+=("--verify")
    local_nonpersistent_flags+=("--verify=")
    flags+=("--work-concurrency=")
    two_word_flags+=("--work-concurrency")
    two_word_flags+=("-w")
    local_nonpersistent_flags+=("--work-concurrency")
    local_nonpersistent_flags+=("--work-concurrency=")
    local_nonpersistent_flags+=("-w")
    flags+=("--zarf-cache=")
    two_word_flags+=("--zarf-cache")
    local_nonpersistent_flags+=("--zarf-cache")
    local_nonpersistent_flags+=("--zarf-cache=")
    flags+=("--log-file")
    flags+=("--log-format=")
    two_word_flags+=("--log-format")
    flags_with_completion+=("--log-format")
    flags_completion+=("__cargoship_handle_go_custom_completion")
    two_word_flags+=("-L")
    flags_with_completion+=("-L")
    flags_completion+=("__cargoship_handle_go_custom_completion")
    flags+=("--log-level=")
    two_word_flags+=("--log-level")
    flags_with_completion+=("--log-level")
    flags_completion+=("__cargoship_handle_go_custom_completion")
    two_word_flags+=("-l")
    flags_with_completion+=("-l")
    flags_completion+=("__cargoship_handle_go_custom_completion")
    flags+=("--no-color")

    must_have_one_flag=()
    must_have_one_flag+=("--config=")
    must_have_one_noun=()
    noun_aliases=()
}

_cargoship_inventory_from-ansible()
{
    last_command="cargoship_inventory_from-ansible"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--loadbalancer=")
    two_word_flags+=("--loadbalancer")
    local_nonpersistent_flags+=("--loadbalancer")
    local_nonpersistent_flags+=("--loadbalancer=")
    flags+=("--name=")
    two_word_flags+=("--name")
    local_nonpersistent_flags+=("--name")
    local_nonpersistent_flags+=("--name=")
    flags+=("--output=")
    two_word_flags+=("--output")
    two_word_flags+=("-o")
    local_nonpersistent_flags+=("--output")
    local_nonpersistent_flags+=("--output=")
    local_nonpersistent_flags+=("-o")
    flags+=("--log-file")
    flags+=("--log-format=")
    two_word_flags+=("--log-format")
    flags_with_completion+=("--log-format")
    flags_completion+=("__cargoship_handle_go_custom_completion")
    two_word_flags+=("-L")
    flags_with_completion+=("-L")
    flags_completion+=("__cargoship_handle_go_custom_completion")
    flags+=("--log-level=")
    two_word_flags+=("--log-level")
    flags_with_completion+=("--log-level")
    flags_completion+=("__cargoship_handle_go_custom_completion")
    two_word_flags+=("-l")
    flags_with_completion+=("-l")
    flags_completion+=("__cargoship_handle_go_custom_completion")
    flags+=("--no-color")
    flags+=("--vault-password-file=")
    two_word_flags+=("--vault-password-file")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_cargoship_inventory()
{
    last_command="cargoship_inventory"

    command_aliases=()

    commands=()
    commands+=("from-ansible")

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--age-identity-file=")
    two_word_flags+=("--age-identity-file")
    local_nonpersistent_flags+=("--age-identity-file")
    local_nonpersistent_flags+=("--age-identity-file=")
    flags+=("--age-recipient=")
    two_word_flags+=("--age-recipient")
    local_nonpersistent_flags+=("--age-recipient")
    local_nonpersistent_flags+=("--age-recipient=")
    flags+=("--age-recipients-file=")
    two_word_flags+=("--age-recipients-file")
    local_nonpersistent_flags+=("--age-recipients-file")
    local_nonpersistent_flags+=("--age-recipients-file=")
    flags+=("--vault-password-file=")
    two_word_flags+=("--vault-password-file")
    flags+=("--log-file")
    flags+=("--log-format=")
    two_word_flags+=("--log-format")
    flags_with_completion+=("--log-format")
    flags_completion+=("__cargoship_handle_go_custom_completion")
    two_word_flags+=("-L")
    flags_with_completion+=("-L")
    flags_completion+=("__cargoship_handle_go_custom_completion")
    flags+=("--log-level=")
    two_word_flags+=("--log-level")
    flags_with_completion+=("--log-level")
    flags_completion+=("__cargoship_handle_go_custom_completion")
    two_word_flags+=("-l")
    flags_with_completion+=("-l")
    flags_completion+=("__cargoship_handle_go_custom_completion")
    flags+=("--no-color")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_cargoship_kube-config()
{
    last_command="cargoship_kube-config"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--config=")
    two_word_flags+=("--config")
    local_nonpersistent_flags+=("--config")
    local_nonpersistent_flags+=("--config=")
    flags+=("--distro=")
    two_word_flags+=("--distro")
    two_word_flags+=("-D")
    local_nonpersistent_flags+=("--distro")
    local_nonpersistent_flags+=("--distro=")
    local_nonpersistent_flags+=("-D")
    flags+=("--kubeconfig=")
    two_word_flags+=("--kubeconfig")
    local_nonpersistent_flags+=("--kubeconfig")
    local_nonpersistent_flags+=("--kubeconfig=")
    flags+=("--log-file")
    flags+=("--log-format=")
    two_word_flags+=("--log-format")
    flags_with_completion+=("--log-format")
    flags_completion+=("__cargoship_handle_go_custom_completion")
    two_word_flags+=("-L")
    flags_with_completion+=("-L")
    flags_completion+=("__cargoship_handle_go_custom_completion")
    flags+=("--log-level=")
    two_word_flags+=("--log-level")
    flags_with_completion+=("--log-level")
    flags_completion+=("__cargoship_handle_go_custom_completion")
    two_word_flags+=("-l")
    flags_with_completion+=("-l")
    flags_completion+=("__cargoship_handle_go_custom_completion")
    flags+=("--no-color")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_cargoship_prepare()
{
    last_command="cargoship_prepare"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--architecture=")
    two_word_flags+=("--architecture")
    flags_with_completion+=("--architecture")
    flags_completion+=("__cargoship_handle_go_custom_completion")
    two_word_flags+=("-a")
    flags_with_completion+=("-a")
    flags_completion+=("__cargoship_handle_go_custom_completion")
    local_nonpersistent_flags+=("--architecture")
    local_nonpersistent_flags+=("--architecture=")
    local_nonpersistent_flags+=("-a")
    flags+=("--certificate-identity=")
    two_word_flags+=("--certificate-identity")
    local_nonpersistent_flags+=("--certificate-identity")
    local_nonpersistent_flags+=("--certificate-identity=")
    flags+=("--certificate-identity-regexp=")
    two_word_flags+=("--certificate-identity-regexp")
    local_nonpersistent_flags+=("--certificate-identity-regexp")
    local_nonpersistent_flags+=("--certificate-identity-regexp=")
    flags+=("--certificate-oidc-issuer=")
    two_word_flags+=("--certificate-oidc-issuer")
    local_nonpersistent_flags+=("--certificate-oidc-issuer")
    local_nonpersistent_flags+=("--certificate-oidc-issuer=")
    flags+=("--certificate-oidc-issuer-regexp=")
    two_word_flags+=("--certificate-oidc-issuer-regexp")
    local_nonpersistent_flags+=("--certificate-oidc-issuer-regexp")
    local_nonpersistent_flags+=("--certificate-oidc-issuer-regexp=")
    flags+=("--concurrency=")
    two_word_flags+=("--concurrency")
    two_word_flags+=("-c")
    local_nonpersistent_flags+=("--concurrency")
    local_nonpersistent_flags+=("--concurrency=")
    local_nonpersistent_flags+=("-c")
    flags+=("--config=")
    two_word_flags+=("--config")
    local_nonpersistent_flags+=("--config")
    local_nonpersistent_flags+=("--config=")
    flags+=("--confirm")
    local_nonpersistent_flags+=("--confirm")
    flags+=("--dry-run")
    local_nonpersistent_flags+=("--dry-run")
    flags+=("--fapolicyd")
    flags+=("-f")
    local_nonpersistent_flags+=("--fapolicyd")
    local_nonpersistent_flags+=("-f")
    flags+=("--firewall")
    flags+=("-F")
    local_nonpersistent_flags+=("--firewall")
    local_nonpersistent_flags+=("-F")
    flags+=("--hosts")
    flags+=("-H")
    local_nonpersistent_flags+=("--hosts")
    local_nonpersistent_flags+=("-H")
    flags+=("--insecure-ignore-tlog")
    local_nonpersistent_flags+=("--insecure-ignore-tlog")
    flags+=("--key=")
    two_word_flags+=("--key")
    two_word_flags+=("-k")
    local_nonpersistent_flags+=("--key")
    local_nonpersistent_flags+=("--key=")
    local_nonpersistent_flags+=("-k")
    flags+=("--timeout=")
    two_word_flags+=("--timeout")
    local_nonpersistent_flags+=("--timeout")
    local_nonpersistent_flags+=("--timeout=")
    flags+=("--tmpdir=")
    two_word_flags+=("--tmpdir")
    local_nonpersistent_flags+=("--tmpdir")
    local_nonpersistent_flags+=("--tmpdir=")
    flags+=("--trusted-root=")
    two_word_flags+=("--trusted-root")
    local_nonpersistent_flags+=("--trusted-root")
    local_nonpersistent_flags+=("--trusted-root=")
    flags+=("--use-signed-timestamps")
    local_nonpersistent_flags+=("--use-signed-timestamps")
    flags+=("--values=")
    two_word_flags+=("--values")
    local_nonpersistent_flags+=("--values")
    local_nonpersistent_flags+=("--values=")
    flags+=("--verify=")
    two_word_flags+=("--verify")
    local_nonpersistent_flags+=("--verify")
    local_nonpersistent_flags+=("--verify=")
    flags+=("--work-concurrency=")
    two_word_flags+=("--work-concurrency")
    two_word_flags+=("-w")
    local_nonpersistent_flags+=("--work-concurrency")
    local_nonpersistent_flags+=("--work-concurrency=")
    local_nonpersistent_flags+=("-w")
    flags+=("--zarf-cache=")
    two_word_flags+=("--zarf-cache")
    local_nonpersistent_flags+=("--zarf-cache")
    local_nonpersistent_flags+=("--zarf-cache=")
    flags+=("--log-file")
    flags+=("--log-format=")
    two_word_flags+=("--log-format")
    flags_with_completion+=("--log-format")
    flags_completion+=("__cargoship_handle_go_custom_completion")
    two_word_flags+=("-L")
    flags_with_completion+=("-L")
    flags_completion+=("__cargoship_handle_go_custom_completion")
    flags+=("--log-level=")
    two_word_flags+=("--log-level")
    flags_with_completion+=("--log-level")
    flags_completion+=("__cargoship_handle_go_custom_completion")
    two_word_flags+=("-l")
    flags_with_completion+=("-l")
    flags_completion+=("__cargoship_handle_go_custom_completion")
    flags+=("--no-color")

    must_have_one_flag=()
    must_have_one_flag+=("--config=")
    must_have_one_noun=()
    noun_aliases=()
}

_cargoship_publish()
{
    last_command="cargoship_publish"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--architecture=")
    two_word_flags+=("--architecture")
    flags_with_completion+=("--architecture")
    flags_completion+=("__cargoship_handle_go_custom_completion")
    two_word_flags+=("-a")
    flags_with_completion+=("-a")
    flags_completion+=("__cargoship_handle_go_custom_completion")
    local_nonpersistent_flags+=("--architecture")
    local_nonpersistent_flags+=("--architecture=")
    local_nonpersistent_flags+=("-a")
    flags+=("--certificate-identity=")
    two_word_flags+=("--certificate-identity")
    local_nonpersistent_flags+=("--certificate-identity")
    local_nonpersistent_flags+=("--certificate-identity=")
    flags+=("--certificate-identity-regexp=")
    two_word_flags+=("--certificate-identity-regexp")
    local_nonpersistent_flags+=("--certificate-identity-regexp")
    local_nonpersistent_flags+=("--certificate-identity-regexp=")
    flags+=("--certificate-oidc-issuer=")
    two_word_flags+=("--certificate-oidc-issuer")
    local_nonpersistent_flags+=("--certificate-oidc-issuer")
    local_nonpersistent_flags+=("--certificate-oidc-issuer=")
    flags+=("--certificate-oidc-issuer-regexp=")
    two_word_flags+=("--certificate-oidc-issuer-regexp")
    local_nonpersistent_flags+=("--certificate-oidc-issuer-regexp")
    local_nonpersistent_flags+=("--certificate-oidc-issuer-regexp=")
    flags+=("--confirm")
    flags+=("-c")
    local_nonpersistent_flags+=("--confirm")
    local_nonpersistent_flags+=("-c")
    flags+=("--insecure-ignore-tlog")
    local_nonpersistent_flags+=("--insecure-ignore-tlog")
    flags+=("--insecure-skip-tls-verify")
    local_nonpersistent_flags+=("--insecure-skip-tls-verify")
    flags+=("--key=")
    two_word_flags+=("--key")
    two_word_flags+=("-k")
    local_nonpersistent_flags+=("--key")
    local_nonpersistent_flags+=("--key=")
    local_nonpersistent_flags+=("-k")
    flags+=("--oci-concurrency=")
    two_word_flags+=("--oci-concurrency")
    flags_with_completion+=("--oci-concurrency")
    flags_completion+=("__cargoship_handle_go_custom_completion")
    local_nonpersistent_flags+=("--oci-concurrency")
    local_nonpersistent_flags+=("--oci-concurrency=")
    flags+=("--plain-http")
    local_nonpersistent_flags+=("--plain-http")
    flags+=("--retries=")
    two_word_flags+=("--retries")
    local_nonpersistent_flags+=("--retries")
    local_nonpersistent_flags+=("--retries=")
    flags+=("--signing-key=")
    two_word_flags+=("--signing-key")
    local_nonpersistent_flags+=("--signing-key")
    local_nonpersistent_flags+=("--signing-key=")
    flags+=("--signing-key-pass=")
    two_word_flags+=("--signing-key-pass")
    local_nonpersistent_flags+=("--signing-key-pass")
    local_nonpersistent_flags+=("--signing-key-pass=")
    flags+=("--tag=")
    two_word_flags+=("--tag")
    local_nonpersistent_flags+=("--tag")
    local_nonpersistent_flags+=("--tag=")
    flags+=("--tmpdir=")
    two_word_flags+=("--tmpdir")
    local_nonpersistent_flags+=("--tmpdir")
    local_nonpersistent_flags+=("--tmpdir=")
    flags+=("--trusted-root=")
    two_word_flags+=("--trusted-root")
    local_nonpersistent_flags+=("--trusted-root")
    local_nonpersistent_flags+=("--trusted-root=")
    flags+=("--use-signed-timestamps")
    local_nonpersistent_flags+=("--use-signed-timestamps")
    flags+=("--verify=")
    two_word_flags+=("--verify")
    local_nonpersistent_flags+=("--verify")
    local_nonpersistent_flags+=("--verify=")
    flags+=("--zarf-cache=")
    two_word_flags+=("--zarf-cache")
    local_nonpersistent_flags+=("--zarf-cache")
    local_nonpersistent_flags+=("--zarf-cache=")
    flags+=("--log-file")
    flags+=("--log-format=")
    two_word_flags+=("--log-format")
    flags_with_completion+=("--log-format")
    flags_completion+=("__cargoship_handle_go_custom_completion")
    two_word_flags+=("-L")
    flags_with_completion+=("-L")
    flags_completion+=("__cargoship_handle_go_custom_completion")
    flags+=("--log-level=")
    two_word_flags+=("--log-level")
    flags_with_completion+=("--log-level")
    flags_completion+=("__cargoship_handle_go_custom_completion")
    two_word_flags+=("-l")
    flags_with_completion+=("-l")
    flags_completion+=("__cargoship_handle_go_custom_completion")
    flags+=("--no-color")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_cargoship_pull()
{
    last_command="cargoship_pull"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--architecture=")
    two_word_flags+=("--architecture")
    flags_with_completion+=("--architecture")
    flags_completion+=("__cargoship_handle_go_custom_completion")
    two_word_flags+=("-a")
    flags_with_completion+=("-a")
    flags_completion+=("__cargoship_handle_go_custom_completion")
    local_nonpersistent_flags+=("--architecture")
    local_nonpersistent_flags+=("--architecture=")
    local_nonpersistent_flags+=("-a")
    flags+=("--certificate-identity=")
    two_word_flags+=("--certificate-identity")
    local_nonpersistent_flags+=("--certificate-identity")
    local_nonpersistent_flags+=("--certificate-identity=")
    flags+=("--certificate-identity-regexp=")
    two_word_flags+=("--certificate-identity-regexp")
    local_nonpersistent_flags+=("--certificate-identity-regexp")
    local_nonpersistent_flags+=("--certificate-identity-regexp=")
    flags+=("--certificate-oidc-issuer=")
    two_word_flags+=("--certificate-oidc-issuer")
    local_nonpersistent_flags+=("--certificate-oidc-issuer")
    local_nonpersistent_flags+=("--certificate-oidc-issuer=")
    flags+=("--certificate-oidc-issuer-regexp=")
    two_word_flags+=("--certificate-oidc-issuer-regexp")
    local_nonpersistent_flags+=("--certificate-oidc-issuer-regexp")
    local_nonpersistent_flags+=("--certificate-oidc-issuer-regexp=")
    flags+=("--insecure-ignore-tlog")
    local_nonpersistent_flags+=("--insecure-ignore-tlog")
    flags+=("--insecure-skip-tls-verify")
    local_nonpersistent_flags+=("--insecure-skip-tls-verify")
    flags+=("--key=")
    two_word_flags+=("--key")
    two_word_flags+=("-k")
    local_nonpersistent_flags+=("--key")
    local_nonpersistent_flags+=("--key=")
    local_nonpersistent_flags+=("-k")
    flags+=("--oci-concurrency=")
    two_word_flags+=("--oci-concurrency")
    flags_with_completion+=("--oci-concurrency")
    flags_completion+=("__cargoship_handle_go_custom_completion")
    local_nonpersistent_flags+=("--oci-concurrency")
    local_nonpersistent_flags+=("--oci-concurrency=")
    flags+=("--output=")
    two_word_flags+=("--output")
    two_word_flags+=("-o")
    local_nonpersistent_flags+=("--output")
    local_nonpersistent_flags+=("--output=")
    local_nonpersistent_flags+=("-o")
    flags+=("--plain-http")
    local_nonpersistent_flags+=("--plain-http")
    flags+=("--shasum=")
    two_word_flags+=("--shasum")
    local_nonpersistent_flags+=("--shasum")
    local_nonpersistent_flags+=("--shasum=")
    flags+=("--tmpdir=")
    two_word_flags+=("--tmpdir")
    local_nonpersistent_flags+=("--tmpdir")
    local_nonpersistent_flags+=("--tmpdir=")
    flags+=("--trusted-root=")
    two_word_flags+=("--trusted-root")
    local_nonpersistent_flags+=("--trusted-root")
    local_nonpersistent_flags+=("--trusted-root=")
    flags+=("--use-signed-timestamps")
    local_nonpersistent_flags+=("--use-signed-timestamps")
    flags+=("--verify=")
    two_word_flags+=("--verify")
    local_nonpersistent_flags+=("--verify")
    local_nonpersistent_flags+=("--verify=")
    flags+=("--zarf-cache=")
    two_word_flags+=("--zarf-cache")
    local_nonpersistent_flags+=("--zarf-cache")
    local_nonpersistent_flags+=("--zarf-cache=")
    flags+=("--log-file")
    flags+=("--log-format=")
    two_word_flags+=("--log-format")
    flags_with_completion+=("--log-format")
    flags_completion+=("__cargoship_handle_go_custom_completion")
    two_word_flags+=("-L")
    flags_with_completion+=("-L")
    flags_completion+=("__cargoship_handle_go_custom_completion")
    flags+=("--log-level=")
    two_word_flags+=("--log-level")
    flags_with_completion+=("--log-level")
    flags_completion+=("__cargoship_handle_go_custom_completion")
    two_word_flags+=("-l")
    flags_with_completion+=("-l")
    flags_completion+=("__cargoship_handle_go_custom_completion")
    flags+=("--no-color")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_cargoship_reset()
{
    last_command="cargoship_reset"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--concurrency=")
    two_word_flags+=("--concurrency")
    two_word_flags+=("-c")
    local_nonpersistent_flags+=("--concurrency")
    local_nonpersistent_flags+=("--concurrency=")
    local_nonpersistent_flags+=("-c")
    flags+=("--config=")
    two_word_flags+=("--config")
    local_nonpersistent_flags+=("--config")
    local_nonpersistent_flags+=("--config=")
    flags+=("--confirm")
    local_nonpersistent_flags+=("--confirm")
    flags+=("--distro=")
    two_word_flags+=("--distro")
    two_word_flags+=("-D")
    local_nonpersistent_flags+=("--distro")
    local_nonpersistent_flags+=("--distro=")
    local_nonpersistent_flags+=("-D")
    flags+=("--dry-run")
    local_nonpersistent_flags+=("--dry-run")
    flags+=("--fapolicyd")
    flags+=("-f")
    local_nonpersistent_flags+=("--fapolicyd")
    local_nonpersistent_flags+=("-f")
    flags+=("--firewall")
    flags+=("-F")
    local_nonpersistent_flags+=("--firewall")
    local_nonpersistent_flags+=("-F")
    flags+=("--hosts")
    flags+=("-H")
    local_nonpersistent_flags+=("--hosts")
    local_nonpersistent_flags+=("-H")
    flags+=("--work-concurrency=")
    two_word_flags+=("--work-concurrency")
    two_word_flags+=("-w")
    local_nonpersistent_flags+=("--work-concurrency")
    local_nonpersistent_flags+=("--work-concurrency=")
    local_nonpersistent_flags+=("-w")
    flags+=("--log-file")
    flags+=("--log-format=")
    two_word_flags+=("--log-format")
    flags_with_completion+=("--log-format")
    flags_completion+=("__cargoship_handle_go_custom_completion")
    two_word_flags+=("-L")
    flags_with_completion+=("-L")
    flags_completion+=("__cargoship_handle_go_custom_completion")
    flags+=("--log-level=")
    two_word_flags+=("--log-level")
    flags_with_completion+=("--log-level")
    flags_completion+=("__cargoship_handle_go_custom_completion")
    two_word_flags+=("-l")
    flags_with_completion+=("-l")
    flags_completion+=("__cargoship_handle_go_custom_completion")
    flags+=("--no-color")

    must_have_one_flag=()
    must_have_one_flag+=("--config=")
    must_have_one_noun=()
    noun_aliases=()
}

_cargoship_schema()
{
    last_command="cargoship_schema"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--architecture=")
    two_word_flags+=("--architecture")
    flags_with_completion+=("--architecture")
    flags_completion+=("__cargoship_handle_go_custom_completion")
    two_word_flags+=("-a")
    flags_with_completion+=("-a")
    flags_completion+=("__cargoship_handle_go_custom_completion")
    local_nonpersistent_flags+=("--architecture")
    local_nonpersistent_flags+=("--architecture=")
    local_nonpersistent_flags+=("-a")
    flags+=("--certificate-identity=")
    two_word_flags+=("--certificate-identity")
    local_nonpersistent_flags+=("--certificate-identity")
    local_nonpersistent_flags+=("--certificate-identity=")
    flags+=("--certificate-identity-regexp=")
    two_word_flags+=("--certificate-identity-regexp")
    local_nonpersistent_flags+=("--certificate-identity-regexp")
    local_nonpersistent_flags+=("--certificate-identity-regexp=")
    flags+=("--certificate-oidc-issuer=")
    two_word_flags+=("--certificate-oidc-issuer")
    local_nonpersistent_flags+=("--certificate-oidc-issuer")
    local_nonpersistent_flags+=("--certificate-oidc-issuer=")
    flags+=("--certificate-oidc-issuer-regexp=")
    two_word_flags+=("--certificate-oidc-issuer-regexp")
    local_nonpersistent_flags+=("--certificate-oidc-issuer-regexp")
    local_nonpersistent_flags+=("--certificate-oidc-issuer-regexp=")
    flags+=("--insecure-ignore-tlog")
    local_nonpersistent_flags+=("--insecure-ignore-tlog")
    flags+=("--insecure-skip-tls-verify")
    local_nonpersistent_flags+=("--insecure-skip-tls-verify")
    flags+=("--key=")
    two_word_flags+=("--key")
    two_word_flags+=("-k")
    local_nonpersistent_flags+=("--key")
    local_nonpersistent_flags+=("--key=")
    local_nonpersistent_flags+=("-k")
    flags+=("--output=")
    two_word_flags+=("--output")
    two_word_flags+=("-o")
    local_nonpersistent_flags+=("--output")
    local_nonpersistent_flags+=("--output=")
    local_nonpersistent_flags+=("-o")
    flags+=("--package=")
    two_word_flags+=("--package")
    local_nonpersistent_flags+=("--package")
    local_nonpersistent_flags+=("--package=")
    flags+=("--plain-http")
    local_nonpersistent_flags+=("--plain-http")
    flags+=("--tmpdir=")
    two_word_flags+=("--tmpdir")
    local_nonpersistent_flags+=("--tmpdir")
    local_nonpersistent_flags+=("--tmpdir=")
    flags+=("--trusted-root=")
    two_word_flags+=("--trusted-root")
    local_nonpersistent_flags+=("--trusted-root")
    local_nonpersistent_flags+=("--trusted-root=")
    flags+=("--use-signed-timestamps")
    local_nonpersistent_flags+=("--use-signed-timestamps")
    flags+=("--verify=")
    two_word_flags+=("--verify")
    local_nonpersistent_flags+=("--verify")
    local_nonpersistent_flags+=("--verify=")
    flags+=("--zarf-cache=")
    two_word_flags+=("--zarf-cache")
    local_nonpersistent_flags+=("--zarf-cache")
    local_nonpersistent_flags+=("--zarf-cache=")
    flags+=("--log-file")
    flags+=("--log-format=")
    two_word_flags+=("--log-format")
    flags_with_completion+=("--log-format")
    flags_completion+=("__cargoship_handle_go_custom_completion")
    two_word_flags+=("-L")
    flags_with_completion+=("-L")
    flags_completion+=("__cargoship_handle_go_custom_completion")
    flags+=("--log-level=")
    two_word_flags+=("--log-level")
    flags_with_completion+=("--log-level")
    flags_completion+=("__cargoship_handle_go_custom_completion")
    two_word_flags+=("-l")
    flags_with_completion+=("-l")
    flags_completion+=("__cargoship_handle_go_custom_completion")
    flags+=("--no-color")

    must_have_one_flag=()
    must_have_one_noun=()
    must_have_one_noun+=("config")
    must_have_one_noun+=("inventory")
    must_have_one_noun+=("package")
    noun_aliases=()
}

_cargoship_sha256sum()
{
    last_command="cargoship_sha256sum"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--extract-path=")
    two_word_flags+=("--extract-path")
    two_word_flags+=("-e")
    local_nonpersistent_flags+=("--extract-path")
    local_nonpersistent_flags+=("--extract-path=")
    local_nonpersistent_flags+=("-e")
    flags+=("--tmpdir=")
    two_word_flags+=("--tmpdir")
    local_nonpersistent_flags+=("--tmpdir")
    local_nonpersistent_flags+=("--tmpdir=")
    flags+=("--log-file")
    flags+=("--log-format=")
    two_word_flags+=("--log-format")
    flags_with_completion+=("--log-format")
    flags_completion+=("__cargoship_handle_go_custom_completion")
    two_word_flags+=("-L")
    flags_with_completion+=("-L")
    flags_completion+=("__cargoship_handle_go_custom_completion")
    flags+=("--log-level=")
    two_word_flags+=("--log-level")
    flags_with_completion+=("--log-level")
    flags_completion+=("__cargoship_handle_go_custom_completion")
    two_word_flags+=("-l")
    flags_with_completion+=("-l")
    flags_completion+=("__cargoship_handle_go_custom_completion")
    flags+=("--no-color")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_cargoship_sign()
{
    last_command="cargoship_sign"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--architecture=")
    two_word_flags+=("--architecture")
    flags_with_completion+=("--architecture")
    flags_completion+=("__cargoship_handle_go_custom_completion")
    two_word_flags+=("-a")
    flags_with_completion+=("-a")
    flags_completion+=("__cargoship_handle_go_custom_completion")
    local_nonpersistent_flags+=("--architecture")
    local_nonpersistent_flags+=("--architecture=")
    local_nonpersistent_flags+=("-a")
    flags+=("--certificate-identity=")
    two_word_flags+=("--certificate-identity")
    local_nonpersistent_flags+=("--certificate-identity")
    local_nonpersistent_flags+=("--certificate-identity=")
    flags+=("--certificate-identity-regexp=")
    two_word_flags+=("--certificate-identity-regexp")
    local_nonpersistent_flags+=("--certificate-identity-regexp")
    local_nonpersistent_flags+=("--certificate-identity-regexp=")
    flags+=("--certificate-oidc-issuer=")
    two_word_flags+=("--certificate-oidc-issuer")
    local_nonpersistent_flags+=("--certificate-oidc-issuer")
    local_nonpersistent_flags+=("--certificate-oidc-issuer=")
    flags+=("--certificate-oidc-issuer-regexp=")
    two_word_flags+=("--certificate-oidc-issuer-regexp")
    local_nonpersistent_flags+=("--certificate-oidc-issuer-regexp")
    local_nonpersistent_flags+=("--certificate-oidc-issuer-regexp=")
    flags+=("--confirm")
    local_nonpersistent_flags+=("--confirm")
    flags+=("--fulcio-auth-flow=")
    two_word_flags+=("--fulcio-auth-flow")
    local_nonpersistent_flags+=("--fulcio-auth-flow")
    local_nonpersistent_flags+=("--fulcio-auth-flow=")
    flags+=("--fulcio-url=")
    two_word_flags+=("--fulcio-url")
    local_nonpersistent_flags+=("--fulcio-url")
    local_nonpersistent_flags+=("--fulcio-url=")
    flags+=("--identity-token=")
    two_word_flags+=("--identity-token")
    local_nonpersistent_flags+=("--identity-token")
    local_nonpersistent_flags+=("--identity-token=")
    flags+=("--insecure-ignore-tlog")
    local_nonpersistent_flags+=("--insecure-ignore-tlog")
    flags+=("--insecure-skip-tls-verify")
    local_nonpersistent_flags+=("--insecure-skip-tls-verify")
    flags+=("--key=")
    two_word_flags+=("--key")
    two_word_flags+=("-k")
    local_nonpersistent_flags+=("--key")
    local_nonpersistent_flags+=("--key=")
    local_nonpersistent_flags+=("-k")
    flags+=("--keyless")
    local_nonpersistent_flags+=("--keyless")
    flags+=("--oci-concurrency=")
    two_word_flags+=("--oci-concurrency")
    flags_with_completion+=("--oci-concurrency")
    flags_completion+=("__cargoship_handle_go_custom_completion")
    local_nonpersistent_flags+=("--oci-concurrency")
    local_nonpersistent_flags+=("--oci-concurrency=")
    flags+=("--oidc-client-id=")
    two_word_flags+=("--oidc-client-id")
    local_nonpersistent_flags+=("--oidc-client-id")
    local_nonpersistent_flags+=("--oidc-client-id=")
    flags+=("--oidc-issuer=")
    two_word_flags+=("--oidc-issuer")
    local_nonpersistent_flags+=("--oidc-issuer")
    local_nonpersistent_flags+=("--oidc-issuer=")
    flags+=("--output=")
    two_word_flags+=("--output")
    two_word_flags+=("-o")
    local_nonpersistent_flags+=("--output")
    local_nonpersistent_flags+=("--output=")
    local_nonpersistent_flags+=("-o")
    flags+=("--overwrite")
    local_nonpersistent_flags+=("--overwrite")
    flags+=("--plain-http")
    local_nonpersistent_flags+=("--plain-http")
    flags+=("--rekor-url=")
    two_word_flags+=("--rekor-url")
    local_nonpersistent_flags+=("--rekor-url")
    local_nonpersistent_flags+=("--rekor-url=")
    flags+=("--retries=")
    two_word_flags+=("--retries")
    local_nonpersistent_flags+=("--retries")
    local_nonpersistent_flags+=("--retries=")
    flags+=("--signing-key=")
    two_word_flags+=("--signing-key")
    local_nonpersistent_flags+=("--signing-key")
    local_nonpersistent_flags+=("--signing-key=")
    flags+=("--signing-key-pass=")
    two_word_flags+=("--signing-key-pass")
    local_nonpersistent_flags+=("--signing-key-pass")
    local_nonpersistent_flags+=("--signing-key-pass=")
    flags+=("--tlog-upload")
    local_nonpersistent_flags+=("--tlog-upload")
    flags+=("--tmpdir=")
    two_word_flags+=("--tmpdir")
    local_nonpersistent_flags+=("--tmpdir")
    local_nonpersistent_flags+=("--tmpdir=")
    flags+=("--trusted-root=")
    two_word_flags+=("--trusted-root")
    local_nonpersistent_flags+=("--trusted-root")
    local_nonpersistent_flags+=("--trusted-root=")
    flags+=("--tsa-server-url=")
    two_word_flags+=("--tsa-server-url")
    local_nonpersistent_flags+=("--tsa-server-url")
    local_nonpersistent_flags+=("--tsa-server-url=")
    flags+=("--use-signed-timestamps")
    local_nonpersistent_flags+=("--use-signed-timestamps")
    flags+=("--verify=")
    two_word_flags+=("--verify")
    local_nonpersistent_flags+=("--verify")
    local_nonpersistent_flags+=("--verify=")
    flags+=("--zarf-cache=")
    two_word_flags+=("--zarf-cache")
    local_nonpersistent_flags+=("--zarf-cache")
    local_nonpersistent_flags+=("--zarf-cache=")
    flags+=("--log-file")
    flags+=("--log-format=")
    two_word_flags+=("--log-format")
    flags_with_completion+=("--log-format")
    flags_completion+=("__cargoship_handle_go_custom_completion")
    two_word_flags+=("-L")
    flags_with_completion+=("-L")
    flags_completion+=("__cargoship_handle_go_custom_completion")
    flags+=("--log-level=")
    two_word_flags+=("--log-level")
    flags_with_completion+=("--log-level")
    flags_completion+=("__cargoship_handle_go_custom_completion")
    two_word_flags+=("-l")
    flags_with_completion+=("-l")
    flags_completion+=("__cargoship_handle_go_custom_completion")
    flags+=("--no-color")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_cargoship_validate()
{
    last_command="cargoship_validate"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--architecture=")
    two_word_flags+=("--architecture")
    flags_with_completion+=("--architecture")
    flags_completion+=("__cargoship_handle_go_custom_completion")
    two_word_flags+=("-a")
    flags_with_completion+=("-a")
    flags_completion+=("__cargoship_handle_go_custom_completion")
    local_nonpersistent_flags+=("--architecture")
    local_nonpersistent_flags+=("--architecture=")
    local_nonpersistent_flags+=("-a")
    flags+=("--certificate-identity=")
    two_word_flags+=("--certificate-identity")
    local_nonpersistent_flags+=("--certificate-identity")
    local_nonpersistent_flags+=("--certificate-identity=")
    flags+=("--certificate-identity-regexp=")
    two_word_flags+=("--certificate-identity-regexp")
    local_nonpersistent_flags+=("--certificate-identity-regexp")
    local_nonpersistent_flags+=("--certificate-identity-regexp=")
    flags+=("--certificate-oidc-issuer=")
    two_word_flags+=("--certificate-oidc-issuer")
    local_nonpersistent_flags+=("--certificate-oidc-issuer")
    local_nonpersistent_flags+=("--certificate-oidc-issuer=")
    flags+=("--certificate-oidc-issuer-regexp=")
    two_word_flags+=("--certificate-oidc-issuer-regexp")
    local_nonpersistent_flags+=("--certificate-oidc-issuer-regexp")
    local_nonpersistent_flags+=("--certificate-oidc-issuer-regexp=")
    flags+=("--insecure-ignore-tlog")
    local_nonpersistent_flags+=("--insecure-ignore-tlog")
    flags+=("--insecure-skip-tls-verify")
    local_nonpersistent_flags+=("--insecure-skip-tls-verify")
    flags+=("--key=")
    two_word_flags+=("--key")
    two_word_flags+=("-k")
    local_nonpersistent_flags+=("--key")
    local_nonpersistent_flags+=("--key=")
    local_nonpersistent_flags+=("-k")
    flags+=("--kind=")
    two_word_flags+=("--kind")
    flags_with_completion+=("--kind")
    flags_completion+=("__cargoship_handle_go_custom_completion")
    local_nonpersistent_flags+=("--kind")
    local_nonpersistent_flags+=("--kind=")
    flags+=("--package=")
    two_word_flags+=("--package")
    local_nonpersistent_flags+=("--package")
    local_nonpersistent_flags+=("--package=")
    flags+=("--plain-http")
    local_nonpersistent_flags+=("--plain-http")
    flags+=("--tmpdir=")
    two_word_flags+=("--tmpdir")
    local_nonpersistent_flags+=("--tmpdir")
    local_nonpersistent_flags+=("--tmpdir=")
    flags+=("--trusted-root=")
    two_word_flags+=("--trusted-root")
    local_nonpersistent_flags+=("--trusted-root")
    local_nonpersistent_flags+=("--trusted-root=")
    flags+=("--use-signed-timestamps")
    local_nonpersistent_flags+=("--use-signed-timestamps")
    flags+=("--verify=")
    two_word_flags+=("--verify")
    local_nonpersistent_flags+=("--verify")
    local_nonpersistent_flags+=("--verify=")
    flags+=("--zarf-cache=")
    two_word_flags+=("--zarf-cache")
    local_nonpersistent_flags+=("--zarf-cache")
    local_nonpersistent_flags+=("--zarf-cache=")
    flags+=("--log-file")
    flags+=("--log-format=")
    two_word_flags+=("--log-format")
    flags_with_completion+=("--log-format")
    flags_completion+=("__cargoship_handle_go_custom_completion")
    two_word_flags+=("-L")
    flags_with_completion+=("-L")
    flags_completion+=("__cargoship_handle_go_custom_completion")
    flags+=("--log-level=")
    two_word_flags+=("--log-level")
    flags_with_completion+=("--log-level")
    flags_completion+=("__cargoship_handle_go_custom_completion")
    two_word_flags+=("-l")
    flags_with_completion+=("-l")
    flags_completion+=("__cargoship_handle_go_custom_completion")
    flags+=("--no-color")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_cargoship_vault_decrypt()
{
    last_command="cargoship_vault_decrypt"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--age-identity-file=")
    two_word_flags+=("--age-identity-file")
    local_nonpersistent_flags+=("--age-identity-file")
    local_nonpersistent_flags+=("--age-identity-file=")
    flags+=("--age-recipient=")
    two_word_flags+=("--age-recipient")
    local_nonpersistent_flags+=("--age-recipient")
    local_nonpersistent_flags+=("--age-recipient=")
    flags+=("--age-recipients-file=")
    two_word_flags+=("--age-recipients-file")
    local_nonpersistent_flags+=("--age-recipients-file")
    local_nonpersistent_flags+=("--age-recipients-file=")
    flags+=("--vault-password-file=")
    two_word_flags+=("--vault-password-file")
    local_nonpersistent_flags+=("--vault-password-file")
    local_nonpersistent_flags+=("--vault-password-file=")
    flags+=("--log-file")
    flags+=("--log-format=")
    two_word_flags+=("--log-format")
    flags_with_completion+=("--log-format")
    flags_completion+=("__cargoship_handle_go_custom_completion")
    two_word_flags+=("-L")
    flags_with_completion+=("-L")
    flags_completion+=("__cargoship_handle_go_custom_completion")
    flags+=("--log-level=")
    two_word_flags+=("--log-level")
    flags_with_completion+=("--log-level")
    flags_completion+=("__cargoship_handle_go_custom_completion")
    two_word_flags+=("-l")
    flags_with_completion+=("-l")
    flags_completion+=("__cargoship_handle_go_custom_completion")
    flags+=("--no-color")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_cargoship_vault_decrypt-file()
{
    last_command="cargoship_vault_decrypt-file"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--age-identity-file=")
    two_word_flags+=("--age-identity-file")
    local_nonpersistent_flags+=("--age-identity-file")
    local_nonpersistent_flags+=("--age-identity-file=")
    flags+=("--age-recipient=")
    two_word_flags+=("--age-recipient")
    local_nonpersistent_flags+=("--age-recipient")
    local_nonpersistent_flags+=("--age-recipient=")
    flags+=("--age-recipients-file=")
    two_word_flags+=("--age-recipients-file")
    local_nonpersistent_flags+=("--age-recipients-file")
    local_nonpersistent_flags+=("--age-recipients-file=")
    flags+=("--dry-run")
    local_nonpersistent_flags+=("--dry-run")
    flags+=("--vault-password-file=")
    two_word_flags+=("--vault-password-file")
    local_nonpersistent_flags+=("--vault-password-file")
    local_nonpersistent_flags+=("--vault-password-file=")
    flags+=("--log-file")
    flags+=("--log-format=")
    two_word_flags+=("--log-format")
    flags_with_completion+=("--log-format")
    flags_completion+=("__cargoship_handle_go_custom_completion")
    two_word_flags+=("-L")
    flags_with_completion+=("-L")
    flags_completion+=("__cargoship_handle_go_custom_completion")
    flags+=("--log-level=")
    two_word_flags+=("--log-level")
    flags_with_completion+=("--log-level")
    flags_completion+=("__cargoship_handle_go_custom_completion")
    two_word_flags+=("-l")
    flags_with_completion+=("-l")
    flags_completion+=("__cargoship_handle_go_custom_completion")
    flags+=("--no-color")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_cargoship_vault_decrypt-path()
{
    last_command="cargoship_vault_decrypt-path"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--age-identity-file=")
    two_word_flags+=("--age-identity-file")
    local_nonpersistent_flags+=("--age-identity-file")
    local_nonpersistent_flags+=("--age-identity-file=")
    flags+=("--age-recipient=")
    two_word_flags+=("--age-recipient")
    local_nonpersistent_flags+=("--age-recipient")
    local_nonpersistent_flags+=("--age-recipient=")
    flags+=("--age-recipients-file=")
    two_word_flags+=("--age-recipients-file")
    local_nonpersistent_flags+=("--age-recipients-file")
    local_nonpersistent_flags+=("--age-recipients-file=")
    flags+=("--dry-run")
    local_nonpersistent_flags+=("--dry-run")
    flags+=("--vault-password-file=")
    two_word_flags+=("--vault-password-file")
    local_nonpersistent_flags+=("--vault-password-file")
    local_nonpersistent_flags+=("--vault-password-file=")
    flags+=("--log-file")
    flags+=("--log-format=")
    two_word_flags+=("--log-format")
    flags_with_completion+=("--log-format")
    flags_completion+=("__cargoship_handle_go_custom_completion")
    two_word_flags+=("-L")
    flags_with_completion+=("-L")
    flags_completion+=("__cargoship_handle_go_custom_completion")
    flags+=("--log-level=")
    two_word_flags+=("--log-level")
    flags_with_completion+=("--log-level")
    flags_completion+=("__cargoship_handle_go_custom_completion")
    two_word_flags+=("-l")
    flags_with_completion+=("-l")
    flags_completion+=("__cargoship_handle_go_custom_completion")
    flags+=("--no-color")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_cargoship_vault_encrypt()
{
    last_command="cargoship_vault_encrypt"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--age-identity-file=")
    two_word_flags+=("--age-identity-file")
    local_nonpersistent_flags+=("--age-identity-file")
    local_nonpersistent_flags+=("--age-identity-file=")
    flags+=("--age-recipient=")
    two_word_flags+=("--age-recipient")
    local_nonpersistent_flags+=("--age-recipient")
    local_nonpersistent_flags+=("--age-recipient=")
    flags+=("--age-recipients-file=")
    two_word_flags+=("--age-recipients-file")
    local_nonpersistent_flags+=("--age-recipients-file")
    local_nonpersistent_flags+=("--age-recipients-file=")
    flags+=("--vault-password-file=")
    two_word_flags+=("--vault-password-file")
    local_nonpersistent_flags+=("--vault-password-file")
    local_nonpersistent_flags+=("--vault-password-file=")
    flags+=("--log-file")
    flags+=("--log-format=")
    two_word_flags+=("--log-format")
    flags_with_completion+=("--log-format")
    flags_completion+=("__cargoship_handle_go_custom_completion")
    two_word_flags+=("-L")
    flags_with_completion+=("-L")
    flags_completion+=("__cargoship_handle_go_custom_completion")
    flags+=("--log-level=")
    two_word_flags+=("--log-level")
    flags_with_completion+=("--log-level")
    flags_completion+=("__cargoship_handle_go_custom_completion")
    two_word_flags+=("-l")
    flags_with_completion+=("-l")
    flags_completion+=("__cargoship_handle_go_custom_completion")
    flags+=("--no-color")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_cargoship_vault_encrypt-file()
{
    last_command="cargoship_vault_encrypt-file"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--age-identity-file=")
    two_word_flags+=("--age-identity-file")
    local_nonpersistent_flags+=("--age-identity-file")
    local_nonpersistent_flags+=("--age-identity-file=")
    flags+=("--age-recipient=")
    two_word_flags+=("--age-recipient")
    local_nonpersistent_flags+=("--age-recipient")
    local_nonpersistent_flags+=("--age-recipient=")
    flags+=("--age-recipients-file=")
    two_word_flags+=("--age-recipients-file")
    local_nonpersistent_flags+=("--age-recipients-file")
    local_nonpersistent_flags+=("--age-recipients-file=")
    flags+=("--dry-run")
    local_nonpersistent_flags+=("--dry-run")
    flags+=("--force")
    local_nonpersistent_flags+=("--force")
    flags+=("--vault-password-file=")
    two_word_flags+=("--vault-password-file")
    local_nonpersistent_flags+=("--vault-password-file")
    local_nonpersistent_flags+=("--vault-password-file=")
    flags+=("--log-file")
    flags+=("--log-format=")
    two_word_flags+=("--log-format")
    flags_with_completion+=("--log-format")
    flags_completion+=("__cargoship_handle_go_custom_completion")
    two_word_flags+=("-L")
    flags_with_completion+=("-L")
    flags_completion+=("__cargoship_handle_go_custom_completion")
    flags+=("--log-level=")
    two_word_flags+=("--log-level")
    flags_with_completion+=("--log-level")
    flags_completion+=("__cargoship_handle_go_custom_completion")
    two_word_flags+=("-l")
    flags_with_completion+=("-l")
    flags_completion+=("__cargoship_handle_go_custom_completion")
    flags+=("--no-color")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_cargoship_vault_encrypt-path()
{
    last_command="cargoship_vault_encrypt-path"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--age-identity-file=")
    two_word_flags+=("--age-identity-file")
    local_nonpersistent_flags+=("--age-identity-file")
    local_nonpersistent_flags+=("--age-identity-file=")
    flags+=("--age-recipient=")
    two_word_flags+=("--age-recipient")
    local_nonpersistent_flags+=("--age-recipient")
    local_nonpersistent_flags+=("--age-recipient=")
    flags+=("--age-recipients-file=")
    two_word_flags+=("--age-recipients-file")
    local_nonpersistent_flags+=("--age-recipients-file")
    local_nonpersistent_flags+=("--age-recipients-file=")
    flags+=("--dry-run")
    local_nonpersistent_flags+=("--dry-run")
    flags+=("--force")
    local_nonpersistent_flags+=("--force")
    flags+=("--vault-password-file=")
    two_word_flags+=("--vault-password-file")
    local_nonpersistent_flags+=("--vault-password-file")
    local_nonpersistent_flags+=("--vault-password-file=")
    flags+=("--log-file")
    flags+=("--log-format=")
    two_word_flags+=("--log-format")
    flags_with_completion+=("--log-format")
    flags_completion+=("__cargoship_handle_go_custom_completion")
    two_word_flags+=("-L")
    flags_with_completion+=("-L")
    flags_completion+=("__cargoship_handle_go_custom_completion")
    flags+=("--log-level=")
    two_word_flags+=("--log-level")
    flags_with_completion+=("--log-level")
    flags_completion+=("__cargoship_handle_go_custom_completion")
    two_word_flags+=("-l")
    flags_with_completion+=("-l")
    flags_completion+=("__cargoship_handle_go_custom_completion")
    flags+=("--no-color")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_cargoship_vault_keygen()
{
    last_command="cargoship_vault_keygen"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--output=")
    two_word_flags+=("--output")
    two_word_flags+=("-o")
    local_nonpersistent_flags+=("--output")
    local_nonpersistent_flags+=("--output=")
    local_nonpersistent_flags+=("-o")
    flags+=("--public-key")
    flags+=("-y")
    local_nonpersistent_flags+=("--public-key")
    local_nonpersistent_flags+=("-y")
    flags+=("--log-file")
    flags+=("--log-format=")
    two_word_flags+=("--log-format")
    flags_with_completion+=("--log-format")
    flags_completion+=("__cargoship_handle_go_custom_completion")
    two_word_flags+=("-L")
    flags_with_completion+=("-L")
    flags_completion+=("__cargoship_handle_go_custom_completion")
    flags+=("--log-level=")
    two_word_flags+=("--log-level")
    flags_with_completion+=("--log-level")
    flags_completion+=("__cargoship_handle_go_custom_completion")
    two_word_flags+=("-l")
    flags_with_completion+=("-l")
    flags_completion+=("__cargoship_handle_go_custom_completion")
    flags+=("--no-color")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_cargoship_vault_rekey()
{
    last_command="cargoship_vault_rekey"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--age-identity-file=")
    two_word_flags+=("--age-identity-file")
    local_nonpersistent_flags+=("--age-identity-file")
    local_nonpersistent_flags+=("--age-identity-file=")
    flags+=("--age-recipient=")
    two_word_flags+=("--age-recipient")
    local_nonpersistent_flags+=("--age-recipient")
    local_nonpersistent_flags+=("--age-recipient=")
    flags+=("--age-recipients-file=")
    two_word_flags+=("--age-recipients-file")
    local_nonpersistent_flags+=("--age-recipients-file")
    local_nonpersistent_flags+=("--age-recipients-file=")
    flags+=("--dry-run")
    local_nonpersistent_flags+=("--dry-run")
    flags+=("--new-vault-password-file=")
    two_word_flags+=("--new-vault-password-file")
    local_nonpersistent_flags+=("--new-vault-password-file")
    local_nonpersistent_flags+=("--new-vault-password-file=")
    flags+=("--vault-password-file=")
    two_word_flags+=("--vault-password-file")
    local_nonpersistent_flags+=("--vault-password-file")
    local_nonpersistent_flags+=("--vault-password-file=")
    flags+=("--log-file")
    flags+=("--log-format=")
    two_word_flags+=("--log-format")
    flags_with_completion+=("--log-format")
    flags_completion+=("__cargoship_handle_go_custom_completion")
    two_word_flags+=("-L")
    flags_with_completion+=("-L")
    flags_completion+=("__cargoship_handle_go_custom_completion")
    flags+=("--log-level=")
    two_word_flags+=("--log-level")
    flags_with_completion+=("--log-level")
    flags_completion+=("__cargoship_handle_go_custom_completion")
    two_word_flags+=("-l")
    flags_with_completion+=("-l")
    flags_completion+=("__cargoship_handle_go_custom_completion")
    flags+=("--no-color")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_cargoship_vault()
{
    last_command="cargoship_vault"

    command_aliases=()

    commands=()
    commands+=("decrypt")
    commands+=("decrypt-file")
    commands+=("decrypt-path")
    commands+=("encrypt")
    commands+=("encrypt-file")
    commands+=("encrypt-path")
    commands+=("keygen")
    commands+=("rekey")

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--log-file")
    flags+=("--log-format=")
    two_word_flags+=("--log-format")
    flags_with_completion+=("--log-format")
    flags_completion+=("__cargoship_handle_go_custom_completion")
    two_word_flags+=("-L")
    flags_with_completion+=("-L")
    flags_completion+=("__cargoship_handle_go_custom_completion")
    flags+=("--log-level=")
    two_word_flags+=("--log-level")
    flags_with_completion+=("--log-level")
    flags_completion+=("__cargoship_handle_go_custom_completion")
    two_word_flags+=("-l")
    flags_with_completion+=("-l")
    flags_completion+=("__cargoship_handle_go_custom_completion")
    flags+=("--no-color")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_cargoship_version()
{
    last_command="cargoship_version"

    command_aliases=()

    commands=()

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--output=")
    two_word_flags+=("--output")
    flags_with_completion+=("--output")
    flags_completion+=("__cargoship_handle_go_custom_completion")
    two_word_flags+=("-o")
    flags_with_completion+=("-o")
    flags_completion+=("__cargoship_handle_go_custom_completion")
    local_nonpersistent_flags+=("--output")
    local_nonpersistent_flags+=("--output=")
    local_nonpersistent_flags+=("-o")
    flags+=("--log-file")
    flags+=("--log-format=")
    two_word_flags+=("--log-format")
    flags_with_completion+=("--log-format")
    flags_completion+=("__cargoship_handle_go_custom_completion")
    two_word_flags+=("-L")
    flags_with_completion+=("-L")
    flags_completion+=("__cargoship_handle_go_custom_completion")
    flags+=("--log-level=")
    two_word_flags+=("--log-level")
    flags_with_completion+=("--log-level")
    flags_completion+=("__cargoship_handle_go_custom_completion")
    two_word_flags+=("-l")
    flags_with_completion+=("-l")
    flags_completion+=("__cargoship_handle_go_custom_completion")
    flags+=("--no-color")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

_cargoship_root_command()
{
    last_command="cargoship"

    command_aliases=()

    commands=()
    commands+=("apply")
    commands+=("create")
    commands+=("engine-config-sync")
    commands+=("inventory")
    commands+=("kube-config")
    commands+=("prepare")
    commands+=("publish")
    commands+=("pull")
    commands+=("reset")
    commands+=("schema")
    commands+=("sha256sum")
    if [[ -z "${BASH_VERSION:-}" || "${BASH_VERSINFO[0]:-}" -gt 3 ]]; then
        command_aliases+=("sum")
        aliashash["sum"]="sha256sum"
    fi
    commands+=("sign")
    if [[ -z "${BASH_VERSION:-}" || "${BASH_VERSINFO[0]:-}" -gt 3 ]]; then
        command_aliases+=("s")
        aliashash["s"]="sign"
    fi
    commands+=("validate")
    commands+=("vault")
    commands+=("version")
    if [[ -z "${BASH_VERSION:-}" || "${BASH_VERSINFO[0]:-}" -gt 3 ]]; then
        command_aliases+=("v")
        aliashash["v"]="version"
    fi

    flags=()
    two_word_flags=()
    local_nonpersistent_flags=()
    flags_with_completion=()
    flags_completion=()

    flags+=("--log-file")
    flags+=("--log-format=")
    two_word_flags+=("--log-format")
    flags_with_completion+=("--log-format")
    flags_completion+=("__cargoship_handle_go_custom_completion")
    two_word_flags+=("-L")
    flags_with_completion+=("-L")
    flags_completion+=("__cargoship_handle_go_custom_completion")
    flags+=("--log-level=")
    two_word_flags+=("--log-level")
    flags_with_completion+=("--log-level")
    flags_completion+=("__cargoship_handle_go_custom_completion")
    two_word_flags+=("-l")
    flags_with_completion+=("-l")
    flags_completion+=("__cargoship_handle_go_custom_completion")
    flags+=("--no-color")

    must_have_one_flag=()
    must_have_one_noun=()
    noun_aliases=()
}

__start_cargoship()
{
    local cur prev words cword split
    declare -A flaghash 2>/dev/null || :
    declare -A aliashash 2>/dev/null || :
    if declare -F _init_completion >/dev/null 2>&1; then
        _init_completion -s || return
    else
        __cargoship_init_completion -n "=" || return
    fi

    local c=0
    local flag_parsing_disabled=
    local flags=()
    local two_word_flags=()
    local local_nonpersistent_flags=()
    local flags_with_completion=()
    local flags_completion=()
    local commands=("cargoship")
    local command_aliases=()
    local must_have_one_flag=()
    local must_have_one_noun=()
    local has_completion_function=""
    local last_command=""
    local nouns=()
    local noun_aliases=()

    __cargoship_handle_word
}

if [[ $(type -t compopt) = "builtin" ]]; then
    complete -o default -F __start_cargoship cargoship
else
    complete -o default -o nospace -F __start_cargoship cargoship
fi

# ex: ts=4 sw=4 et filetype=sh
