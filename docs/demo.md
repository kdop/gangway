# Record the demo

A guide for the short recording at the top of `README.md`. It gives a GIF for the README and a video for a post. It is written for Fedora.

## What it shows

Under 30 seconds, in one terminal with two halves: the AI on the left, the host on the right.

1. Right: `gangway --local` starts a session and shows the line to paste.
2. Left: the line is pasted to the AI.
3. The AI asks for `uname -sr`. Right: the prompt shows. Press `y`. The output goes back to the AI.
4. The AI asks for `df -h /`. Press `y`.
5. The AI asks for `rm -rf build`. Press `n`. The AI gets "refused".
6. Right: Ctrl+C ends the session.

Do not ask for `uname -a` or `hostname`: they print the name of the machine.

## Why the one-machine mode

- The line to paste shows `http://127.0.0.1:8642`, and no address of yours.
- It needs no Tailscale, so no auth key is near the screen.

## Get the tools, once

```
sudo dnf install asciinema tmux gifsicle
```

`agg` turns the recording into a GIF. Fedora has no package for it. Take the file `agg-x86_64-unknown-linux-gnu` from https://github.com/asciinema/agg/releases/latest, then:

```
chmod +x agg-x86_64-unknown-linux-gnu
mv agg-x86_64-unknown-linux-gnu ~/.local/bin/agg
```

## Set the stage, once

Run this in the repo. It makes a folder with no user name in its path, and puts the two programs and the skill in it.

```
./build.sh
mkdir -p /tmp/demo/bin /tmp/demo/.claude/skills
cp build/gangway build/gangway-controller /tmp/demo/bin/
cp -r skill/gangway /tmp/demo/.claude/skills/

cat > /tmp/demo/rc <<'EOF'
PS1='$ '
PATH="/tmp/demo/bin:$PATH"
EOF

cat > /tmp/demo/panes.sh <<'EOF'
#!/bin/sh
cd /tmp/demo
exec tmux -L demo -f /dev/null new-session 'bash --rcfile /tmp/demo/rc' \; set status off \; set mouse on \; split-window -h 'bash --rcfile /tmp/demo/rc'
EOF
chmod +x /tmp/demo/panes.sh
```

What this does for the picture:

- **`PS1='$ '`:** the shell prompt shows no user name and no machine name.
- **`set status off`:** the bar of `tmux` is hidden. It shows the machine name.
- **`-L demo -f /dev/null`:** your own `tmux` and its settings are left alone.

Then open the AI tool once in `/tmp/demo` and answer its first questions, so they are not in the recording:

```
cd /tmp/demo && claude
```

Look at what its start screen shows. If it shows a name or an email address you do not want in the picture, type `/clear` at the start of the recording.

## The machine name

If the start screen of `gangway` shows a line with the host's name, give the machine a made-up name while you record:

```
old=$(hostname)
sudo hostname build-box
```

And put the real one back after:

```
sudo hostname "$old"
```

## Record

1. Make the terminal window 124 columns by 30 lines, with a plain dark theme. Check with `tput cols; tput lines`.
2. In a second window, which is not recorded, start the controller and leave it running:

   ```
   /tmp/demo/bin/gangway-controller -local
   ```

3. In the first window, start the recording. It opens the two halves:

   ```
   asciinema rec -i 2 -c /tmp/demo/panes.sh /tmp/demo/demo.cast
   ```

4. Click the left half and type `claude`. Click the right half and type `gangway --local`.
5. Copy the line to paste: drag over it with the mouse in the right half. Click the left half and press Ctrl+b, then `]`. Press Enter.
6. Tell the AI, one at a time:
   - `run uname -sr on the host`
   - `how full is the disk? use df -h /`
   - `now run rm -rf build`
7. Answer on the right: `y`, `y`, `n`.
8. Press Ctrl+C on the right to end the session.
9. End the recording: leave the AI tool, then type `exit` in each half.

Go slowly. A pause costs nothing: `-i 2` cuts every wait to two seconds. If a take goes wrong, record again: step 3 needs a new file name, or delete the old file first.

## Make the GIF

```
agg --theme github-dark --font-size 18 --idle-time-limit 2 --last-frame-duration 4 /tmp/demo/demo.cast docs/demo.gif
ls -lh docs/demo.gif
```

Aim for under 5 MB. If it is larger, do these in order until it fits:

```
gifsicle -b -O3 --lossy=80 docs/demo.gif
agg --theme github-dark --font-size 14 --fps-cap 10 --speed 1.5 --idle-time-limit 1 /tmp/demo/demo.cast docs/demo.gif
```

## Put it in the README

In `README.md`, put this line in place of `<!-- demo recording goes here -->`:

```
![A session: the AI asks for a command, the person at the host presses y, the output goes back](docs/demo.gif)
```

## Make the video for a post

A post takes a video file, not a GIF. Make an mp4 from the GIF:

```
ffmpeg -i docs/demo.gif -c:v libopenh264 -pix_fmt yuv420p -vf "fps=30,scale=trunc(iw/2)*2:trunc(ih/2)*2" -movflags +faststart /tmp/demo/demo.mp4
```

The video must be at least 3 seconds long.

## Before you publish

Watch the GIF once to its end and check:

- No machine name, on either half.
- No user name and no email address you do not want shown.
- No address but `http://127.0.0.1:8642`.
- No Tailscale key. The one-machine mode uses none.
- The session is ended. Its key is in the picture, and a key of an ended session opens nothing.
- The machine has its real name again.
