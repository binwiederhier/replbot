# REPLbot session recording

This ZIP archive contains a recording of your REPLbot session.
It contains the following files:

## `recording.txt`
The entire terminal output as you'd see it in your own terminal window. 
Depending on how much output you produced, this output may be cut off.

## `replay.script` / `replay.timing`
These two files can be used to replay the terminal session in realtime using
the `scriptreplay` tool. Simply run this command and be amazed:

```
$ scriptreplay -t replay.timing replay.script
```

You may also speed up replay a little with the `-d`/`--divisor` option, and avoid
long pauses with the `-m`/`--maxdelay` option:

```
$ scriptreplay -d 1.2 -m 5 -t replay.timing replay.script
```
