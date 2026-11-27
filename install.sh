#!/bin/sh
# Puts the cli-tool `gangway`, the controller `gangway-controller`, or both
# into ~/.local/bin, for the user who runs it.
#
#   sh install.sh                      the cli-tool, of the latest release
#   sh install.sh v0.1.0               the cli-tool, of that release
#   sh install.sh controller           the controller
#   sh install.sh all                  both
#   sh install.sh controller v0.1.0    the controller, of that release
#
# Everything is inside functions and the last line starts them, so a copy of
# this script that was cut off on its way runs nothing.

set -eu

RELEASES="https://github.com/kdop/gangway/releases"

say() {
	printf '%s\n' "$*"
}

stop() {
	say "install: $*" >&2
	exit 1
}

# fetch ADDRESS FILE: download ADDRESS into FILE.
fetch() {
	if command -v curl >/dev/null 2>&1; then
		curl --proto "=$protocol" --proto-redir "=$protocol" --tlsv1.2 -fsSL -o "$2" "$1"
	elif command -v wget >/dev/null 2>&1; then
		if [ "$protocol" = https ]; then
			wget -q --https-only -O "$2" "$1"
		else
			wget -q -O "$2" "$1"
		fi
	else
		stop "this needs curl or wget, and found neither."
	fi
}

clean_up() {
	if [ -n "${work:-}" ]; then
		rm -rf "$work"
	fi
}

main() {
	programs=""
	version=""
	for word in "$@"; do
		case "$word" in
		cli | controller | all)
			if [ -n "$programs" ]; then
				stop "say once what to install: cli, controller or all."
			fi
			case "$word" in
			cli) programs="gangway" ;;
			controller) programs="gangway-controller" ;;
			all) programs="gangway gangway-controller" ;;
			esac
			;;
		*)
			if [ -n "$version" ]; then
				stop "give one version at most, such as v0.1.0."
			fi
			version="$word"
			;;
		esac
	done
	if [ -z "$programs" ]; then
		programs="gangway"
	fi
	case "$version" in
	*[!A-Za-z0-9._-]*)
		stop "\"$version\" is not a version. Give one such as v0.1.0, or none for the latest."
		;;
	esac

	if [ "$(id -u)" = 0 ]; then
		stop "do not run this as root. gangway is for a plain user and does not run as root. Run it again as the user who will start the sessions."
	fi

	system="$(uname -s)"
	if [ "$system" != Linux ]; then
		stop "gangway runs on Linux only. This is $system."
	fi
	machine="$(uname -m)"
	case "$machine" in
	x86_64 | amd64) kind=amd64 ;;
	aarch64 | arm64) kind=arm64 ;;
	*) stop "there is no gangway for this kind of machine ($machine). There is one for x86_64 and one for aarch64." ;;
	esac

	case "${HOME:-}" in
	/*) ;;
	*) stop "HOME is not set to a folder, so there is no place to install to." ;;
	esac

	# The place to download from can be changed, for the tests of this
	# script. It must be https, or be on this machine.
	from="${GANGWAY_INSTALL_FROM:-$RELEASES}"
	from="${from%/}"
	case "$from" in
	*@*) stop "GANGWAY_INSTALL_FROM must not hold an @." ;;
	https://?*) protocol=https ;;
	http://127.0.0.1 | http://127.0.0.1[:/]* | http://localhost | http://localhost[:/]*) protocol=http ;;
	*) stop "GANGWAY_INSTALL_FROM must start with https://. Only an address on this machine may be plain http." ;;
	esac

	if ! command -v sha256sum >/dev/null 2>&1; then
		stop "this needs the program sha256sum to check the download, and did not find it."
	fi

	if [ -n "$version" ]; then
		release="$from/download/$version"
		what="release $version"
	else
		release="$from/latest/download"
		what="the latest release"
	fi

	folder="$HOME/.local/bin"
	for program in $programs; do
		if [ -d "$folder/$program" ]; then
			stop "$folder/$program is a folder, so the program cannot be put there. Nothing was installed."
		fi
	done
	mkdir -p "$folder"

	# The download lands in a folder next to the target, so that the last
	# step is one move on the same disk: the program is the old one or the
	# new one, never half of one.
	work=""
	trap clean_up EXIT
	trap 'exit 1' HUP INT TERM
	work="$(mktemp -d "$folder/.gangway-install.XXXXXX")"

	# Every program is downloaded and checked before the first one is put
	# in place: all of them are installed, or none.
	for program in $programs; do
		name="$program-linux-$kind"
		say "Downloading $name of $what ..."
		if ! fetch "$release/$name" "$work/$program"; then
			stop "could not download $release/$name. There may be no release yet, or no such version. The releases are at $RELEASES. Nothing was installed."
		fi
	done
	if ! fetch "$release/SHA256SUMS" "$work/sums"; then
		stop "could not download $release/SHA256SUMS, so the download cannot be checked. Nothing was installed."
	fi

	for program in $programs; do
		name="$program-linux-$kind"
		want="$(awk -v name="$name" '$2 == name || $2 == "*" name { print $1 }' "$work/sums")"
		case "$want" in
		"") stop "SHA256SUMS of $what has no line for $name, so the download cannot be checked. Nothing was installed." ;;
		*[!0-9a-f]*) stop "SHA256SUMS of $what has no single good line for $name. Nothing was installed." ;;
		esac
		if [ "${#want}" -ne 64 ]; then
			stop "SHA256SUMS of $what has no single good line for $name. Nothing was installed."
		fi
		have="$(sha256sum <"$work/$program" | awk '{ print $1 }')"
		if [ "$have" != "$want" ]; then
			stop "the download does not match its checksum in SHA256SUMS. Nothing was installed. Try again; if it happens again, do not use the file."
		fi

		chmod 755 "$work/$program"
	done

	for program in $programs; do
		mv -f "$work/$program" "$folder/$program"
	done

	say "The checksum matches."
	for program in $programs; do
		say "$program is installed at $folder/$program"
	done
	case ":${PATH:-}:" in
	*":$folder:"*) ;;
	*)
		say "$folder is not on your PATH. Add this line to the start file of your shell, such as ~/.profile:"
		say "  export PATH=\"\$HOME/.local/bin:\$PATH\""
		;;
	esac
	case "$programs" in
	"gangway") say "Next: gangway login" ;;
	"gangway-controller") say "Next: gangway-controller login" ;;
	*)
		say "Next, with Tailscale: gangway-controller login, and gangway login"
		say "Or try both on this machine with no Tailscale, in two terminals:"
		say "  gangway-controller -local"
		say "  gangway --local"
		;;
	esac
}

# In braces, so that this line cut short is an error and not a start.
{ main "$@"; }
